package graph

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
)

type metric struct {
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
}

type series struct {
	name  string
	color string
	vals  []float64 // math.NaN() — пропуск точки
	note  string    // текущее значение для легенды
}

type panel struct {
	title  string
	series []series
}

type Chart struct {
	host    string
	spanTxt string
	ts      []int64
	panels  [4]panel
}

// Load читает файл и определяет формат по содержимому.
func Load(path string) (*Chart, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Parse определяет формат по содержимому: экспорт от бота
// ({"host":...,"metrics":[...]}) или сырой metrics.jsonl (строка на метрику).
func Parse(data []byte) (*Chart, error) {
	var top struct {
		Host    string   `json:"host"`
		Metrics []metric `json:"metrics"`
	}
	if err := json.Unmarshal(data, &top); err == nil && top.Metrics != nil {
		if len(top.Metrics) == 0 {
			return nil, errors.New("в файле нет метрик")
		}
		return build(top.Host, top.Metrics), nil
	}
	rows, err := scanRows(data)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errors.New("в файле нет метрик")
	}
	return build("", rows), nil
}

func scanRows(data []byte) ([]metric, error) {
	rows := []metric{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var m metric
		if json.Unmarshal(line, &m) == nil && m.Ts != 0 {
			rows = append(rows, m)
		}
	}
	return rows, sc.Err()
}

func build(host string, rows []metric) *Chart {
	if host == "" {
		host = "metrics"
	}
	c := &Chart{host: host}
	for _, r := range rows {
		c.ts = append(c.ts, r.Ts)
	}
	cpu := make([]float64, len(rows))
	mem := make([]float64, len(rows))
	disk := make([]float64, len(rows))
	for i, r := range rows {
		cpu[i] = r.CPU
		mem[i] = r.Mem
		disk[i] = r.Disk
	}
	c.panels[0] = panel{title: "CPU %", series: []series{{name: "cpu", color: "#e74c3c", vals: cpu}}}
	c.panels[1] = panel{title: "Память / диск %", series: []series{
		{name: "mem %", color: "#1f77b4", vals: mem, note: usageNote(mem, rows, func(m metric) float64 { return m.MemUsed }, func(m metric) float64 { return m.MemTotal })},
		{name: "disk %", color: "#ff7f0e", vals: disk, note: usageNote(disk, rows, func(m metric) float64 { return m.DiskUsed }, func(m metric) float64 { return m.DiskTotal })},
	}}
	rxVals := ptrs(rows, func(m metric) *float64 { return m.RxMbps })
	txVals := ptrs(rows, func(m metric) *float64 { return m.TxMbps })
	c.panels[2] = panel{title: "Сетевой трафик", series: []series{
		{name: "rx, Мбит/с", color: "#2ca02c", vals: rxVals, note: rateNote(rxVals)},
		{name: "tx, Мбит/с", color: "#d62728", vals: txVals, note: rateNote(txVals)},
	}}
	c.panels[3] = panel{title: "Нагрузка / температура", series: []series{
		{name: "load", color: "#17becf", vals: ptrs(rows, func(m metric) *float64 { return m.Load })},
		{name: "temp °C", color: "#e377c2", vals: ptrs(rows, func(m metric) *float64 { return m.Temp })},
	}}

	minTs, maxTs := rows[0].Ts, rows[len(rows)-1].Ts
	c.spanTxt = spanText(float64(maxTs-minTs) / 3600.0)
	return c
}

// Ts возвращает метки времени рядов.
func (c *Chart) Ts() []int64 { return c.ts }

// lastIdx возвращает индекс последнего валидного значения.
func lastIdx(vals []float64) (int, bool) {
	for i := len(vals) - 1; i >= 0; i-- {
		if !math.IsNaN(vals[i]) {
			return i, true
		}
	}
	return 0, false
}

// lastNonNaN возвращает последнее валидное значение ряда (для легенды).
func lastNonNaN(vals []float64) (float64, bool) {
	i, ok := lastIdx(vals)
	if !ok {
		return 0, false
	}
	return vals[i], true
}

// usageNote формирует подпись легенды для процентов использования:
// " · 25.5% · 2.6/16.0 ГБ". total <= 0 (старые файлы) — подпись пустая.
func usageNote(vals []float64, rows []metric, usedOf, totalOf func(metric) float64) string {
	i, ok := lastIdx(vals)
	if !ok || i >= len(rows) {
		return ""
	}
	total := totalOf(rows[i])
	if total <= 0 {
		return ""
	}
	return fmt.Sprintf(" · %.1f%% · %s/%s", vals[i], sizeBytes(usedOf(rows[i])), sizeBytes(total))
}

// rateNote — подпись текущей скорости: " · 1.20 Мбит/с".
func rateNote(vals []float64) string {
	v, ok := lastNonNaN(vals)
	if !ok {
		return ""
	}
	return fmt.Sprintf(" · %.2f Мбит/с", v)
}

func sizeBytes(b float64) string {
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

func ptrs(rows []metric, get func(metric) *float64) []float64 {
	out := make([]float64, len(rows))
	for i, m := range rows {
		if v := get(m); v != nil {
			out[i] = *v
		} else {
			out[i] = math.NaN()
		}
	}
	return out
}

func (c *Chart) Thin(step int) {
	if step <= 1 {
		return
	}
	ts := make([]int64, 0, (len(c.ts)+step-1)/step)
	for i := 0; i < len(c.ts); i += step {
		ts = append(ts, c.ts[i])
	}
	c.ts = ts
	for pi := range c.panels {
		for si := range c.panels[pi].series {
			s := &c.panels[pi].series[si]
			vals := make([]float64, 0, len(ts))
			for i := 0; i < len(s.vals); i += step {
				vals = append(vals, s.vals[i])
			}
			s.vals = vals
		}
	}
}

func spanText(hours float64) string {
	if hours < 1 {
		return fmt.Sprintf("%d мин", int(math.Max(1, math.Round(hours*60))))
	}
	if hours < 48 {
		return fmt.Sprintf("%d ч", int(math.Round(hours)))
	}
	return fmt.Sprintf("%d дн", int(math.Round(hours/24)))
}
