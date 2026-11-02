package main

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"pmonitoring/graph"
)

func checkIssues(m Metric, cfg *Config) []string {
	t := cfg.Thresholds
	issues := []string{}
	if t.CPU != nil && m.CPU > *t.CPU {
		issues = append(issues, fmt.Sprintf("CPU %.0f%% (порог %.0f%%)", m.CPU, *t.CPU))
	}
	if t.Mem != nil && m.Mem > *t.Mem {
		issues = append(issues, fmt.Sprintf("Память %.0f%% (порог %.0f%%)", m.Mem, *t.Mem))
	}
	if t.Disk != nil && m.Disk > *t.Disk {
		issues = append(issues, fmt.Sprintf("Диск %.0f%% (порог %.0f%%)", m.Disk, *t.Disk))
	}
	if t.Temp != nil && m.Temp != nil && *m.Temp > *t.Temp {
		issues = append(issues, fmt.Sprintf("Температура %.1f°C (порог %.0f°C)", *m.Temp, *t.Temp))
	}
	if t.Load != nil && m.Load != nil && *m.Load > *t.Load {
		issues = append(issues, fmt.Sprintf("Load average %.2f (порог %.2f)", *m.Load, *t.Load))
	}
	if t.ChannelOccupancy != nil && m.Occ != nil && *m.Occ > *t.ChannelOccupancy {
		issues = append(issues, fmt.Sprintf("Загрузка канала %.0f%% (порог %.0f%%)", *m.Occ, *t.ChannelOccupancy))
	}
	if !m.NetOK {
		issues = append(issues, "Нет доступа в интернет")
	}
	return issues
}

func maybeSend(cfg *Config, state *State, sender Telegram, issues []string, host string) {
	cooldown := float64(cfg.Telegram.AlertCooldownMin * 60)
	required := 1 + cfg.Telegram.ConfirmChecks
	if required < 1 {
		required = 1
	}
	now := float64(time.Now().Unix())
	st := state.Get()

	if len(issues) > 0 {
		streak := st.ProblemStreak + 1
		if streak < required || (st.ProblemActive && (now-st.LastAlertTs) < cooldown) {
			state.Update(func(d *StateData) { d.ProblemStreak = streak })
			return
		}
		var b strings.Builder
		b.WriteString("⚠️ ")
		b.WriteString(host)
		b.WriteString("\n")
		for _, i := range issues {
			b.WriteString("• ")
			b.WriteString(i)
			b.WriteString("\n")
		}
		if sender.SendText(strings.TrimRight(b.String(), "\n"), nil) == nil {
			state.Update(func(d *StateData) {
				d.LastAlertTs = now
				d.ProblemActive = true
				d.ProblemStreak = 0
			})
		}
		return
	}

	// проблем нет: сбрасываем счётчик подтверждений, по истечении cooldown шлём recovery
	state.Update(func(d *StateData) {
		d.ProblemStreak = 0
		if d.ProblemActive && (now-d.LastAlertTs) >= cooldown {
			d.ProblemActive = false
			d.LastAlertTs = now
		}
	})
	if st.ProblemActive && (now-st.LastAlertTs) >= cooldown && cfg.Telegram.NotifyRecovery {
		_ = sender.SendText(fmt.Sprintf("✅ %s\nСостояние в норме", host), nil)
	}
}

type exportPayload struct {
	Host      string   `json:"host"`
	Days      int      `json:"days"`
	SpanHours float64  `json:"span_hours"`
	Generated int64    `json:"generated"`
	Metrics   []Metric `json:"metrics"`
}

func exportMetrics(st *Storage, host string, since int64) ([]byte, string, float64, error) {
	rows := st.Fetch(since)
	if len(rows) == 0 {
		return nil, "", 0, errNoData
	}
	span := spanOf(rows)
	p := exportPayload{Host: host, Days: int(math.Round(span / 24)), SpanHours: span,
		Generated: time.Now().Unix(), Metrics: rows}
	b, err := json.Marshal(p)
	if err != nil {
		return nil, "", 0, err
	}
	filename := fmt.Sprintf("metrics_%s_%s.json", host, time.Now().Format("20060102"))
	return b, filename, span, nil
}

