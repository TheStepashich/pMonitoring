package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeTG struct {
	texts  []string
	docs   []string
	photos []string
}

func (f *fakeTG) SendText(text string, chatID *int64) error {
	f.texts = append(f.texts, text)
	return nil
}

func (f *fakeTG) SendDocument(doc []byte, filename, caption string, chatID *int64) error {
	f.docs = append(f.docs, filename)
	return nil
}

func (f *fakeTG) SendPhoto(photo []byte, caption string, chatID *int64) error {
	f.photos = append(f.photos, caption)
	return nil
}

func (f *fakeTG) GetUpdates(offset, timeout int) ([]Update, error) {
	return nil, nil
}

const samplePeers = `
=== Peers for network 'pnet' (port 15888) ===
| ipv4         | hostname                         | cost  | lat(ms) | loss | rx        | tx        | tunnel | NAT            | version        |
|--------------|----------------------------------|-------|---------|------|-----------|-----------|--------|----------------|----------------|
| 10.5.0.7/24  | thestepashich-pc                 | Local | -       | -    | -         | -         | -      | PortRestricted | 2.4.5-4c4d172e |
|              | PublicServer_isabelline-gahnospi | p2p   | 56.24   | 0.0% | 111.92 kB | 140.26 kB | udp    | NoPat          | 2.4.5-4c4d172e |
| 10.5.0.1/24  | potato-debian                    | p2p   | 118.69  | 0.0% | 667.84 kB | 748.65 kB | udp    | Symmetric      | 2.4.5-4c4d172e |
| 10.5.0.10/24 | bork-server                      | p2p   | 0.49    | 0.0% | 97.50 kB  | 106.83 kB | udp    | PortRestricted | 2.4.5-4c4d172e |
| 10.5.0.11/24 | isabelline-gahnospinel8936       | p2p   | 57.01   | 0.0% | 2.48 MB   | 1.99 MB   | udp    | NoPat          | 2.4.5-4c4d172e |

=== Peers for network 'bork' (port 15889) ===
| ipv4          | hostname                   | cost  | lat(ms) | loss | rx        | tx        | tunnel | NAT            | version        |
|---------------|----------------------------|-------|---------|------|-----------|-----------|--------|----------------|----------------|
| 10.10.0.7/24  | thestepashich-pc           | Local | -       | -    | -         | -         | -      | PortRestricted | 2.4.5-4c4d172e |
|               | PublicServer_p-russia      | p2p   | 56.44   | 0.0% | 159.93 kB | 205.76 kB | udp    | NoPat          | 2.4.5-4c4d172e |
| 10.10.0.1/24  | isabelline-gahnospinel8936 | p2p   | 56.84   | 0.0% | 1.62 MB   | 1.50 MB   | udp    | NoPat          | 2.4.5-4c4d172e |
| 10.10.0.10/24 | bork-server                | p2p   | 0.38    | 0.0% | 735.42 kB | 218.26 kB | udp    | PortRestricted | 2.4.5-4c4d172e |
| 10.10.0.20/24 | DESKTOP-TQVJDH6            | p2p   | 79.16   | 0.0% | 1.34 MB   | 151.31 kB | udp    | PortRestricted | 2.4.5-4c4d172e |
`

func TestParsePeers(t *testing.T) {
	all := parsePeers(samplePeers, nil)
	if ip := all["thestepashich-pc"]; len(ip) != 2 {
		t.Fatalf("thestepashich-pc должно быть в обеих сетях: %v", ip)
	}
	if _, ok := all["PublicServer_isabelline-gahnospi"]; ok {
		t.Fatal("peer без IPv4 не должен попасть")
	}
	if _, ok := all["PublicServer_p-russia"]; ok {
		t.Fatal("peer без IPv4 не должен попасть")
	}
	for _, name := range all {
		for _, ip := range name {
			if strings.Contains(ip, "-") {
				t.Fatalf("мусорная запись в ips: %q", ip)
			}
		}
	}
	pnet := parsePeers(samplePeers, []string{"pnet"})
	if _, ok := pnet["DESKTOP-TQVJDH6"]; ok {
		t.Fatal("bork-пир попал в pnet")
	}
	if len(pnet["thestepashich-pc"]) != 1 || pnet["thestepashich-pc"][0] != "10.5.0.7" {
		t.Fatalf("pnet: %v", pnet["thestepashich-pc"])
	}
}

