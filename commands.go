package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type CommandPoller struct {
	app      *App
	offset   int64
	discover func(*Config) map[string][]string
}

func (p *CommandPoller) peers() map[string][]string {
	if p.discover != nil {
		return p.discover(p.app.cfg)
	}
	return discoverPeers(p.app.cfg)
}

func (p *CommandPoller) Run() {
	defer func() {
		if r := recover(); r != nil {
			logError("паника в CommandPoller: %v", r)
		}
	}()
	for {
		if p.app.election.AmLeader() && p.app.cfg.Commands {
			updates, err := p.app.sender.GetUpdates(int(p.offset), 25)
			if err == ErrConflict {
				logWarn("409 Conflict, отказываюсь от лидерства")
				p.app.election.StepDown()
				time.Sleep(time.Duration(p.app.cfg.Election.BackoffSec) * time.Second)
				continue
			}
			if err != nil {
				logWarn("getUpdates: %v", err)
				time.Sleep(5 * time.Second)
				continue
			}
			for _, u := range updates {
				if u.UpdateID+1 > p.offset {
					p.offset = u.UpdateID + 1
				}
				p.handle(u)
			}
		} else {
			time.Sleep(2 * time.Second)
		}
	}
}

func (p *CommandPoller) reply(chatID int64, text string) {
	msg := fmt.Sprintf("[%s] %s", p.app.name, text)
	if err := p.app.sender.SendText(msg, &chatID); err != nil {
		logWarn("reply: %v", err)
	}
}

func (p *CommandPoller) handle(u Update) {
	defer func() {
		if r := recover(); r != nil {
			logError("паника при обработке команды: %v", r)
		}
	}()
	if u.Message == nil || u.Message.Chat == nil {
		return
	}
	text := strings.TrimSpace(u.Message.Text)
	chatID := u.Message.Chat.ID
	if !strings.HasPrefix(text, "/") {
		return
	}
	cmd, arg := text, ""
	if idx := strings.IndexByte(text, ' '); idx >= 0 {
		cmd, arg = text[:idx], strings.TrimSpace(text[idx+1:])
	}
	cmd = strings.ToLower(strings.SplitN(cmd, "@", 2)[0])
	switch cmd {
	case "/list":
		p.cmdList(chatID)
	case "/report":
		fields := strings.Fields(arg)
		if len(fields) == 0 {
			p.reply(chatID, usage())
			break
		}
		since := int64(0)
		if len(fields) > 1 {
			var ok bool
			if since, ok = parsePeriod(fields[1]); !ok {
				p.reply(chatID, fmt.Sprintf("Не понимаю период %q (формат: 30m/6h/2d)", fields[1]))
				break
			}
		}
		if fields[0] == "all" {
			p.cmdReportAll(chatID, since)
		} else {
			p.cmdReportOne(chatID, fields[0], since)
		}
	case "/check":
		target := strings.TrimSpace(arg)
		if target == "all" {
			p.cmdCheckAll(chatID)
		} else if target != "" {
			p.cmdCheck(chatID, target)
		} else {
			p.reply(chatID, "Использование: /check <hostname|ip> — один сервер, /check all — все")
		}
	}
}

var periodRe = regexp.MustCompile(`^(\d+)\s*(m|h|d)$`)

func parsePeriod(s string) (int64, bool) {
	m := periodRe.FindStringSubmatch(strings.TrimSpace(strings.ToLower(s)))
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n <= 0 {
		return 0, false
	}
	var d time.Duration
	switch m[2] {
	case "m":
		d = time.Duration(n) * time.Minute
	case "h":
		d = time.Duration(n) * time.Hour
	case "d":
		d = time.Duration(n) * 24 * time.Hour
	}
	return time.Now().Add(-d).Unix(), true
}

func usage() string {
	return "Команды:\n" +
		"/list — список участников сети\n" +
		"/check <hostname|ip> — текущее состояние CPU/память/диск\n" +
		"/check all — состояние всех серверов\n" +
		"/report <hostname|ip> [30m|6h|2d] — отчёт с сервера за период\n" +
		"/report all [30m|6h|2d] — отчёты со всех серверов"
}

func sortedKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func (p *CommandPoller) cmdList(chatID int64) {
	peers := p.peers()
	if len(peers) == 0 {
		p.reply(chatID, "Не удалось получить список peers (easytier-cli недоступен)")
		return
	}
	lines := []string{"Участники сети:"}
	for _, name := range sortedKeys(peers) {
		mark := ""
		if name == p.app.name {
			mark = "  ← этот сервер"
		}
		lines = append(lines, fmt.Sprintf("• %s%s — %s", name, mark, strings.Join(peers[name], ", ")))
	}
	p.reply(chatID, strings.Join(lines, "\n"))
}

// requestAgent отправляет POST-запрос с body на path агента по каждому IP.
func (p *CommandPoller) requestAgent(ips []string, path string, body map[string]any) bool {
	port := p.app.cfg.HTTP.Port
	token := p.app.cfg.HTTP.AuthToken
	client := &http.Client{Timeout: 10 * time.Second}
	data, _ := json.Marshal(body)
	for _, ip := range ips {
		req, err := http.NewRequest(http.MethodPost,
			fmt.Sprintf("http://%s:%d%s", ip, port, path), bytes.NewReader(data))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Auth-Token", token)
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		payload, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			var j struct {
				OK bool `json:"ok"`
			}
			if json.Unmarshal(payload, &j) == nil && j.OK {
				return true
			}
		}
	}
	return false
}

func (p *CommandPoller) requestReport(name string, ips []string, chatID int64, since int64) bool {
	return p.requestAgent(ips, "/report", map[string]any{"chat_id": chatID, "since": since})
}

func (p *CommandPoller) cmdReportOne(chatID int64, target string, since int64) {
	peers := p.peers()
	name, ips := "", []string(nil)
	for n, ipl := range peers {
		if n == target || contains(ipl, target) {
			name, ips = n, ipl
			break
		}
	}
	if name == "" {
		p.reply(chatID, fmt.Sprintf("Сервер %s не найден. Список: /list", target))
		return
	}
	if p.requestReport(name, ips, chatID, since) {
		p.reply(chatID, fmt.Sprintf("Запрос отчёта отправлен на %s", name))
	} else {
		p.reply(chatID, fmt.Sprintf("Не удалось достучаться до %s (%s)", name, strings.Join(ips, ", ")))
	}
}

func (p *CommandPoller) cmdReportAll(chatID int64, since int64) {
	peers := p.peers()
	okList, failList := []string{}, []string{}
	for _, name := range sortedKeys(peers) {
		if p.requestReport(name, peers[name], chatID, since) {
			okList = append(okList, name)
		} else {
			failList = append(failList, name)
		}
	}
	text := fmt.Sprintf("Отчёты запрошены: %d, не удалось: %d", len(okList), len(failList))
	if len(failList) > 0 {
		text += " (" + strings.Join(failList, ", ") + ")"
	}
	p.reply(chatID, text)
}

func (p *CommandPoller) cmdCheckAll(chatID int64) {
	peers := p.peers()
	okList, failList := []string{}, []string{}
	for _, name := range sortedKeys(peers) {
		if p.requestAgent(peers[name], "/check", map[string]any{"chat_id": chatID}) {
			okList = append(okList, name)
		} else {
			failList = append(failList, name)
		}
	}
	text := fmt.Sprintf("Проверка запрошена: %d, не удалось: %d", len(okList), len(failList))
	if len(failList) > 0 {
		text += " (" + strings.Join(failList, ", ") + ")"
	}
	p.reply(chatID, text)
}

func (p *CommandPoller) cmdCheck(chatID int64, target string) {
	peers := p.peers()
	name, ips := "", []string(nil)
	for n, ipl := range peers {
		if n == target || contains(ipl, target) {
			name, ips = n, ipl
			break
		}
	}
	if name == "" {
		p.reply(chatID, fmt.Sprintf("Сервер %s не найден. Список: /list", target))
		return
	}
	if p.requestAgent(ips, "/check", map[string]any{"chat_id": chatID}) {
		p.reply(chatID, fmt.Sprintf("Запрошено состояние %s", name))
	} else {
		p.reply(chatID, fmt.Sprintf("Не удалось достучаться до %s (%s)", name, strings.Join(ips, ", ")))
	}
}