func spanOf(rows []Metric) float64 {
	if len(rows) == 0 {
		return 0
	}
	minTs, maxTs := rows[0].Ts, rows[0].Ts
	for _, m := range rows[1:] {
		if m.Ts < minTs {
			minTs = m.Ts
		}
		if m.Ts > maxTs {
			maxTs = m.Ts
		}
	}
	return float64(maxTs-minTs) / 3600.0
}

func spanText(hours float64) string {
	if hours < 1 {
		return fmt.Sprintf("%.0f мин", math.Round(hours*60))
	}
	if hours < 48 {
		return fmt.Sprintf("%.0f ч", math.Round(hours))
	}
	return fmt.Sprintf("%.0f дн", math.Round(hours/24))
}

func doReport(app *App, chatID *int64, since int64) {
	defer func() {
		if r := recover(); r != nil {
			logError("паника в /report: %v", r)
		}
	}()
	if since == 0 {
		since = time.Now().Add(-time.Duration(app.cfg.Storage.RetentionDays) * 24 * time.Hour).Unix()
	}
	payload, filename, span, err := exportMetrics(app.storage, app.name, since)
	if err == errNoData {
		req := spanText(float64(time.Now().Unix()-since) / 3600.0)
		if e := app.sender.SendText(fmt.Sprintf("%s: нет данных за %s", app.name, req), chatID); e != nil {
			logWarn("send text: %v", e)
		}
		return
	}
	if err != nil {
		logError("Не удалось сформировать отчёт %s: %v", app.name, err)
		return
	}
	caption := fmt.Sprintf("%s: метрики за %s", app.name, spanText(span))
	photo, err := renderReportPNG(payload)
	if err != nil {
		logWarn("не удалось отрисовать график %s: %v, шлю JSON", app.name, err)
		if e := app.sender.SendDocument(payload, filename, caption, chatID); e != nil {
			logWarn("send document: %v", e)
		}
		return
	}
	if err := app.sender.SendPhoto(photo, caption, chatID); err != nil {
		logWarn("send photo: %v", err)
	}
}

// renderReportPNG отрисовывает график по сформированному экспорту метрик.
func renderReportPNG(payload []byte) ([]byte, error) {
	c, err := graph.Parse(payload)
	if err != nil {
		return nil, err
	}
	return c.RenderPNG()
}

// doCheck собирает свежие метрики и шлёт текущее состояние CPU/памяти/диска.
func doCheck(app *App, chatID *int64) {
	defer func() {
		if r := recover(); r != nil {
			logError("паника в /check: %v", r)
		}
	}()
	if app.collector == nil {
		logWarn("/check: коллектор не инициализирован")
		return
	}
	m := app.collector.Collect()
	var b strings.Builder
	fmt.Fprintf(&b, "%s — состояние\n", app.name)
	fmt.Fprintf(&b, "CPU: %.1f%%\n", m.CPU)
	fmt.Fprintf(&b, "Память: %s / %s (%.1f%%)\n", sizeStr(m.MemUsed), sizeStr(m.MemTotal), m.Mem)
	fmt.Fprintf(&b, "Диск: %s / %s (%.1f%%)\n", sizeStr(m.DiskUsed), sizeStr(m.DiskTotal), m.Disk)
	if m.Load != nil {
		fmt.Fprintf(&b, "Нагрузка: %.2f\n", *m.Load)
	}
	if m.Temp != nil {
		fmt.Fprintf(&b, "Температура: %.0f°C\n", *m.Temp)
	}
	fmt.Fprintf(&b, "Интернет: %s\n", netStatus(m.NetOK))
	if m.RxMbps != nil && m.TxMbps != nil {
		fmt.Fprintf(&b, "Сеть (%s): rx %.2f Мбит/с, tx %.2f Мбит/с\n", m.Iface, *m.RxMbps, *m.TxMbps)
	}
	if err := app.sender.SendText(b.String(), chatID); err != nil {
		logWarn("send check: %v", err)
	}
}

// sizeStr форматирует размер в байтах: 5.2 ГБ / 780 МБ.
func sizeStr(b float64) string {
	const (
		mb = 1024 * 1024
		gb = 1024 * 1024 * 1024
	)
	switch {
	case b >= gb:
		return fmt.Sprintf("%.1f ГБ", b/gb)
	case b >= mb:
		return fmt.Sprintf("%.0f МБ", b/mb)
	default:
		return fmt.Sprintf("%.0f Б", b)
	}
}
