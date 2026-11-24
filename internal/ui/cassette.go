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

	// Label bottom: tape type and running time.
	{
		var lb lineBuf
		lb.add(labelMuted, "  "+truncate(m.tapeType(), lw/2))
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

// octants maps a 2×4 coverage mask to a block glyph. Bit n-1 stands for
// octant n, the octants being numbered row by row: 1 2 / 3 4 / 5 6 / 7 8.
// Unicode 16 has a BLOCK OCTANT character for every mask, in order of the
// mask from U+1CD00, but for the 26 that older block characters draw.
var octants = func() (g [256]string) {
	oct := func(n ...int) (m int) {
		for _, i := range n {
			m |= 1 << (i - 1)
		}
		return m
	}
	older := map[int]rune{
		0: ' ', 255: '█',
		oct(1, 3): '▘', oct(2, 4): '▝', oct(5, 7): '▖', oct(6, 8): '▗',
		oct(1, 2, 3, 4): '▀', oct(5, 6, 7, 8): '▄', oct(1, 3, 5, 7): '▌', oct(2, 4, 6, 8): '▐',
		oct(2, 4, 5, 7): '▞', oct(1, 3, 6, 8): '▚',
		oct(1, 2, 3, 4, 5, 7): '▛', oct(1, 2, 3, 4, 6, 8): '▜', oct(1, 3, 5, 6, 7, 8): '▙', oct(2, 4, 5, 6, 7, 8): '▟',
		oct(1, 2): '\U0001FB82', oct(7, 8): '▂', oct(1, 2, 3, 4, 5, 6): '\U0001FB85', oct(3, 4, 5, 6, 7, 8): '▆',
		oct(1): '\U0001CEA8', oct(2): '\U0001CEAB', oct(7): '\U0001CEA3', oct(8): '\U0001CEA0',
		oct(3, 5): '\U0001FBE6', oct(4, 6): '\U0001FBE7',
	}
	next := rune(0x1CD00)
	for m := range g {
		r, ok := older[m]
		if !ok {
			r = next
			next++
		}
		g[m] = string(r)
	}
	return g
}()

// reels is where the reels lie in a window, in braille dot space.
type reels struct {
	cxL, cxR, cy float64 // centres
	hubR         float64
	hole         float64 // radius of the spindle hole
	rMin, rMax   float64 // radius of an empty and a full pack
}

// reelGeometry lays out the reels in a window of cols×rows cells. Each
// cell holds 2×4 dots, and the reels turn about the middle of four dots,
// so that every round thing about them is drawn symmetric:
//
//   - The hub is drawn in braille, on the tape: a braille cell has but one
//     color behind its dots, and the tape of an empty reel is just enough
//     to fill the cells of the hub's ring, so that the edge of the pack is
//     always drawn in octants, and round.
//   - The spindle hole in the middle of the hub is a disc of the dots whose
//     centres lie within a circle an even number of dots across: as round
//     as dots make a disc. It is drawn in octants as well, in the cells it
//     reaches into, which the hub leaves.
//
// The middle of a window an odd number of rows high, as it is, lies in the
// middle of four dots already. Across, a reel turns about the middle of a
// cell or the line between two cells, whichever lets the cells of its
// ring reach the least beyond it, a dot at most from a quarter of the
// width; the right reel mirrors the left.
func reelGeometry(cols, rows int) reels {
	dw, dh := float64(cols*2), float64(rows*4)
	g := reels{hubR: hubRadius(rows), cy: (dh - 1) / 2}
	g.hole = math.Max(1, math.Round(g.hubR*0.3))
	q := (dw - 1) * 0.25
	reach := math.Inf(1)
	for _, cx := range []float64{2*math.Round((q-0.5)/2) + 0.5, 2*math.Round((q+0.5)/2) - 0.5} {
		if rr := ringReach(cx, g.cy, g.hubR); rr < reach || rr == reach && math.Abs(cx-q) < math.Abs(g.cxL-q) {
			reach, g.cxL = rr, cx
		}
	}
	g.cxR = dw - 1 - g.cxL
	g.rMin = unNubbed(reach)
	// A full pack fills the height of the window, and in a low window it is
	// wider than that and cut off above and below, as it is by the window of
	// a real cassette: the hub leaves too little room for it otherwise.
	g.rMax = unNubbed(math.Min(math.Max(dh/2+0.4, g.hubR+4.3), (g.cxR-g.cxL)-g.rMin-1.5))
	return g
}

// ringReach returns how far from cx, cy the farthest dot lies of the cells
// that hold the ring of a hub of radius r.
func ringReach(cx, cy, r float64) float64 {
	reach := 0.0
	for y := math.Floor(cy - r - 1); y <= cy+r+1; y++ {
		for x := math.Floor(cx - r - 1); x <= cx+r+1; x++ {
			if !onRing(math.Hypot(x-cx, y-cy), r) {
				continue
			}
			x0, y0 := 2*math.Floor(x/2), 4*math.Floor(y/4) // the cell's first dot
			for _, c := range [4][2]float64{{0, 0}, {1, 0}, {0, 3}, {1, 3}} {
				reach = math.Max(reach, math.Hypot(x0+c[0]-cx, y0+c[1]-cy))
			}
		}
	}
	return reach
}

// unNubbed returns the radius r, or a little more, such that the disc of
// the dots around the middle of four dots does not end in a nub: a row of
// two dots at the top, bottom and sides, as it does when r just reaches
// another row. A pack that grows or shrinks passes through such radii; an
// empty or a full one does not stay at one.
func unNubbed(r float64) float64 {
	a := math.Floor(math.Sqrt(r*r-0.25)-0.5) + 0.5 // the outermost row reached
	if r < math.Hypot(a, 1.5) {                    // with two dots only
		return math.Hypot(a, 1.5)
	}
	return r
}

// reelWindow renders the cassette window: the tape packs are solid discs
// drawn with octant blocks (2×4 sub-cells, two colors) and the rotating
// hubs are drawn on top in braille for the finer detail. All geometry is in
// braille dot space, where dots are roughly square; an octant is one dot.
func (m *Model) reelWindow(cols, rows int) []string {
	t := m.theme
	dw, dh := float64(cols*2), float64(rows*4)
	g := reelGeometry(cols, rows)
	cxL, cxR, cy, hubR, hole := g.cxL, g.cxR, g.cy, g.hubR, g.hole
	rMin, rMax := g.rMin, g.rMax
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

	// The tape color runs into the hub up to the spindle hole.
	inHole := func(x, y float64) bool {
		return math.Hypot(x-cxL, y-cy) <= hole || math.Hypot(x-cxR, y-cy) <= hole
	}
	inPack := func(x, y float64) bool {
		return !inHole(x, y) && (math.Hypot(x-cxL, y-cy) <= rL || math.Hypot(x-cxR, y-cy) <= rR)
	}

	pack := fgbg(t.Tape, t.WindowBg)
	hub := fgbg(t.Hub, t.Tape)
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
			q, holed := 0, false
			for i := range 8 {
				dx, dy := fx+float64(i%2), fy+float64(i/2)
				if inPack(dx, dy) {
					q |= 1 << i
				}
				holed = holed || inHole(dx, dy)
			}
			// The hub lies on the tape, and leaves the cells the spindle
			// hole reaches into, see reelGeometry.
			if mask := hubs.mask[y*cols+x]; mask != 0 && !holed {
				emit(&hub, string(rune(0x2800+int(mask))))
				continue
			}
			if mask := strands.mask[y*cols+x]; q == 0 && mask != 0 {
				emit(&pack, string(rune(0x2800+int(mask))))
				continue
			}
			emit(&pack, octants[q])
		}
		if run.Len() > 0 {
			sb.WriteString(cur.Render(run.String()))
		}
		out[y] = sb.String()
	}
	return out
}

// hubRadius is the radius of a reel hub in a window rows cells high. In
// the low windows the hub is larger for the height: below some four dots
// of radius there is no telling that it turns. The radii keep the cells of
// the hub's ring from reaching far beyond it, see reelGeometry: in a window
// of seven rows its outermost dots are the outermost of their cells.
func hubRadius(rows int) float64 {
	switch {
	case rows >= 7:
		return 5.5
	case rows >= 5:
		return 4.5
	}
	return 3.7
}

// onRing reports whether a dot d from the centre of a hub of radius r is
// on its ring.
func onRing(d, r float64) bool {
	return d >= r-0.6 && d <= r+0.45
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
			on := onRing(d, r)
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
