package graph

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	W    = 1300
	H    = 800
	ML   = 70
	MR   = 25
	MT   = 100
	MB   = 55
	PGap = 55
)

type rect struct{ x, y, w, h int }

func (c *Chart) RenderSVG() string {
	var b strings.Builder
	header := `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" font-family="sans-serif">`
	fmt.Fprintf(&b, header+"\n", W, H, W, H)
	b.WriteString(`<rect width="100%" height="100%" fill="#ffffff"/>` + "\n")
	fmt.Fprintf(&b, `<text x="%d" y="44" text-anchor="middle" font-size="26" font-weight="bold" fill="#222">%s — метрики за %s</text>`+"\n",
		W/2, esc(c.host), esc(c.spanTxt))

	pw := (W - ML - MR - PGap) / 2
	ph := (H - MT - MB - PGap) / 2
	rects := [4]rect{
		{ML, MT, pw, ph},
		{ML + pw + PGap, MT, pw, ph},
		{ML, MT + ph + PGap, pw, ph},
		{ML + pw + PGap, MT + ph + PGap, pw, ph},
	}

	tmin, tmax := int64(0), int64(0)
	if len(c.ts) > 0 {
		tmin, tmax = c.ts[0], c.ts[len(c.ts)-1]
	}

	for i, p := range c.panels {
		r := rects[i]
		fmt.Fprintf(&b, `<rect x="%d" y="%d" width="%d" height="%d" fill="#fafafa" stroke="#cccccc"/>`+"\n", r.x, r.y, r.w, r.h)
		fmt.Fprintf(&b, `<text x="%d" y="%d" font-size="16" font-weight="bold" fill="#222">%s</text>`+"\n", r.x+8, r.y+16, esc(p.title))

		ymin, ymax := math.NaN(), math.NaN()
		hasData := false
		for _, s := range p.series {
			for _, v := range s.vals {
				if math.IsNaN(v) {
					continue
				}
				hasData = true
				if math.IsNaN(ymin) || v < ymin {
					ymin = v
				}
				if math.IsNaN(ymax) || v > ymax {
					ymax = v
				}
			}
		}
		if !hasData {
			fmt.Fprintf(&b, `<text x="%d" y="%d" text-anchor="middle" font-size="14" fill="#999">нет данных</text>`+"\n",
				r.x+r.w/2, r.y+r.h/2)
			continue
		}
		pad := (ymax - ymin) * 0.05
		if pad == 0 {
			pad = 1
		}
		ymin -= pad
		ymax += pad

		for gi := 0; gi <= 4; gi++ {
			frac := float64(gi) / 4
			yy := r.y + int(float64(r.h)*(1-frac))
			v := ymin + frac*(ymax-ymin)
			fmt.Fprintf(&b, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="#e0e0e0" stroke-width="1"/>`+"\n", r.x, yy, r.x+r.w, yy)
			fmt.Fprintf(&b, `<text x="%d" y="%d" text-anchor="end" font-size="12" fill="#555">%s</text>`+"\n", r.x-8, yy+4, numStr(v))
		}

		if tmax > tmin {
			step := xTickStep(tmax - tmin)
			for t := ((tmin + step - 1) / step) * step; t <= tmax; t += step {
				x := r.x + int(float64(t-tmin)/float64(tmax-tmin)*float64(r.w))
				fmt.Fprintf(&b, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="#e0e0e0" stroke-width="1"/>`+"\n", x, r.y, x, r.y+r.h)
				ty := r.y + r.h + 16
				fmt.Fprintf(&b, `<text x="%d" y="%d" transform="rotate(-30 %d %d)" text-anchor="end" font-size="12" fill="#555">%s</text>`+"\n",
					x, ty, x, ty, esc(tickLabel(t, step)))
			}
		}

		for _, s := range p.series {
			pts := []string{}
			for i, v := range s.vals {
				if math.IsNaN(v) {
					emitPoly(&b, pts, s.color)
					pts = pts[:0]
					continue
				}
				if tmax <= tmin {
					continue
				}
				x := r.x + int(float64(c.ts[i]-tmin)/float64(tmax-tmin)*float64(r.w))
				y := r.y + int((1-(v-ymin)/(ymax-ymin))*float64(r.h))
				pts = append(pts, fmt.Sprintf("%d,%d", x, y))
			}
			emitPoly(&b, pts, s.color)
		}

		if len(p.series) > 1 {
			for si, s := range p.series {
				lx := r.x + 8
				ly := r.y + 36 + si*20
				fmt.Fprintf(&b, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="%s" stroke-width="3"/>`+"\n",
					lx, ly-4, lx+22, ly-4, s.color)
				fmt.Fprintf(&b, `<text x="%d" y="%d" font-size="13" fill="#333">%s%s</text>`+"\n", lx+28, ly, esc(s.name), esc(s.note))
			}
		}
	}
	b.WriteString("</svg>\n")
	return b.String()
}

func emitPoly(b *strings.Builder, pts []string, color string) {
	if len(pts) < 2 {
		return
	}
	fmt.Fprintf(b, `<polyline points="%s" fill="none" stroke="%s" stroke-width="2" stroke-linejoin="round"/>`+"\n",
		strings.Join(pts, " "), color)
}

func xTickStep(span int64) int64 {
	cands := []int64{60, 120, 300, 600, 900, 1800, 3600, 7200, 10800, 21600, 43200,
		86400, 172800, 259200, 604800, 1209600, 2592000}
	if span <= 0 {
		return 60
	}
	for _, s := range cands {
		if span/s <= 6 {
			return s
		}
	}
	return cands[len(cands)-1]
}

func tickLabel(t, step int64) string {
	tt := time.Unix(t, 0)
	if step >= 86400 {
		return tt.Format("02.01")
	}
	return tt.Format("02.01 15:04")
}

func numStr(v float64) string {
	if math.Abs(v) >= 100 || v == math.Trunc(v) {
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	return strconv.FormatFloat(v, 'f', 1, 64)
}

func esc(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;", "'", "&apos;")
	return r.Replace(s)
}
