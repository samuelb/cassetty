package ui

import (
	"math"
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
)

// canvas is a monochrome-per-cell braille drawing surface: every terminal
// cell holds 2×4 dots. Each cell also remembers the highest layer drawn into
// it, which selects the cell's color.
type canvas struct {
	w, h  int
	mask  []uint8
	layer []uint8
}

var brailleBit = [4][2]uint8{{0x01, 0x08}, {0x02, 0x10}, {0x04, 0x20}, {0x40, 0x80}}

func newCanvas(w, h int) *canvas {
	return &canvas{w: w, h: h, mask: make([]uint8, w*h), layer: make([]uint8, w*h)}
}

func (c *canvas) set(x, y int, layer uint8) {
	if x < 0 || y < 0 || x >= c.w*2 || y >= c.h*4 {
		return
	}
	i := (y/4)*c.w + x/2
	c.mask[i] |= brailleBit[y%4][x%2]
	c.layer[i] = max(c.layer[i], layer)
}

func (c *canvas) line(x0, y0, x1, y1 float64, layer uint8) {
	n := int(math.Ceil(math.Hypot(x1-x0, y1-y0)*2.5)) + 1
	for i := 0; i <= n; i++ {
		t := float64(i) / float64(n)
		c.set(int(math.Round(x0+(x1-x0)*t)), int(math.Round(y0+(y1-y0)*t)), layer)
	}
}

const (
	layerNone uint8 = iota
	layerTape
	layerHub
)

// cassetteHeight picks the cassette size for the available terminal height.
func cassetteHeight(termH int) int {
	switch {
	case termH >= 38:
		return 13
	case termH >= 28:
		return 11
	default:
		return 9
	}
}

// renderCassette draws the tape (or an empty deck) into exactly h lines of
// w cells.
func (m *Model) renderCassette(w, h int) []string {
	t := m.theme
	if m.src == nil {
		return m.renderEmptyDeck(w, h)
	}

	edge := fg(t.ShellEdge)
	shell := fgbg(t.ShellEdge, t.ShellBg)
	label := fgbg(t.LabelText, t.LabelBg)
	labelMuted := fgbg(t.LabelMuted, t.LabelBg)
	lw := w - 4 // label width
	wr := h - 6 // window rows
	side := max(3, lw/7)
	winW := lw - 2*side

	lines := make([]string, 0, h)
	lines = append(lines, edge.Render("╭"+strings.Repeat("─", w-2)+"╮"))

	shellRow := func(content string, cw int, l, r string) string {
		var lb lineBuf
		lb.add(edge, "│").add(shell, l).raw(content, cw).add(shell, r).add(edge, "│")
		return lb.String()
	}

	// Label: title line.
	{
		var lb lineBuf
		badge := lipgloss.NewStyle().Foreground(t.LabelBg).Background(t.Stripe1).Bold(true)
		lb.add(label, " ").add(badge, " A ").add(label, " ")
		right := m.labelRight()
		title := truncate(m.trackTitle(), lw-lb.w-utf8.RuneCountInString(right)-2)
		lb.add(label.Bold(true), title)
		lb.padTo(label, lw-len([]rune(right))-1).add(labelMuted, right).add(label, " ")
		lines = append(lines, shellRow(lb.String(), lw, " ", " "))
	}
	// Label: artist / album line, with handwriting rule.
	{
		var lb lineBuf
		sub := truncate(m.trackSubtitle(), lw-4)
		lb.add(label, "  ").add(labelMuted.Italic(true), sub).add(label, " ")
		rule := fgbg(t.LabelMuted, t.LabelBg)
		if rest := lw - lb.w - 1; rest > 0 {
			lb.add(rule, strings.Repeat("┈", rest))
		}
		lb.padTo(label, lw)
		lines = append(lines, shellRow(lb.String(), lw, " ", " "))
	}

	// Window with the reels, framed by the colored band of the label.
	window := m.reelWindow(winW, wr)
	band := gradient(wr, t.Stripe1, t.Stripe2)
	lo, hi := 1, wr-2 // rows showing the band
	if wr <= 3 {
		lo, hi = 0, wr-1
	}
	if len(t.Band) > 0 {
		copy(band[lo:], ramp(hi-lo+1, t.Band...))
	}
	for y := 0; y < wr; y++ {
		sideStyle := label
		sideText := strings.Repeat(" ", side)
		if y >= lo && y <= hi {
			sideStyle = fgbg(band[y], band[y])
			sideText = strings.Repeat("█", side)
		}
		var lb lineBuf
		lb.add(sideStyle, sideText).raw(window[y], winW).add(sideStyle, sideText)
		lines = append(lines, shellRow(lb.String(), lw, " ", " "))
	}

	// Label bottom: tape type, printed bold while it is heard, and running
	// time.
	{
		var lb lineBuf
		tapeStyle := labelMuted
		if m.tape {
			tapeStyle = label.Bold(true)
		}
		lb.add(tapeStyle, "  "+truncate(m.tapeType(), lw/2))
		right := m.tapeLength() + "  "
		lb.padTo(label, lw-len([]rune(right)))
		lb.add(labelMuted, right)
		lines = append(lines, shellRow(lb.String(), lw, " ", " "))
	}

	// Head area: screws, trapezoid with capstan holes and exposed tape.
	{
		inner := w - 2
		trap := max(10, inner*3/5)
		trap -= trap % 2
		gap := (inner - trap) / 2
		hole := fgbg(t.WindowBg, t.ShellBg)
		tape := fgbg(t.Tape, t.ShellBg)
		var lb lineBuf
		lb.add(edge, "│")
		lb.add(shell, " ◦").add(shell, strings.Repeat(" ", gap-2))
		lb.add(shell, "╱")
		mid := trap - 2
		holes := []rune(strings.Repeat("▁", mid))
		place := func(at int, s string) {
			for i, r := range []rune(s) {
				if at+i >= 0 && at+i < len(holes) {
					holes[at+i] = r
				}
			}
		}
		place(1, "●")
		place(mid-2, "●")
		place(mid/4, "■")
		place(mid-1-mid/4, "■")
		head := max(2, mid/5)
		place(mid/2-head/2, strings.Repeat("▬", head))
		for _, r := range holes {
			switch r {
			case '▁':
				lb.add(tape, string(r))
			case '▬':
				lb.add(fgbg(t.Muted, t.ShellBg), string(r))
			default:
				lb.add(hole, string(r))
			}
		}
		lb.add(shell, "╲")
		lb.padTo(shell, w-3).add(shell, "◦ ")
		lb.add(edge, "│")
		lines = append(lines, lb.String())
	}
	lines = append(lines, edge.Render("╰"+strings.Repeat("─", w-2)+"╯"))
	return lines
}

