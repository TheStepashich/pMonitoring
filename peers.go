package main

import (
	"bufio"
	"context"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	networkRe  = regexp.MustCompile(`=== Peers for network '([^']+)'`)
	hostCharRe = regexp.MustCompile(`[A-Za-z0-9]`)
)

// parsePeers разбирает вывод easytier/pnet.
//
// easytier печатает для каждой сети таблицу, начинающуюся строкой
// "| ipv4 | hostname ... |". pnet (Python-обёртка) печатает перед таблицей
// заголовок "=== Peers for network 'X' ===". При выводе в pipe (не TTY) pnet
// буферизует эти заголовки и печатает их после таблиц, поэтому таблицы и
// заголовки могут идти в любом порядке. Порядок сетей внутри потока при этом
// сохраняется, поэтому i-я таблица соответствует i-му заголовку в обоих
// случаях.
func parsePeers(text string, onlyNetworks []string) map[string][]string {
	type table struct{ rows [][2]string }
	var tables []table
	var netNames []string
	cur := -1
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if m := networkRe.FindStringSubmatch(line); m != nil {
			netNames = append(netNames, m[1])
			continue
		}
		if !strings.HasPrefix(line, "|") {
			continue
		}
		parts := []string{}
		for _, p := range strings.Split(strings.Trim(line, "|"), "|") {
			parts = append(parts, strings.TrimSpace(p))
		}
		if len(parts) < 2 {
			continue
		}
		ipv4, hostname := parts[0], parts[1]
		if ipv4 == "ipv4" && hostname == "hostname" {
			tables = append(tables, table{})
			cur++
			continue
		}
		if hostname == "" || hostname == "ipv4" || hostname == "hostname" {
			continue
		}
		if !hostCharRe.MatchString(hostname) {
			continue
		}
		if ipv4 == "" {
			continue
		}
		ip := strings.SplitN(ipv4, "/", 2)[0]
		if cur < 0 {
			tables = append(tables, table{})
			cur++
		}
		tables[cur].rows = append(tables[cur].rows, [2]string{ip, hostname})
	}

	peers := map[string]map[string]bool{}
	addRow := func(ip, host string) {
		if peers[host] == nil {
			peers[host] = map[string]bool{}
		}
		peers[host][ip] = true
	}
	for i, tb := range tables {
		name := ""
		if i < len(netNames) {
			name = netNames[i]
		}
		if onlyNetworks != nil {
			if name != "" {
				if !contains(onlyNetworks, name) {
					continue
				}
			} else if len(netNames) > 0 {
				// заголовок для таблицы потерялся, но заголовки в выводе
				// есть — такую таблицу сети не сопоставить, пропускаем
				continue
			}
			// name=="" и заголовков нет вовсе: pnet их не печатал,
			// считаем таблицу принадлежащей настроенной сети
		}
		for _, r := range tb.rows {
			addRow(r[0], r[1])
		}
	}
	return collect(peers)
}

func collect(peers map[string]map[string]bool) map[string][]string {
	out := map[string][]string{}
	for name, ips := range peers {
		list := make([]string, 0, len(ips))
		for ip := range ips {
			list = append(list, ip)
		}
		sort.Strings(list)
		out[name] = list
	}
	return out
}

func discoverPeers(cfg *Config) map[string][]string {
	cmd, err := splitCommand(cfg.Easytier.Command)
	if err != nil || len(cmd) == 0 {
		logWarn("easytier-cli: неверная команда: %v", err)
		return map[string][]string{}
	}
	ctx, cancel := context.WithTimeout(context.Background(),
		time.Duration(cfg.Easytier.TimeoutSec)*time.Second)
	defer cancel()
	proc := exec.CommandContext(ctx, cmd[0], cmd[1:]...)
	out, err := proc.Output()
	if err != nil {
		logWarn("easytier-cli: %v", err)
		return map[string][]string{}
	}
	var networks []string
	if len(cfg.Easytier.Networks) > 0 {
		networks = cfg.Easytier.Networks
	}
	return parsePeers(string(out), networks)
}

func splitCommand(s string) ([]string, error) {
	var args []string
	var cur strings.Builder
	quote := rune(0)
	sc := bufio.NewScanner(strings.NewReader(s))
	sc.Split(bufio.ScanRunes)
	for sc.Scan() {
		r := rune(sc.Text()[0])
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
			continue
		}
		switch r {
		case '\'', '"':
			quote = r
		case ' ', '\t':
			if cur.Len() > 0 {
				args = append(args, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		args = append(args, cur.String())
	}
	return args, nil
}
