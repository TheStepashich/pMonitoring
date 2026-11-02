package main

import (
	"encoding/json"
	"os"
	"sync"
)

type NetState struct {
	Iface string `json:"iface"`
	RX    uint64 `json:"rx"`
	TX    uint64 `json:"tx"`
	Ts    int64  `json:"ts"`
}

type StateData struct {
	LastAlertTs   float64   `json:"last_alert_ts,omitempty"`
	ProblemActive bool      `json:"problem_active"`
	ProblemStreak int       `json:"problem_streak,omitempty"`
	Net           *NetState `json:"net"`
}

type State struct {
	mu   sync.Mutex
	path string
	data StateData
}

func NewState(path string) *State {
	s := &State{path: path}
	s.load()
	return s
}

func (s *State) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	_ = json.Unmarshal(data, &s.data)
}

func (s *State) Get() StateData {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data
}

func (s *State) Update(fn func(*StateData)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.data)
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, s.mustMarshal(), 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, s.path)
}

func (s *State) mustMarshal() []byte {
	b, err := json.Marshal(s.data)
	if err != nil {
		return []byte("{}")
	}
	return b
}
