package main

import (
	"bufio"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Metric struct {
	Ts        int64    `json:"ts"`
	CPU       float64  `json:"cpu"`
	Mem       float64  `json:"mem"`
	Disk      float64  `json:"disk"`
	Load      *float64 `json:"load"`
	Temp      *float64 `json:"temp"`
	RxMbps    *float64 `json:"rx_mbps"`
	TxMbps    *float64 `json:"tx_mbps"`
	Occ       *float64 `json:"occ"`
	NetOK     bool     `json:"net_ok"`
	MemUsed   float64  `json:"mem_used"`  // байты
	MemTotal  float64  `json:"mem_total"` // байты
	DiskUsed  float64  `json:"disk_used"` // байты
	DiskTotal float64  `json:"disk_total"`
	Iface     string   `json:"-"`
}

type Collector struct {
	cfg   *Config
	state *State
	iface string
	prev  *NetState
}

func NewCollector(cfg *Config, state *State) *Collector {
	c := &Collector{cfg: cfg, state: state}
	c.iface = detectInterface(cfg.Network.Interface)
	st := state.Get()
	if st.Net != nil && st.Net.Iface == c.iface {
		c.prev = st.Net
	}
	return c
}

func (c *Collector) Collect() Metric {
	now := time.Now()
	m := Metric{Ts: now.Unix(), Iface: c.iface}
	m.CPU = cpuPercent(1 * time.Second)
	m.MemUsed, m.MemTotal, m.Mem = memInfo()
	m.DiskUsed, m.DiskTotal, m.Disk = diskInfo()
	m.Load = loadAvg1()
	m.Temp = temperature()
	m.NetOK = internetOK(c.cfg.InternetCheck.Hosts,
		time.Duration(c.cfg.InternetCheck.TimeoutSec)*time.Second)

	rx, tx, ok := ifaceCounters(c.iface)
	if ok {
		if c.prev != nil && c.prev.Iface == c.iface {
			dt := now.Unix() - c.prev.Ts
			if dt > 0 {
				drx, dtx := rx-c.prev.RX, tx-c.prev.TX
				rv := float64(drx) * 8 / float64(dt) / 1e6
				tv := float64(dtx) * 8 / float64(dt) / 1e6
				m.RxMbps, m.TxMbps = &rv, &tv
				if cap := c.cfg.Network.ChannelCapacityMbps; cap > 0 {
					o := (rv + tv) / cap * 100
					m.Occ = &o
				}
			}
		}
		c.prev = &NetState{Iface: c.iface, RX: rx, TX: tx, Ts: now.Unix()}
		saved := c.prev
		c.state.Update(func(d *StateData) { d.Net = saved })
	}
	return m
}

var skipFSTypes = map[string]bool{
	"proc": true, "sysfs": true, "devpts": true, "tmpfs": true, "devtmpfs": true,
	"efivarfs": true, "autofs": true, "cgroup": true, "cgroup2": true, "pstore": true,
	"bpf": true, "securityfs": true, "debugfs": true, "tracefs": true, "fusectl": true,
	"configfs": true, "hugetlbfs": true, "mqueue": true, "binfmt_misc": true,
	"nsfs": true, "ramfs": true, "none": true, "squashfs": true,
	"overlay": true, "aufs": true,
}

// Виртуальные интерфейсы, которые не считаются основными (по ним не меряем
// трафик/канал): loopback, docker, wireguard, tun/tap (easytier, openvpn), мосты.
var virtualPrefixes = []string{
	"lo", "docker", "veth", "br-", "virbr", "tun", "tap", "wg",
	"tailscale", "utun", "vmnet",
}

func skipIface(name string) bool {
	for _, p := range virtualPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

func detectInterface(configured string) string {
	if configured != "" {
		return configured
	}
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return ""
	}
	defer f.Close()
	var sb strings.Builder
	if _, err := io.Copy(&sb, f); err != nil {
		return ""
	}
	iface, _ := detectInterfaceFrom(sb.String())
	return iface
}

// detectInterfaceFrom выбирает физический интерфейс с наибольшим суммарным
// трафиком по выводу /proc/net/dev (виртуальные интерфейсы пропускаются).
func detectInterfaceFrom(procNetDev string) (string, bool) {
	best, bestTotal := "", uint64(0)
	sc := bufio.NewScanner(strings.NewReader(procNetDev))
	for sc.Scan() {
		line := sc.Text()
		idx := strings.IndexByte(line, ':')
		if idx < 0 {
			continue
		}
		name := strings.TrimSpace(line[:idx])
		if skipIface(name) {
			continue
		}
		fields := strings.Fields(line[idx+1:])
		if len(fields) < 9 {
			continue
		}
		total := atou(fields[0]) + atou(fields[8])
		if total > bestTotal {
			best, bestTotal = name, total
		}
	}
	return best, best != ""
}

func ifaceCounters(iface string) (rx, tx uint64, ok bool) {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		idx := strings.IndexByte(line, ':')
		if idx < 0 {
			continue
		}
		if strings.TrimSpace(line[:idx]) != iface {
			continue
		}
		fields := strings.Fields(line[idx+1:])
		if len(fields) < 9 {
			return
		}
		return atou(fields[0]), atou(fields[8]), true
	}
	return
}