// quadrants maps a 2×2 coverage mask (TL=1, TR=2, BL=4, BR=8) to a glyph.
var quadrants = []string{" ", "▘", "▝", "▀", "▖", "▌", "▞", "▛", "▗", "▚", "▐", "▜", "▄", "▙", "▟", "█"}

// reelWindow renders the cassette window: the tape packs are solid discs
// drawn with quadrant blocks (2×2 sub-cells, two colors) and the rotating
// hubs are drawn on top in braille for the finer detail. All geometry is in
// braille dot space, where dots are roughly square.
func (m *Model) reelWindow(cols, rows int) []string {
	t := m.theme
	dw, dh := float64(cols*2), float64(rows*4)
	cy := (dh - 1) / 2
	cxL, cxR := (dw-1)*0.25, (dw-1)*0.75
	hubR := hubRadius(dh)
	rMin := hubR + 0.8
	// A full pack fills the height of the window, and in a low window it is
	// wider than that and cut off above and below, as it is by the window of
	// a real cassette: the hub leaves too little room for it otherwise.
	rMax := math.Min(math.Max(dh/2+0.4, hubR+4.3), (cxR-cxL)-rMin-1.5)
	// Tape length is proportional to the pack's area, so the radius follows
	// the square root of the remaining fraction.
	p := m.progress()
	rL := math.Sqrt(rMin*rMin + (1-p)*(rMax*rMax-rMin*rMin))
	rR := math.Sqrt(rMin*rMin + p*(rMax*rMax-rMin*rMin))

	hubs := newCanvas(cols, rows)
	drawHub(hubs, cxL, cy, hubR, m.angleL)
	drawHub(hubs, cxR, cy, hubR, m.angleR)

	// The tape leaves each pack on a tangent towards the guide rollers in
	// the bottom corners.
	strands := newCanvas(cols, rows)
	tangent := func(cx, r, px, py float64, sign float64) {
		dx, dy := px-cx, py-cy
		d := math.Hypot(dx, dy)
		if d <= r {
			return
		}
		a := math.Atan2(dy, dx) + sign*math.Acos(r/d)
		strands.line(cx+r*math.Cos(a), cy+r*math.Sin(a), px, py, layerTape)
	}
	tangent(cxL, rL, 0, dh-1, 1)
	tangent(cxR, rR, dw-1, dh-1, -1)

	inPack := func(x, y float64) bool {
		dl := math.Hypot(x-cxL, y-cy)
		dr := math.Hypot(x-cxR, y-cy)
		return (dl > hubR && dl <= rL) || (dr > hubR && dr <= rR)
	}
	// Hub cells take the tape color as background unless they are part of
	// the centre hole, so the hub reads as a ring set into the pack.
	onTape := func(x, y float64) bool {
		dl := math.Hypot(x-cxL, y-cy)
		dr := math.Hypot(x-cxR, y-cy)
		return (dl > hubR*0.38 && dl <= rL) || (dr > hubR*0.38 && dr <= rR)
	}

	pack := fgbg(t.Tape, t.WindowBg)
	hub := fgbg(t.Hub, t.WindowBg)
	hubOnTape := fgbg(t.Hub, t.Tape)
	out := make([]string, rows)
	for y := 0; y < rows; y++ {
		var sb strings.Builder
		var run strings.Builder
		var cur *lipgloss.Style
		emit := func(st *lipgloss.Style, s string) {
			if cur != st {
				if run.Len() > 0 {
					sb.WriteString(cur.Render(run.String()))
					run.Reset()
				}
				cur = st
			}
			run.WriteString(s)
		}
		for x := 0; x < cols; x++ {
			fx, fy := float64(2*x), float64(4*y)
			if mask := hubs.mask[y*cols+x]; mask != 0 {
				st := &hub
				if onTape(fx+0.5, fy+1.5) {
					st = &hubOnTape
				}
				emit(st, string(rune(0x2800+int(mask))))
				continue
			}
			q := 0
			for i, pt := range [4][2]float64{{0, 0.5}, {1, 0.5}, {0, 2.5}, {1, 2.5}} {
				if inPack(fx+pt[0], fy+pt[1]) {
					q |= 1 << i
				}
			}
			if mask := strands.mask[y*cols+x]; q == 0 && mask != 0 {
				emit(&pack, string(rune(0x2800+int(mask))))
				continue
			}
			emit(&pack, quadrants[q])
		}
		if run.Len() > 0 {
			sb.WriteString(cur.Render(run.String()))
		}
		out[y] = sb.String()
	}
	return out
}