func TestParsePeersBufferedPnet(t *testing.T) {
	// Реальный вывод pnet в pipe (не TTY): заголовки буферизуются и приходят
	// ПОСЛЕ таблиц в том же порядке сетей. Таблицы разделяются строками
	// "| ipv4 | hostname ... |". Сначала таблица pnet (как в конфиге), затем
	// таблица bork, затем отложенные заголовки pnet, bork.
	const broken = `| ipv4         | hostname                         | cost  | lat(ms) | loss | rx        | tx        | tunnel | NAT            | version        |
|--------------|----------------------------------|-------|---------|------|-----------|-----------|--------|----------------|----------------|
| 10.5.0.7/24  | thestepashich-pc                 | Local | -       | -    | -         | -         | -      | PortRestricted | 2.4.5-4c4d172e |
| 10.5.0.1/24  | potato-debian                    | p2p   | 114.93  | 1.0% | 323.21 kB | 5.16 MB   | udp    | Symmetric      | 2.4.5-4c4d172e |
| 10.5.0.10/24 | bork-server                      | p2p   | 0.40    | 0.0% | 10.12 kB  | 12.73 kB  | udp    | PortRestricted | 2.4.5-4c4d172e |
| 10.5.0.15/24 | bork-666                         | p2p   | 55.03   | 0.0% | 9.95 kB   | 12.79 kB  | udp    | NoPat          | 2.4.5-4c4d172e |
| ipv4            | hostname              | cost  | lat(ms) | loss | rx       | tx       | tunnel | NAT            | version        |
|-----------------|-----------------------|-------|---------|------|----------|----------|--------|----------------|----------------|
| 10.126.126.1/24 | BORK                  | Local | -       | -    | -        | -        | -      | PortRestricted | 2.4.5-4c4d172e |
|                 | PublicServer_p-russia | p2p   | 54.95   | 0.0% | 84.37 kB | 90.99 kB | udp    | NoPat          | 2.4.5-4c4d172e |
=== Peers for network 'pnet' (port 15888) ===

=== Peers for network 'bork' (port 15889) ===
`
	all := parsePeers(broken, nil)
	if _, ok := all["BORK"]; !ok {
		t.Fatalf("без фильтра BORK должен попасть: %v", all)
	}
	if len(all["thestepashich-pc"]) != 1 || all["thestepashich-pc"][0] != "10.5.0.7" {
		t.Fatalf("thestepashich-pc: %v", all["thestepashich-pc"])
	}
	if _, ok := all["PublicServer_p-russia"]; ok {
		t.Fatal("PublicServer без IPv4 не должен попасть")
	}

	pnet := parsePeers(broken, []string{"pnet"})
	if len(pnet["potato-debian"]) != 1 || pnet["potato-debian"][0] != "10.5.0.1" {
		t.Fatalf("potato-debian: %v", pnet["potato-debian"])
	}
	if len(pnet["bork-server"]) != 1 || pnet["bork-server"][0] != "10.5.0.10" {
		t.Fatalf("bork-server: %v", pnet["bork-server"])
	}
	if len(pnet["bork-666"]) != 1 || pnet["bork-666"][0] != "10.5.0.15" {
		t.Fatalf("bork-666: %v", pnet["bork-666"])
	}
	if _, ok := pnet["BORK"]; ok {
		t.Fatalf("bork-таблица попала в pnet: %v", pnet)
	}

	bork := parsePeers(broken, []string{"bork"})
	if len(bork) != 1 || bork["BORK"][0] != "10.126.126.1" {
		t.Fatalf("bork: %v", bork)
	}

	if got := parsePeers(broken, []string{"nonexistent"}); len(got) != 0 {
		t.Fatalf("чужая сеть: %v", got)
	}
}

