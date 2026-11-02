package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

type App struct {
	cfg       *Config
	state     *State
	sender    Telegram
	storage   *Storage
	name      string
	election  *Election
	collector *Collector
}

type Agent struct {
	app    *App
	server *http.Server
	ln     net.Listener
}

func NewAgent(app *App) *Agent {
	ag := &Agent{app: app}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", ag.handleHealth)
	mux.HandleFunc("/elect", ag.handleHealth)
	mux.HandleFunc("/report", ag.handleReport)
	mux.HandleFunc("/check", ag.handleCheck)
	ag.server = &http.Server{
		Addr:    fmt.Sprintf("%s:%d", app.cfg.HTTP.BindHost, app.cfg.HTTP.Port),
		Handler: mux,
	}
	return ag
}

func (ag *Agent) Start() error {
	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", ag.app.cfg.HTTP.BindHost, ag.app.cfg.HTTP.Port))
	if err != nil {
		return err
	}
	ag.ln = ln
	logInfo("HTTP-агент слушает %s", ln.Addr())
	go func() {
		if err := ag.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			logError("HTTP-агент упал: %v", err)
		}
	}()
	return nil
}

func (ag *Agent) Addr() net.Addr {
	return ag.ln.Addr()
}

func (ag *Agent) authOK(r *http.Request) bool {
	token := ag.app.cfg.HTTP.AuthToken
	return token == "" || r.Header.Get("X-Auth-Token") == token
}

func (ag *Agent) writeJSON(w http.ResponseWriter, code int, obj any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(obj)
}

func (ag *Agent) handleHealth(w http.ResponseWriter, r *http.Request) {
	if !ag.authOK(r) {
		ag.writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "auth"})
		return
	}
	resp := map[string]any{
		"ok":       true,
		"name":     ag.app.name,
		"priority": ag.app.cfg.Priority[ag.app.name],
		"ts":       time.Now().Unix(),
	}
	if ag.app.election != nil {
		resp["leader"] = ag.app.election.AmLeader()
		resp["leader_name"] = ag.app.election.Leader()
	}
	ag.writeJSON(w, http.StatusOK, resp)
}

func (ag *Agent) handleReport(w http.ResponseWriter, r *http.Request) {
	if !ag.authOK(r) {
		ag.writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "auth"})
		return
	}
	if r.Method != http.MethodPost {
		ag.writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "not found"})
		return
	}
	var chatID *int64
	since := int64(0)
	body, err := io.ReadAll(r.Body)
	if err == nil && len(body) > 0 {
		var req struct {
			ChatID *int64 `json:"chat_id"`
			Since  int64  `json:"since"`
		}
		if json.Unmarshal(body, &req) == nil {
			chatID = req.ChatID
			since = req.Since
		}
	}
	go doReport(ag.app, chatID, since)
	ag.writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": ag.app.name, "async": true})
}

func (ag *Agent) handleCheck(w http.ResponseWriter, r *http.Request) {
	if !ag.authOK(r) {
		ag.writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "auth"})
		return
	}
	if r.Method != http.MethodPost {
		ag.writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "not found"})
		return
	}
	var chatID *int64
	body, err := io.ReadAll(r.Body)
	if err == nil && len(body) > 0 {
		var req struct {
			ChatID *int64 `json:"chat_id"`
		}
		if json.Unmarshal(body, &req) == nil {
			chatID = req.ChatID
		}
	}
	go doCheck(ag.app, chatID)
	ag.writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": ag.app.name, "async": true})
}