// hubRadius is the radius of a reel hub in a window dh dots high. A low
// window gets a hub that is larger for its height: below some four dots of
// radius there is no telling that it turns.
func hubRadius(dh float64) float64 {
	return math.Max(3.7, dh*0.24)
}

// drawHub draws a reel hub: a ring with spokes around the spindle hole. A
// dot is set by how far its centre is from the middle of a spoke, which
// keeps the spokes the same width at every angle; drawn as lines they grow
// and shrink while they turn. A small hub has one spoke only: three of them
// run into each other, and a single one shows the rotation.
func drawHub(cv *canvas, cx, cy, r, angle float64) {
	spokes, inner, half := 3, 0.3*r, 1.0
	if r < 4.5 {
		spokes, inner, half = 1, 0, 0.9
	}
	type dir struct{ cs, sn float64 }
	dirs := make([]dir, spokes)
	for k := range dirs {
		a := angle + float64(k)*2*math.Pi/float64(spokes)
		dirs[k] = dir{math.Cos(a), math.Sin(a)}
	}
	for y := int(cy - r - 1); y <= int(cy+r+1); y++ {
		for x := int(cx - r - 1); x <= int(cx+r+1); x++ {
			dx, dy := float64(x)-cx, float64(y)-cy
			d := math.Hypot(dx, dy)
			if d > r+0.45 {
				continue
			}
			on := d >= r-0.6
			for _, s := range dirs {
				if on {
					break
				}
				along, across := dx*s.cs+dy*s.sn, dy*s.cs-dx*s.sn
				on = along >= inner && math.Abs(across) <= half
			}
			if on {
				cv.set(x, y, layerHub)
			}
		}
	}
}

func (m *Model) renderEmptyDeck(w, h int) []string {
	t := m.theme
	dash := fg(t.Faint)
	lines := []string{dash.Render("╭" + strings.Repeat("┄", w-2) + "╮")}
	msg := []string{"", "NO TAPE", "", "pick a track in the library", "and press ⏎ to load it"}
	top := (h - 2 - len(msg)) / 2
	for y := 0; y < h-2; y++ {
		text := ""
		if i := y - top; i >= 0 && i < len(msg) {
			text = msg[i]
		}
		st := fg(t.Muted)
		if text == "NO TAPE" {
			st = fg(t.Text).Bold(true)
		}
		lines = append(lines, dash.Render("┆")+st.Render(center(text, w-2))+dash.Render("┆"))
	}
	lines = append(lines, dash.Render("╰"+strings.Repeat("┄", w-2)+"╯"))
	return lines
}