func TestParsePeersSingleNetworkBuffered(t *testing.T) {
	// сервер с одной сетью: одна таблица и один отложенный заголовок
	const single = `| ipv4         | hostname       | cost  | lat(ms) | loss | rx      | tx       | tunnel | NAT            | version        |
|--------------|----------------|-------|---------|------|---------|----------|--------|----------------|----------------|
| 10.5.0.3/24  | w-france       | Local | -       | -    | -       | -        | -      | PortRestricted | 2.4.5-4c4d172e |
| 10.5.0.7/24  | thestepashich-pc | p2p  | 1.20    | 0.0% | 1.1 MB  | 900 kB   | udp    | Unknown        | 2.4.5-4c4d172e |
| 10.5.0.1/24  | potato-debian  | p2p   | 95.55   | 0.0% | 5.16 MB | 323.21 kB| udp    | Symmetric      | 2.4.5-4c4d172e |
=== Peers for network 'pnet' (port 15888) ===
`
	pnet := parsePeers(single, []string{"pnet"})
	if len(pnet["w-france"]) != 1 || pnet["w-france"][0] != "10.5.0.3" {
		t.Fatalf("w-france: %v", pnet["w-france"])
	}
	if len(pnet["thestepashich-pc"]) != 1 || pnet["thestepashich-pc"][0] != "10.5.0.7" {
		t.Fatalf("thestepashich-pc: %v", pnet["thestepashich-pc"])
	}
	if got := parsePeers(single, []string{"bork"}); len(got) != 0 {
		t.Fatalf("единственная таблица не bork: %v", got)
	}
}

