package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"time"
)

var errNoData = errors.New("нет данных")

type Storage struct {
	mu        sync.Mutex
	path      string
	retention int // дней
	rows      []Metric
}

func NewStorage(path string, retentionDays int) (*Storage, error) {
	s := &Storage{path: path, retention: retentionDays}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Storage) load() error {
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	cutoff := time.Now().Add(-time.Duration(s.retention) * 24 * time.Hour).Unix()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var m Metric
		if json.Unmarshal(line, &m) != nil {
			continue
		}
		if m.Ts >= cutoff {
			s.rows = append(s.rows, m)
		}
	}
	return sc.Err()
}

func (s *Storage) Insert(m Metric) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	line, err := json.Marshal(m)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		return err
	}
	f.Close()
	s.rows = append(s.rows, m)
	return nil
}

func (s *Storage) Purge() {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-time.Duration(s.retention) * 24 * time.Hour).Unix()
	kept := s.rows[:0]
	for _, m := range s.rows {
		if m.Ts >= cutoff {
			kept = append(kept, m)
		}
	}
	if len(kept) == len(s.rows) {
		return
	}
	s.rows = kept
	tmp := s.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	bw := bufio.NewWriter(f)
	for _, m := range s.rows {
		if line, err := json.Marshal(m); err == nil {
			bw.Write(append(line, '\n'))
		}
	}
	bw.Flush()
	f.Close()
	_ = os.Rename(tmp, s.path)
}

func (s *Storage) Fetch(since int64) []Metric {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Metric, 0, len(s.rows))
	for _, m := range s.rows {
		if m.Ts >= since {
			out = append(out, m)
		}
	}
	return out
}

func (s *Storage) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.rows)
}
