package graph

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// PNG-рендер того же макета, что и SVG: 2x2 панели с осями, сеткой
// и легендой. Текст рисуется встроенным шрифтом Go (без внешних файлов).

var (
	faceCacheMu sync.Mutex
	faceCache   = map[faceKey]font.Face{}
)

type faceKey struct {
	bold bool
	size float64
}

func faceFor(bold bool, size float64) font.Face {
	faceCacheMu.Lock()
	defer faceCacheMu.Unlock()
	k := faceKey{bold: bold, size: size}
	if f, ok := faceCache[k]; ok {
		return f
	}
	var ttf []byte
	if bold {
		ttf = gobold.TTF
	} else {
		ttf = goregular.TTF
	}
	fontData, err := opentype.Parse(ttf)
	if err != nil {
		panic(err)
	}
	f, err := opentype.NewFace(fontData, &opentype.FaceOptions{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		panic(err)
	}
	faceCache[k] = f
	return f
}

func hexColor(s string) color.RGBA {
	c := color.RGBA{A: 0xff}
	v, _ := strconv.ParseUint(strings.TrimPrefix(s, "#"), 16, 32)
	c.R = uint8(v >> 16)
	c.G = uint8(v >> 8)
	c.B = uint8(v)
	return c
}

func textWidth(s string, bold bool, size float64) int {
	w := font.MeasureString(faceFor(bold, size), s)
	return int((w + 32) / 64)
}

// drawText рисует текст с левым краем в (x, y) — y это базовая линия.
func drawText(img *image.RGBA, x, y int, bold bool, size float64, col color.RGBA, s string) {
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(col),
		Face: faceFor(bold, size),
		Dot:  fixed.P(x, y),
	}
	d.DrawString(s)
}

// drawTextAnchor рисует текст с заданным горизонтальным выравниванием.
func drawTextAnchor(img *image.RGBA, x, y int, bold bool, size float64, col color.RGBA, s, anchor string) {
	w := textWidth(s, bold, size)
	switch anchor {
	case "middle":
		x -= w / 2
	case "end":
		x -= w
	}
	drawText(img, x, y, bold, size, col, s)
}

func bresenham(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	dx, dy := x1-x0, y1-y0
	if dx < 0 {
		dx = -dx
	}
	if dy < 0 {
		dy = -dy
	}
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	err := dx - dy
	for {
		img.Set(x0, y0, c)
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x0 += sx
		}
		if e2 < dx {
			err += dx
			y0 += sy
		}
	}
}

func drawLine(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA, width int) {
	for i := 0; i < width; i++ {
		bresenham(img, x0, y0+i, x1, y1+i, c)
	}
}

func fillRect(img *image.RGBA, r image.Rectangle, c color.RGBA) {
	for x := r.Min.X; x < r.Max.X; x++ {
		for y := r.Min.Y; y < r.Max.Y; y++ {
			img.Set(x, y, c)
		}
	}
}

// drawSeries рисует ломаную с разрывами на NaN.
func drawSeries(img *image.RGBA, r image.Rectangle, c *Chart, vals []float64, col color.RGBA, width int) {
	tmin, tmax := c.ts[0], c.ts[len(c.ts)-1]
	if tmax <= tmin {
		return
	}
	ymin, ymax := c.panelRange(r, vals)
	if math.IsNaN(ymin) {
		return
	}
	prevX, prevY, have := 0, 0, false
	for i, v := range vals {
		if math.IsNaN(v) {
			have = false
			continue
		}
		x := r.Min.X + int(float64(c.ts[i]-tmin)/float64(tmax-tmin)*float64(r.Dx()))
		y := r.Min.Y + int((1-(v-ymin)/(ymax-ymin))*float64(r.Dy()))
		if have {
			drawLine(img, prevX, prevY, x, y, col, width)
		}
		prevX, prevY, have = x, y, true
	}
}

// panelRange возвращает масштаб панели по одному ряду (с полем 5%).
func (c *Chart) panelRange(r image.Rectangle, vals []float64) (ymin, ymax float64) {
	ymin, ymax = math.NaN(), math.NaN()
	for _, v := range vals {
		if math.IsNaN(v) {
			continue
		}
		if math.IsNaN(ymin) || v < ymin {
			ymin = v
		}
		if math.IsNaN(ymax) || v > ymax {
			ymax = v
		}
	}
	if math.IsNaN(ymin) {
		return math.NaN(), math.NaN()
	}
	pad := (ymax - ymin) * 0.05
	if pad == 0 {
		pad = 1
	}
	return ymin - pad, ymax + pad
}

// panelRangeAll объединяет диапазон всех рядов панели.
func (c *Chart) panelRangeAll(r image.Rectangle, p panel) (ymin, ymax float64) {
	ymin, ymax = math.NaN(), math.NaN()
	for _, s := range p.series {
		a, b := c.panelRange(r, s.vals)
		if math.IsNaN(a) {
			continue
		}
		if math.IsNaN(ymin) || a < ymin {
			ymin = a
		}
		if math.IsNaN(ymax) || b > ymax {
			ymax = b
		}
	}
	return ymin, ymax
}

