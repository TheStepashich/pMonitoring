package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"
)

type Election struct {
	mu        sync.Mutex
	app       *App
	amLeader  bool
	leader    string
	lastSeen  map[string]time.Time
	downUntil time.Time
	discover  func(*Config) map[string][]string
	pinger    func(*Election, string, []string) bool
}

func NewElection(app *App) *Election {
	return &Election{
		app:      app,
		lastSeen: map[string]time.Time{},
		discover: discoverPeers,
		pinger:   (*Election).ping,
	}
}

func (e *Election) AmLeader() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.amLeader
}

func (e *Election) Leader() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.leader
}

func (e *Election) StepDown() {
	e.mu.Lock()
	e.downUntil = time.Now().Add(time.Duration(e.app.cfg.Election.BackoffSec) * time.Second)
	e.amLeader = false
	e.leader = ""
	e.mu.Unlock()
	logWarn("Отказ от лидерства (backoff после 409)")
}

func (e *Election) ping(name string, ips []string) bool {
	port := e.app.cfg.HTTP.Port
	token := e.app.cfg.HTTP.AuthToken
	client := &http.Client{Timeout: 2 * time.Second}
	for _, ip := range ips {
		req, err := http.NewRequest(http.MethodGet,
			fmt.Sprintf("http://%s:%d/health", ip, port), nil)
		if err != nil {
			continue
		}
		req.Header.Set("X-Auth-Token", token)
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			var j struct {
				OK bool `json:"ok"`
			}
			if json.Unmarshal(body, &j) == nil && j.OK {
				return true
			}
		}
	}
	return false
}

func (e *Election) tick() {
	cfg := e.app.cfg
	now := time.Now()
	heart := time.Duration(cfg.Election.FailoverSec) * time.Second

	e.mu.Lock()
	if now.Before(e.downUntil) {
		if e.amLeader {
			logWarn("Отказ от лидерства (backoff после 409)")
			e.amLeader = false
		}
		e.mu.Unlock()
		return
	}

	peers := e.discover(cfg)
	type cand struct {
		pri  int
		name string
	}
	// себя считаем живым всегда: процесс запущен, а пинг к собственному адресу
	// через easytier может петлять, или себя может не быть в списке peers
	alive := []cand{{cfg.Priority[e.app.name], e.app.name}}
	for name, ips := range peers {
		if name == e.app.name {
			continue
		}
		seen, ok := e.lastSeen[name]
		if !ok || now.Sub(seen) > heart {
			if e.pinger(e, name, ips) {
				e.lastSeen[name] = now
			}
		}
		seen, ok = e.lastSeen[name]
		if ok && now.Sub(seen) <= heart {
			alive = append(alive, cand{cfg.Priority[name], name})
		}
	}
	sort.Slice(alive, func(i, j int) bool {
		if alive[i].pri != alive[j].pri {
			return alive[i].pri < alive[j].pri
		}
		return alive[i].name < alive[j].name
	})
	leader := ""
	if len(alive) > 0 {
		leader = alive[0].name
	}
	e.leader = leader
	was := e.amLeader
	e.amLeader = leader == e.app.name
	e.mu.Unlock()

	if was && !e.amLeader {
		logInfo("Лидером стал %s", leader)
	} else if !was && e.amLeader {
		logInfo("Я теперь лидер")
	}
}

func (e *Election) Run() {
	hb := e.app.cfg.Election.HeartbeatSec
	if hb < 1 {
		hb = 1
	}
	for {
		e.tick()
		time.Sleep(time.Duration(hb) * time.Second)
	}
}