func cpuPercent(interval time.Duration) float64 {
	t0, i0 := readCPUStat()
	time.Sleep(interval)
	t1, i1 := readCPUStat()
	dt := t1 - t0
	if dt == 0 {
		return 0
	}
	return (1 - float64(i1-i0)/float64(dt)) * 100
}

func readCPUStat() (total, idle uint64) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return
	}
	first := strings.SplitN(string(data), "\n", 2)[0]
	fields := strings.Fields(first)
	if len(fields) < 9 {
		return
	}
	vals := [8]uint64{}
	for i := 0; i < 8; i++ {
		vals[i] = atou(fields[1+i])
	}
	idle = vals[3] + vals[4]
	for _, v := range vals {
		total += v
	}
	return
}

func memInfo() (used, total, pct float64) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, 0
	}
	var totalKB, availKB float64
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			totalKB = memValue(line)
		} else if strings.HasPrefix(line, "MemAvailable:") {
			availKB = memValue(line)
		}
	}
	if totalKB <= 0 {
		return 0, 0, 0
	}
	total = totalKB * 1024
	used = (totalKB - availKB) * 1024
	pct = (1 - availKB/totalKB) * 100
	return
}

func memValue(line string) float64 {
	f := strings.Fields(line)
	if len(f) < 2 {
		return 0
	}
	v, _ := strconv.ParseFloat(f[1], 64)
	return v
}

func loadAvg1() *float64 {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return nil
	}
	f := strings.Fields(string(data))
	if len(f) == 0 {
		return nil
	}
	v, err := strconv.ParseFloat(f[0], 64)
	if err != nil {
		return nil
	}
	return &v
}

func temperature() *float64 {
	zones, _ := filepath.Glob("/sys/class/thermal/thermal_zone*/temp")
	best := -1.0
	for _, z := range zones {
		data, err := os.ReadFile(z)
		if err != nil {
			continue
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
		if err != nil {
			continue
		}
		c := v / 1000.0
		if c <= 0 || c > 200 {
			continue
		}
		if c > best {
			best = c
		}
	}
	if best < 0 {
		return nil
	}
	return &best
}

func diskInfo() (used, total, pct float64) {
	bestTotal := 0.0
	var bu, bt, bp float64
	f, err := os.Open("/proc/mounts")
	if err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			fields := strings.Fields(sc.Text())
			if len(fields) < 3 {
				continue
			}
			mount := unescapeMount(fields[1])
			fstype := fields[2]
			if skipFSTypes[fstype] || strings.HasPrefix(mount, "/snap") {
				continue
			}
			u, t, p := statfsUsage(mount)
			if t > bestTotal {
				bestTotal, bu, bt, bp = t, u, t, p
			}
		}
		f.Close()
	}
	if bestTotal <= 0 {
		bu, bt, bp = statfsUsage("/")
	}
	return bu, bt, bp
}

func unescapeMount(s string) string {
	r := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	return r.Replace(s)
}

func statfsUsage(path string) (used, total, pct float64) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, 0
	}
	bsize := float64(st.Bsize)
	total = float64(st.Blocks) * bsize
	used = float64(st.Blocks-st.Bavail) * bsize
	if total <= 0 {
		return 0, 0, 0
	}
	pct = used / total * 100
	return
}

func internetOK(hosts []string, timeout time.Duration) bool {
	for _, hp := range hosts {
		conn, err := net.DialTimeout("tcp", hp, timeout)
		if err == nil {
			conn.Close()
			return true
		}
	}
	return false
}

func atou(s string) uint64 {
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0
	}
	return v
}
