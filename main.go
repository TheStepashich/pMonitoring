package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"time"
)

func main() {
	configPath := flag.String("config", "config.yaml", "путь к config.yaml")
	once := flag.Bool("once", false, "одна проверка и выход")
	testSend := flag.String("test-send", "", "отправить тестовое сообщение и выйти")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		logFatal("конфиг: %v", err)
	}
	setupLogging(cfg.LogFile)
	name := hostName(cfg)

	if *testSend != "" {
		sender := NewSender(cfg)
		if err := sender.SendText(*testSend, nil); err != nil {
			logError("тест не отправлен: %v", err)
			os.Exit(1)
		}
		logInfo("Тестовое сообщение отправлено")
		return
	}

	state := NewState(cfg.StateFile)
	storage, err := NewStorage(cfg.Storage.Path, cfg.Storage.RetentionDays)
	if err != nil {
		logFatal("storage: %v", err)
	}
	collector := NewCollector(cfg, state)
	sender := NewSender(cfg)
	app := &App{cfg: cfg, state: state, sender: sender, storage: storage, name: name, collector: collector}

	if *once {
		m := collector.Collect()
		if err := storage.Insert(m); err != nil {
			logError("insert: %v", err)
		}
		storage.Purge()
		issues := checkIssues(m, cfg)
		maybeSend(cfg, state, sender, issues, name)
		out, _ := json.Marshal(m)
		logInfo("Результат: %s", string(out))
		return
	}

	agent := NewAgent(app)
	if err := agent.Start(); err != nil {
		logFatal("не удалось запустить HTTP-агент: %v", err)
	}
	election := NewElection(app)
	app.election = election
	go election.Run()
	go (&CommandPoller{app: app}).Run()

	lo, hi := 5, 10
	if len(cfg.CheckIntervalMin) >= 2 {
		lo, hi = cfg.CheckIntervalMin[0], cfg.CheckIntervalMin[1]
	}
	if lo < 1 {
		lo = 1
	}
	if hi < lo {
		hi = lo
	}
	logInfo("Запущен мониторинг на %s, интерфейс %s", name, collector.iface)

	for {
		m := collector.Collect()
		if err := storage.Insert(m); err != nil {
			logError("insert: %v", err)
		}
		storage.Purge()
		logInfo("cpu=%2.0f%% mem=%2.0f%% disk=%2.0f%% temp=%s load=%s net=%s rx=%s tx=%s occ=%s",
			m.CPU, m.Mem, m.Disk,
			fStr(m.Temp, "%.1f", "-"),
			fStr(m.Load, "%.2f", "-"),
			netStatus(m.NetOK),
			fStr(m.RxMbps, "%.2f", "-"),
			fStr(m.TxMbps, "%.2f", "-"),
			fPct(m.Occ),
		)
		issues := checkIssues(m, cfg)
		maybeSend(cfg, state, sender, issues, name)
		delay := time.Duration((float64(lo) + rand.Float64()*float64(hi-lo)) * float64(time.Minute))
		logInfo("Следующая проверка через %.0f мин", delay.Minutes())
		time.Sleep(delay)
	}
}

func hostName(cfg *Config) string {
	if cfg.Name != nil && *cfg.Name != "" {
		return *cfg.Name
	}
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return h
}

func fStr(p *float64, format, none string) string {
	if p == nil {
		return none
	}
	return fmt.Sprintf(format, *p)
}

func fPct(p *float64) string {
	if p == nil {
		return "-"
	}
	return fmt.Sprintf("%.0f%%", *p)
}

func netStatus(ok bool) string {
	if ok {
		return "ok"
	}
	return "DOWN"
}