// pngTickLabel — компактная подпись тика по X без поворота.
func pngTickLabel(t, step int64, spacing int) string {
	tt := time.Unix(t, 0)
	if spacing >= 90 {
		return tt.Format("02.01 15:04")
	}
	if step >= 86400 {
		return tt.Format("02.01")
	}
	return tt.Format("15:04")
}

func (c *Chart) RenderPNG() ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, W, H))
	white := color.RGBA{0xff, 0xff, 0xff, 0xff}
	fillRect(img, img.Bounds(), white)

	bold := color.RGBA{0x22, 0x22, 0x22, 0xff}
	drawTextAnchor(img, W/2, 44, true, 26, bold,
		fmt.Sprintf("%s — метрики за %s", c.host, c.spanTxt), "middle")

	pw := (W - ML - MR - PGap) / 2
	ph := (H - MT - MB - PGap) / 2
	rects := [4]image.Rectangle{
		{Min: image.Pt(ML, MT), Max: image.Pt(ML+pw, MT+ph)},
		{Min: image.Pt(ML+pw+PGap, MT), Max: image.Pt(ML+2*pw+PGap, MT+ph)},
		{Min: image.Pt(ML, MT+ph+PGap), Max: image.Pt(ML+pw, MT+2*ph+PGap)},
		{Min: image.Pt(ML+pw+PGap, MT+ph+PGap), Max: image.Pt(ML+2*pw+PGap, MT+2*ph+PGap)},
	}

	panelBG := color.RGBA{0xfa, 0xfa, 0xfa, 0xff}
	panelEdge := color.RGBA{0xcc, 0xcc, 0xcc, 0xff}
	gridCol := color.RGBA{0xe0, 0xe0, 0xe0, 0xff}
	axisCol := color.RGBA{0x55, 0x55, 0x55, 0xff}

	tmin, tmax := int64(0), int64(0)
	if len(c.ts) > 0 {
		tmin, tmax = c.ts[0], c.ts[len(c.ts)-1]
	}

	for i, p := range c.panels {
		r := rects[i]
		fillRect(img, r, panelBG)
		drawLine(img, r.Min.X, r.Min.Y, r.Max.X, r.Min.Y, panelEdge, 1)
		drawLine(img, r.Min.X, r.Max.Y, r.Max.X, r.Max.Y, panelEdge, 1)
		drawLine(img, r.Min.X, r.Min.Y, r.Min.X, r.Max.Y, panelEdge, 1)
		drawLine(img, r.Max.X, r.Min.Y, r.Max.X, r.Max.Y, panelEdge, 1)

		drawText(img, r.Min.X+8, r.Min.Y+16, true, 16, color.RGBA{0x22, 0x22, 0x22, 0xff}, p.title)

		ymin, ymax := c.panelRangeAll(r, p)
		if math.IsNaN(ymin) {
			drawTextAnchor(img, r.Min.X+r.Dx()/2, r.Min.Y+r.Dy()/2, false, 14,
				color.RGBA{0x99, 0x99, 0x99, 0xff}, "нет данных", "middle")
			continue
		}

		for gi := 0; gi <= 4; gi++ {
			frac := float64(gi) / 4
			yy := r.Min.Y + int(float64(r.Dy())*(1-frac))
			v := ymin + frac*(ymax-ymin)
			drawLine(img, r.Min.X, yy, r.Max.X, yy, gridCol, 1)
			drawTextAnchor(img, r.Min.X-8, yy+4, false, 12, axisCol, numStr(v), "end")
		}

		spacing := 0
		if tmax > tmin {
			step := xTickStep(tmax - tmin)
			spacing = int(float64(r.Dx()) * float64(step) / float64(tmax-tmin))
			for t := ((tmin + step - 1) / step) * step; t <= tmax; t += step {
				x := r.Min.X + int(float64(t-tmin)/float64(tmax-tmin)*float64(r.Dx()))
				drawLine(img, x, r.Min.Y, x, r.Max.Y, gridCol, 1)
				drawTextAnchor(img, x, r.Max.Y+20, false, 10, axisCol, pngTickLabel(t, step, spacing), "middle")
			}
		}

		for _, s := range p.series {
			drawSeries(img, r, c, s.vals, hexColor(s.color), 2)
		}

		if len(p.series) > 1 {
			for si, s := range p.series {
				lx := r.Min.X + 8
				ly := r.Min.Y + 36 + si*20
				drawLine(img, lx, ly-4, lx+22, ly-4, hexColor(s.color), 3)
				drawText(img, lx+28, ly, false, 13, color.RGBA{0x33, 0x33, 0x33, 0xff}, s.name+s.note)
			}
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
