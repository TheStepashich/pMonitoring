package graph

import (
	"bytes"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func fptr(v float64) *float64 { return &v }

func TestParseExport(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "m.json")
	doc := `{"host":"bork-server","days":3,"span_hours":2.5,"generated":175,"metrics":[
		{"ts":100,"cpu":30,"mem":40,"disk":50,"load":1.0,"temp":45.0,"rx_mbps":8,"tx_mbps":4,"occ":12,"net_ok":true},
		{"ts":200,"cpu":31,"mem":41,"disk":51,"load":1.1,"temp":45.5,"rx_mbps":9,"tx_mbps":5,"occ":13,"net_ok":true}
	]}`
	if err := os.WriteFile(p, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.host != "bork-server" {
		t.Fatalf("host: %s", c.host)
	}
	if len(c.ts) != 2 || c.ts[0] != 100 || c.ts[1] != 200 {
		t.Fatalf("ts: %v", c.ts)
	}
	p0 := c.panels[0].series[0]
	if p0.name != "cpu" || p0.color != "#e74c3c" || p0.vals[0] != 30 {
		t.Fatalf("cpu series: %+v", p0)
	}
	if len(c.panels[1].series) != 2 {
		t.Fatal("mem/disk серий должно быть 2")
	}
	if c.panels[2].series[0].vals[0] != 8 {
		t.Fatalf("rx: %v", c.panels[2].series[0].vals)
	}
	if c.panels[3].series[1].name != "temp °C" {
		t.Fatalf("temp: %v", c.panels[3].series)
	}
}

func TestParseJSONL(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "metrics.jsonl")
	var sb strings.Builder
	for i := 0; i < 5; i++ {
		sb.WriteString(`{"ts":` + strconv.FormatInt(1000+int64(i)*100, 10) + `,"cpu":10,"mem":20,"disk":30,"net_ok":true}` + "\n")
	}
	sb.WriteString("junk line\n")
	if err := os.WriteFile(p, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.ts) != 5 {
		t.Fatalf("ts: %d", len(c.ts))
	}
	if c.host != "metrics" {
		t.Fatalf("host по умолчанию: %s", c.host)
	}
}

func TestParseEmpty(t *testing.T) {
	p := filepath.Join(t.TempDir(), "e.json")
	if err := os.WriteFile(p, []byte(`{"host":"x","metrics":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "нет метрик") {
		t.Fatalf("пустой файл: %v", err)
	}
}

func TestThin(t *testing.T) {
	c := &Chart{ts: []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}}
	c.panels[0] = panel{series: []series{{vals: []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}}}}
	c.Thin(3)
	if len(c.ts) != 4 || len(c.panels[0].series[0].vals) != 4 {
		t.Fatalf("thin: ts=%v vals=%v", c.ts, c.panels[0].series[0].vals)
	}
	if c.ts[1] != 4 {
		t.Fatalf("thin ts: %v", c.ts)
	}
}

func TestRenderSVG(t *testing.T) {
	now := int64(1753000000)
	rows := []metric{}
	for i := 0; i < 60; i++ {
		rows = append(rows, metric{Ts: now + int64(i*60), CPU: float64(i % 100),
			Mem: 50, Disk: 60, Load: fptr(1.5), Temp: fptr(45),
			RxMbps: fptr(8), TxMbps: fptr(4), NetOK: true,
			MemUsed: 8e9, MemTotal: 16e9, DiskUsed: 8e11, DiskTotal: 1e12})
	}
	rows[30].Temp = nil
	c := build("bork-server", rows)
	svg := c.RenderSVG()
	for _, want := range []string{"CPU %", "Память / диск %", "Сетевой трафик",
		"Нагрузка / температура", "bork-server", "метрики за", "<polyline", "<svg"} {
		if !strings.Contains(svg, want) {
			t.Fatalf("в SVG нет %q", want)
		}
	}
	for _, want := range []string{"7.5 ГБ/14.9 ГБ", "745.1 ГБ/931.3 ГБ",
		"rx, Мбит/с · 8.00 Мбит/с", "tx, Мбит/с · 4.00 Мбит/с"} {
		if !strings.Contains(svg, want) {
			t.Fatalf("в SVG нет текущего значения %q", want)
		}
	}
	if !strings.HasPrefix(svg, "<svg") || !strings.HasSuffix(svg, "</svg>\n") {
		t.Fatal("SVG не корректен")
	}
}

func TestUsageNoteNoTotals(t *testing.T) {
	// старые файлы без mem_total/disk_total — подпись не добавляется
	rows := []metric{{Ts: 100, CPU: 10, Mem: 20, Disk: 30, NetOK: true}}
	c := build("x", rows)
	if c.panels[1].series[0].note != "" || c.panels[1].series[1].note != "" {
		t.Fatalf("без total подписи быть не должно: %+v", c.panels[1].series)
	}
}

func TestRenderNoData(t *testing.T) {
	c := build("x", []metric{{Ts: 100, CPU: 10, Mem: 20, Disk: 30, NetOK: true}})
	c.panels[2].series[0].vals = []float64{math.NaN()}
	c.panels[2].series[1].vals = []float64{math.NaN()}
	svg := c.RenderSVG()
	if !strings.Contains(svg, "нет данных") {
		t.Fatal("ожидали 'нет данных' в панели без данных")
	}
}

func TestRenderPNG(t *testing.T) {
	now := int64(1753000000)
	rows := []metric{}
	for i := 0; i < 60; i++ {
		rows = append(rows, metric{Ts: now + int64(i*60), CPU: float64(i % 100),
			Mem: 50, Disk: 60, Load: fptr(1.5), Temp: fptr(45),
			RxMbps: fptr(8), TxMbps: fptr(4), NetOK: true,
			MemUsed: 8e9, MemTotal: 16e9, DiskUsed: 8e11, DiskTotal: 1e12})
	}
	rows[30].Temp = nil
	c := build("bork-server", rows)
	data, err := c.RenderPNG()
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 100 {
		t.Fatalf("PNG подозрительно маленький: %d байт", len(data))
	}
	if !bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("магические байты PNG не совпадают")
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("не удалось декодировать PNG: %v", err)
	}
	if b := img.Bounds(); b.Dx() != W || b.Dy() != H {
		t.Fatalf("размер %dx%d, ожидали %dx%d", b.Dx(), b.Dy(), W, H)
	}
}

func TestRenderPNGNoData(t *testing.T) {
	c := build("x", []metric{{Ts: 100, CPU: 10, Mem: 20, Disk: 30, NetOK: true}})
	c.panels[2].series[0].vals = []float64{math.NaN()}
	c.panels[2].series[1].vals = []float64{math.NaN()}
	if _, err := c.RenderPNG(); err != nil {
		t.Fatal(err)
	}
}

func TestSpanText(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0.5, "30 мин"},
		{1, "1 ч"},
		{6, "6 ч"},
		{47, "47 ч"},
		{48, "2 дн"},
		{72, "3 дн"},
	}
	for _, c := range cases {
		if got := spanText(c.in); got != c.want {
			t.Fatalf("spanText(%v)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestEsc(t *testing.T) {
	if got := esc(`<a b="c">&'`); got != "&lt;a b=&quot;c&quot;&gt;&amp;&apos;" {
		t.Fatalf("esc: %s", got)
	}
}

func TestXTickStep(t *testing.T) {
	if s := xTickStep(3600); s != 600 {
		t.Fatalf("span 1ч: %d (ожидали 600)", s)
	}
	if s := xTickStep(72 * 3600); s != 43200 {
		t.Fatalf("span 3д: %d (ожидали 43200)", s)
	}
}