func TestSplitCommand(t *testing.T) {
	got, err := splitCommand(`easytier-cli peer --net "my net"`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"easytier-cli", "peer", "--net", "my net"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestStorage(t *testing.T) {
	st, err := NewStorage(filepath.Join(t.TempDir(), "m.jsonl"), 3)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	for i := 0; i < 20; i++ {
		m := Metric{Ts: now - int64(20-i)*300, CPU: float64(i), Mem: 50, Disk: 60,
			Load: fptr(1.0), Temp: fptr(50), NetOK: true}
		if err := st.Insert(m); err != nil {
			t.Fatal(err)
		}
	}
	if rows := st.Fetch(now - 86400); len(rows) != 20 {
		t.Fatalf("fetch: %d", len(rows))
	}
	st.Insert(Metric{Ts: now - 4*86400, CPU: 1, Mem: 1, Disk: 1, NetOK: true})
	st.Purge()
	if st.Count() != 20 {
		t.Fatalf("после purge: %d (старая запись не удалена)", st.Count())
	}
	// перезагрузка из файла
	st2, err := NewStorage(st.path, 3)
	if err != nil {
		t.Fatal(err)
	}
	if st2.Count() != 20 {
		t.Fatalf("перезагрузка: %d", st2.Count())
	}
}

func TestExportMetrics(t *testing.T) {
	st, err := NewStorage(filepath.Join(t.TempDir(), "m.jsonl"), 3)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	for i := 0; i < 30; i++ {
		st.Insert(Metric{Ts: now - int64(30-i)*300, CPU: 30 + float64(i), Mem: 40,
			Disk: 50, Load: fptr(1.0), Temp: fptr(45), RxMbps: fptr(8), TxMbps: fptr(4),
			Occ: fptr(12), NetOK: true})
	}
	payload, filename, span, err := exportMetrics(st, "bork-server", time.Now().Add(-3*24*time.Hour).Unix())
	if err != nil {
		t.Fatal(err)
	}
	if filename != "metrics_bork-server_"+time.Now().Format("20060102")+".json" {
		t.Fatalf("filename: %s", filename)
	}
	var p struct {
		Host      string  `json:"host"`
		Days      int     `json:"days"`
		SpanHours float64 `json:"span_hours"`
		Metrics   []struct {
			Ts     int64    `json:"ts"`
			CPU    float64  `json:"cpu"`
			Mem    float64  `json:"mem"`
			Disk   float64  `json:"disk"`
			Load   *float64 `json:"load"`
			Temp   *float64 `json:"temp"`
			RxMbps *float64 `json:"rx_mbps"`
			TxMbps *float64 `json:"tx_mbps"`
			Occ    *float64 `json:"occ"`
			NetOK  bool     `json:"net_ok"`
		} `json:"metrics"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		t.Fatal(err)
	}
	if p.Host != "bork-server" || len(p.Metrics) != 30 {
		t.Fatalf("export: %+v", p.Host)
	}
	if math.Abs(p.SpanHours-2.5) > 0.1 {
		t.Fatalf("span_hours: %.1f (ожидали ~2.5)", p.SpanHours)
	}
	if math.Abs(span-2.5) > 0.1 {
		t.Fatalf("span из exportMetrics: %.1f", span)
	}
	if p.Metrics[0].CPU != 30 || !p.Metrics[0].NetOK || p.Metrics[0].Load == nil {
		t.Fatalf("экспортные поля: %+v", p.Metrics[0])
	}
	empty, _ := NewStorage(filepath.Join(t.TempDir(), "e.jsonl"), 3)
	if _, _, _, err := exportMetrics(empty, "x", time.Now().Add(-24*time.Hour).Unix()); err != errNoData {
		t.Fatalf("пустое хранилище должно давать errNoData: %v", err)
	}
}

func TestParsePeriod(t *testing.T) {
	now := time.Now().Unix()
	cases := []struct {
		in   string
		want time.Duration
		ok   bool
	}{
		{"30m", 30 * time.Minute, true},
		{"6h", 6 * time.Hour, true},
		{"2d", 2 * 24 * time.Hour, true},
		{" 3 h ", 3 * time.Hour, true},
		{"2H", 2 * time.Hour, true},
		{"", 0, false},
		{"x", 0, false},
		{"1w", 0, false},
		{"0h", 0, false},
		{"-1d", 0, false},
		{"abc30m", 0, false},
	}
	for _, c := range cases {
		got, ok := parsePeriod(c.in)
		if ok != c.ok {
			t.Fatalf("parsePeriod(%q): ok=%v want %v", c.in, ok, c.ok)
		}
		if !ok {
			continue
		}
		want := now - int64(c.want.Seconds())
		diff := got - want
		if diff < -2 || diff > 2 {
			t.Fatalf("parsePeriod(%q): since=%d want~%d", c.in, got, want)
		}
	}
	if got, ok := parsePeriod("6h"); ok && got > now {
		t.Fatal("since должно быть в прошлом")
	}
}

func TestElection(t *testing.T) {
	cfg := defaultConfig()
	cfg.Priority = map[string]int{"bork-server": 10, "w-france": 0, "p-russia": 5}
	app := &App{cfg: &cfg, name: "bork-server"}
	e := NewElection(app)
	peers := map[string][]string{
		"bork-server": {"10.5.0.10"}, "w-france": {"10.5.0.3"}, "p-russia": {"10.5.0.5"},
	}
	alive := map[string]bool{"bork-server": true, "w-france": true, "p-russia": true}
	e.discover = func(*Config) map[string][]string { return peers }
	e.pinger = func(_ *Election, name string, _ []string) bool { return alive[name] }

	e.tick()
	if e.leader != "w-france" || e.amLeader {
		t.Fatalf("все живы: leader=%s amLeader=%v (ожидали w-france, false)", e.leader, e.amLeader)
	}

	// лидер w-france умер -> failover
	alive["w-france"] = false
	e.lastSeen["w-france"] = time.Now().Add(-time.Hour)
	e.tick()
	if e.leader != "p-russia" || e.amLeader {
		t.Fatalf("w-france мёртв: leader=%s (ожидали p-russia)", e.leader)
	}

	// w-france вернулся -> снова лидер
	alive["w-france"] = true
	e.lastSeen["w-france"] = time.Now().Add(-time.Hour)
	e.tick()
	if e.leader != "w-france" {
		t.Fatalf("w-france вернулся: leader=%s", e.leader)
	}

	// все умерли кроме bork -> bork лидер
	alive["w-france"], alive["p-russia"] = false, false
	e.lastSeen["w-france"] = time.Now().Add(-time.Hour)
	e.lastSeen["p-russia"] = time.Now().Add(-time.Hour)
	e.tick()
	if e.leader != "bork-server" || !e.amLeader {
		t.Fatalf("один bork жив: leader=%s amLeader=%v", e.leader, e.amLeader)
	}

	// stepdown -> backoff
	e.StepDown()
	e.tick()
	if e.amLeader {
		t.Fatal("во время backoff не должен быть лидером")
	}
	e.downUntil = time.Time{}
	e.tick()
	if !e.amLeader {
		t.Fatal("после backoff снова лидер")
	}

	// без peers -> сам себе лидер
	peers2 := map[string][]string{}
	e2 := NewElection(app)
	e2.discover = func(*Config) map[string][]string { return peers2 }
	e2.tick()
	if !e2.amLeader {
		t.Fatal("без peers должен быть лидером")
	}
}

func TestAgentHTTP(t *testing.T) {
	cfg := defaultConfig()
	cfg.HTTP = HTTPConfig{BindHost: "127.0.0.1", Port: 0, AuthToken: "sec"}
	cfg.Storage = StorageConfig{Path: filepath.Join(t.TempDir(), "m.jsonl"), RetentionDays: 3}
	st, err := NewStorage(cfg.Storage.Path, 3)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	for i := 0; i < 10; i++ {
		st.Insert(Metric{Ts: now - int64(10-i)*300, CPU: 10, Mem: 20, Disk: 30, NetOK: true})
	}
	f := &fakeTG{}
	app := &App{cfg: &cfg, state: NewState(filepath.Join(t.TempDir(), "st.json")),
		sender: f, storage: st, name: "bork-server"}
	ag := NewAgent(app)
	if err := ag.Start(); err != nil {
		t.Fatal(err)
	}
	base := "http://" + ag.Addr().String()

	resp, err := http.Get(base + "/health")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("без токена: %d", resp.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodGet, base+"/health", nil)
	req.Header.Set("X-Auth-Token", "sec")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("с токеном: %d", resp.StatusCode)
	}

	body, _ := json.Marshal(map[string]int64{"chat_id": 777})
	req, _ = http.NewRequest(http.MethodPost, base+"/report", strings.NewReader(string(body)))
	req.Header.Set("X-Auth-Token", "sec")
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("report: %d", resp.StatusCode)
	}
	time.Sleep(300 * time.Millisecond)
	if len(f.photos) != 1 || !strings.HasPrefix(f.photos[0], "bork-server: метрики за") {
		t.Fatalf("фото-график не отправлен: %v", f.photos)
	}
}

func TestMaybeSend(t *testing.T) {
	cfg := defaultConfig()
	cfg.Telegram.AlertCooldownMin = 60
	cfg.Telegram.ConfirmChecks = 3
	f := &fakeTG{}
	state := NewState(filepath.Join(t.TempDir(), "st.json"))

	// первые три проверки с проблемой — молчание, уведомление на четвёртой
	for i := 0; i < 3; i++ {
		maybeSend(&cfg, state, f, []string{"CPU 100%"}, "h1")
		if len(f.texts) != 0 {
			t.Fatalf("алерт до подтверждения: %v", f.texts)
		}
	}
	maybeSend(&cfg, state, f, []string{"CPU 100%"}, "h1")
	if len(f.texts) != 1 || !strings.Contains(f.texts[0], "CPU 100%") {
		t.Fatalf("алерт после подтверждения не отправлен: %v", f.texts)
	}
	// после отправки счётчик сбрасывается, до cooldown повтор не шлём
	maybeSend(&cfg, state, f, []string{"CPU 100%"}, "h1")
	if len(f.texts) != 1 {
		t.Fatalf("повторный алерт до cooldown: %v", f.texts)
	}
	state.Update(func(d *StateData) {
		d.LastAlertTs = float64(time.Now().Unix() - 7200)
		d.ProblemStreak = 0
	})
	// после cooldown снова нужны три подтверждения
	for i := 0; i < 3; i++ {
		maybeSend(&cfg, state, f, []string{"CPU 100%"}, "h1")
		if len(f.texts) != 1 {
			t.Fatalf("алерт до подтверждения (повтор): %v", f.texts)
		}
	}
	maybeSend(&cfg, state, f, []string{"CPU 100%"}, "h1")
	if len(f.texts) != 2 {
		t.Fatalf("алерт после cooldown: %v", f.texts)
	}
	// восстановление (тоже за cooldown)
	maybeSend(&cfg, state, f, nil, "h1")
	if len(f.texts) != 2 {
		t.Fatalf("recovery раньше cooldown: %v", f.texts)
	}
	st := state.Get()
	if !st.ProblemActive {
		t.Fatal("проблема должна оставаться активной до recovery")
	}
	state.Update(func(d *StateData) { d.LastAlertTs = float64(time.Now().Unix() - 7200) })
	maybeSend(&cfg, state, f, nil, "h1")
	if len(f.texts) != 3 || !strings.Contains(f.texts[2], "Состояние в норме") {
		t.Fatalf("recovery не отправлен: %v", f.texts)
	}
	st = state.Get()
	if st.ProblemActive {
		t.Fatal("problem_active должен сброситься")
	}
	if st.ProblemStreak != 0 {
		t.Fatalf("problem_streak должен сброситься, а он %d", st.ProblemStreak)
	}
}

func TestLoadConfigPartial(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
telegram:
  bot_token: "tok123"
backends:
  socks5:
    host: "10.5.0.1"
thresholds:
  cpu: 95
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Telegram.BotToken != "tok123" {
		t.Fatalf("bot_token: %q", cfg.Telegram.BotToken)
	}
	if cfg.Backends.Socks5.Host != "10.5.0.1" {
		t.Fatalf("socks5 host: %q", cfg.Backends.Socks5.Host)
	}
	if cfg.Backends.Socks5.Port != 1080 {
		t.Fatalf("socks5 port должен быть default 1080: %d", cfg.Backends.Socks5.Port)
	}
	if cfg.Thresholds.CPU == nil || *cfg.Thresholds.CPU != 95 {
		t.Fatalf("cpu threshold: %v", cfg.Thresholds.CPU)
	}
	if cfg.Thresholds.Mem == nil || *cfg.Thresholds.Mem != 90 {
		t.Fatalf("mem threshold default: %v", cfg.Thresholds.Mem)
	}
	if cfg.Thresholds.Load != nil {
		t.Fatalf("load threshold default должен быть nil: %v", *cfg.Thresholds.Load)
	}
	if len(cfg.CheckIntervalMin) != 2 {
		t.Fatalf("check_interval default: %v", cfg.CheckIntervalMin)
	}
}

func fptr(v float64) *float64 {
	return &v
}

func TestSkipIface(t *testing.T) {
	skip := []string{"lo", "docker0", "veth2512ef4", "br-65a2ba6fe330", "virbr0",
		"tun2", "tap0", "wg0", "tailscale0", "utun4", "vmnet8"}
	keep := []string{"enp3s0", "eth0", "ens1", "eno1", "enx0c3790a3b2f1"}
	for _, n := range skip {
		if !skipIface(n) {
			t.Fatalf("интерфейс %s должен быть в списке пропускаемых", n)
		}
	}
	for _, n := range keep {
		if skipIface(n) {
			t.Fatalf("физический интерфейс %s не должен пропускаться", n)
		}
	}
}

func TestDetectInterfacePrefersPhysical(t *testing.T) {
	const proc = `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 100 1 0 0 0 0 0 0 100 1 0 0 0 0 0 0
enp3s0: 500 1 0 0 0 0 0 0 500 1 0 0 0 0 0 0
   wg0: 9999999 1 0 0 0 0 0 0 9999999 1 0 0 0 0 0 0
`
	iface, ok := detectInterfaceFrom(proc)
	if !ok || iface != "enp3s0" {
		t.Fatalf("ожидали enp3s0 (wg0 с большим трафиком должен пропускаться), got %q ok=%v", iface, ok)
	}
}

func TestDoCheck(t *testing.T) {
	cfg := defaultConfig()
	cfg.InternetCheck.Hosts = nil
	st := NewState(filepath.Join(t.TempDir(), "s.json"))
	f := &fakeTG{}
	chat := int64(42)
	app := &App{cfg: &cfg, sender: f, name: "bork-server", collector: NewCollector(&cfg, st)}
	doCheck(app, &chat)
	if len(f.texts) != 1 {
		t.Fatalf("ожидали одно сообщение, got %d", len(f.texts))
	}
	msg := f.texts[0]
	for _, want := range []string{"bork-server", "CPU:", "Память:", "Диск:"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("сообщение не содержит %q:\n%s", want, msg)
		}
	}
	if !strings.Contains(msg, "/") || !strings.Contains(msg, "%") {
		t.Fatalf("нет абсолютного размера/процентов:\n%s", msg)
	}
}

func TestParsePeersNoHeaders(t *testing.T) {
	text := `| ipv4 | hostname | cost |
|------|----------|------|
| 10.5.0.1/24 | potato-debian | Local |
| 10.5.0.3/24 | w-france | p2p |
`
	p := parsePeers(text, []string{"pnet"})
	if len(p) != 2 || p["potato-debian"][0] != "10.5.0.1" || p["w-france"][0] != "10.5.0.3" {
		t.Fatalf("таблицы без заголовков должны учитываться: %v", p)
	}
}

func TestElectionSelfAlwaysAlive(t *testing.T) {
	cfg := defaultConfig()
	app := &App{name: "bork-server", cfg: &cfg}
	e := NewElection(app)
	e.discover = func(*Config) map[string][]string {
		return map[string][]string{"isabelline-gahnospinel8936": {"10.5.0.11"}}
	}
	e.pinger = func(*Election, string, []string) bool { return false }
	e.tick()
	if !e.AmLeader() || e.Leader() != "bork-server" {
		t.Fatalf("себя должны считать живым: leader=%q amLeader=%v", e.Leader(), e.AmLeader())
	}
}

func TestSizeStr(t *testing.T) {
	cases := []struct {
		b    float64
		want string
	}{
		{512, "512 Б"},
		{5 * 1024 * 1024, "5 МБ"},
		{2.5 * 1024 * 1024 * 1024, "2.5 ГБ"},
		{900 * 1024 * 1024 * 1024, "900.0 ГБ"},
	}
	for _, c := range cases {
		if got := sizeStr(c.b); got != c.want {
			t.Fatalf("sizeStr(%v) = %q, want %q", c.b, got, c.want)
		}
	}
}

func TestCommandReportUnknown(t *testing.T) {
	cfg := defaultConfig()
	f := &fakeTG{}
	app := &App{cfg: &cfg, sender: f, name: "bork-server"}
	p := &CommandPoller{app: app, discover: func(*Config) map[string][]string {
		return map[string][]string{"bork-server": {"10.5.0.10"}}
	}}
	p.handle(Update{Message: &Message{Chat: &Chat{ID: 1}, Text: "/report nope"}})
	if len(f.texts) != 1 || !strings.Contains(f.texts[0], "не найден") {
		t.Fatalf("ответ про неизвестный сервер: %v", f.texts)
	}
	if !strings.HasPrefix(f.texts[0], "[bork-server] ") {
		t.Fatalf("в ответе нет имени отвечающего сервера: %v", f.texts)
	}
	f.texts = nil
	p.handle(Update{Message: &Message{Chat: &Chat{ID: 1}, Text: "/report@bot all"}})
	if len(f.texts) != 1 || !strings.Contains(f.texts[0], "Отчёты запрошены") {
		t.Fatalf("report all: %v", f.texts)
	}
	f.texts = nil
	p.handle(Update{Message: &Message{Chat: &Chat{ID: 1}, Text: "/check all"}})
	if len(f.texts) != 1 || !strings.Contains(f.texts[0], "Проверка запрошена") {
		t.Fatalf("check all: %v", f.texts)
	}
	_ = fmt.Sprintf("%v", cfg.Priority)
}
