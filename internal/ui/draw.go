package ui

import (
	"image/color"
	"slices"
	"strings"
	"sync"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lucasb-eyer/go-colorful"
)

func fg(c color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }

func fgbg(f, b color.Color) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(f).Background(b)
}

// lineBuf assembles one terminal line out of styled segments while keeping
// track of its printable width. Segments that follow one another in the same
// style are written as one run, with the style switched on once: a line of
// the display is hundreds of cells, many of them alike.
type lineBuf struct {
	sb  strings.Builder
	w   int
	run string // the escape sequence of the run being written, "" for none
}

func (l *lineBuf) add(s lipgloss.Style, text string) *lineBuf {
	if text == "" {
		return l
	}
	if on, ok := styleOn(s); !ok {
		l.end()
		l.sb.WriteString(s.Render(text))
	} else {
		if on != l.run {
			l.end()
			l.sb.WriteString(on)
			l.run = on
		}
		l.sb.WriteString(text)
	}
	l.w += cells(text)
	return l
}

// end switches the style of the run being written off.
func (l *lineBuf) end() {
	if l.run != "" {
		l.sb.WriteString(styleOff)
		l.run = ""
	}
}

// raw appends an already rendered string of known printable width.
func (l *lineBuf) raw(text string, w int) *lineBuf {
	l.end()
	l.sb.WriteString(text)
	l.w += w
	return l
}

// padTo fills the line with styled spaces up to width w.
func (l *lineBuf) padTo(s lipgloss.Style, w int) *lineBuf {
	if w > l.w {
		l.add(s, strings.Repeat(" ", w-l.w))
	}
	return l
}

func (l *lineBuf) String() string {
	l.end()
	return l.sb.String()
}

// styleOff is what Style.Render ends a styled text with.
const styleOff = "\x1b[m"

// styleKey tells apart the styles that lineBuf writes by itself: those of a
// color, a background, and bold, italic, faint, reversed or blinking text.
type styleKey struct {
	fg, bg color.Color
	attrs  uint8
}

// stylesOn holds the escape sequence that switches each such style on, as
// Style.Render writes it. Rendering works out much more than that sequence,
// far too much to do it for every cell of every frame.
var stylesOn = struct {
	sync.Mutex
	m map[styleKey]string
}{m: map[styleKey]string{}}

// styleOn returns the escape sequence that switches s on, "" for a style
// that changes nothing. It reports false for a style that takes more than a
// sequence in front of the text: underlined and struck through text, which
// Style.Render styles letter by letter.
func styleOn(s lipgloss.Style) (string, bool) {
	if s.GetUnderline() || s.GetStrikethrough() {
		return "", false
	}
	k := styleKey{fg: s.GetForeground(), bg: s.GetBackground()}
	for i, on := range [...]bool{s.GetBold(), s.GetItalic(), s.GetFaint(), s.GetReverse(), s.GetBlink()} {
		if on {
			k.attrs |= 1 << i
		}
	}
	stylesOn.Lock()
	defer stylesOn.Unlock()
	if on, ok := stylesOn.m[k]; ok {
		return on, true
	}
	const probe = "x"
	on, ok := strings.CutSuffix(s.Render(probe), probe+styleOff)
	if !ok {
		if on, ok = strings.CutSuffix(s.Render(probe), probe); !ok || on != "" {
			return "", false // a style that does more than this knows of
		}
	}
	if len(stylesOn.m) >= 1<<14 {
		clear(stylesOn.m) // the blends of every size of terminal add up
	}
	stylesOn.m[k] = on
	return on, true
}

// cells returns the printable width of text.
func cells(text string) int {
	for i := 0; i < len(text); i++ {
		if c := text[i]; c < ' ' || c > '~' {
			return ansi.StringWidth(text)
		}
	}
	return len(text) // plain ASCII: a cell each
}

// truncate shortens plain text to at most w cells, ending in an ellipsis.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}

// fit pads or truncates an (optionally styled) string to exactly w cells.
func fit(s string, w int) string {
	sw := ansi.StringWidth(s)
	if sw > w {
		return ansi.Truncate(s, w, "")
	}
	return s + strings.Repeat(" ", w-sw)
}

// center centers plain text in w cells.
func center(s string, w int) string {
	s = truncate(s, w)
	left := (w - ansi.StringWidth(s)) / 2
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", w-left-ansi.StringWidth(s))
}

// panel describes a rounded, titled box.
type panel struct {
	width, height int
	title, right  string // pre-rendered title segments for the top border
	footL, footR  string // pre-rendered segments for the bottom border
	focused       bool
	// optional scroll indicator drawn on the right border
	total, offset, visible int
}

func (m *Model) renderPanel(p panel, body []string) string {
	t := m.theme
	bc := t.Border
	if p.focused {
		bc = t.BorderFocus
	}
	bs := fg(bc)
	inner := p.width - 2

	border := func(left, right, l, r string) string {
		var lb lineBuf
		lb.add(bs, left)
		used := 0
		if l != "" {
			lb.add(bs, "─").raw(l, ansi.StringWidth(l))
			used += 1 + ansi.StringWidth(l)
		}
		rw := 0
		if r != "" {
			rw = ansi.StringWidth(r) + 1
		}
		if fill := inner - used - rw; fill > 0 {
			lb.add(bs, strings.Repeat("─", fill))
		}
		if r != "" {
			lb.raw(r, rw-1).add(bs, "─")
		}
		lb.add(bs, right)
		return lb.String()
	}

	rows := p.height - 2
	thumbStart, thumbEnd := -1, -1
	if p.total > p.visible && p.visible > 0 && rows > 0 {
		size := max(1, rows*p.visible/p.total)
		maxOff := p.total - p.visible
		thumbStart = (rows - size) * p.offset / max(1, maxOff)
		thumbEnd = thumbStart + size
	}

	out := make([]string, 0, p.height)
	out = append(out, border("╭", "╮", p.title, p.right))
	side, thumb := bs.Render("│"), fg(t.Accent).Render("┃")
	for i := 0; i < rows; i++ {
		line := ""
		if i < len(body) {
			line = body[i]
		}
		rb := side
		if i >= thumbStart && i < thumbEnd {
			rb = thumb
		}
		out = append(out, side+fit(line, inner)+rb)
	}
	out = append(out, border("╰", "╯", p.footL, p.footR))
	return strings.Join(out, "\n")
}

// gradient returns n colors blended through the given stops.
func gradient(n int, stops ...color.Color) []color.Color {
	if n <= 0 {
		return nil
	}
	if n == 1 {
		return []color.Color{stops[0]}
	}
	return blended(false, n, stops, func() []color.Color { return lipgloss.Blend1D(n, stops...) })
}

// ramp returns n colors spread evenly over the stops. Unlike gradient it
// always runs from the first stop to the last, however few colors are
// asked for, so a short rainbow still has both ends.
func ramp(n int, stops ...color.Color) []color.Color {
	if n <= 0 || len(stops) < 2 {
		return gradient(n, stops...)
	}
	return blended(true, n, stops, func() []color.Color {
		cs := make([]colorful.Color, len(stops))
		for i, s := range stops {
			cs[i], _ = colorful.MakeColor(s)
		}
		out := make([]color.Color, n)
		for i := range out {
			pos := 0.0
			if n > 1 {
				pos = float64(i) / float64(n-1) * float64(len(cs)-1)
			}
			k := min(int(pos), len(cs)-2)
			out[i] = cs[k].BlendLab(cs[k+1], pos-float64(k)).Clamped()
		}
		return out
	})
}

// blendKey names a gradient or a ramp of n colors through up to eight
// stops.
type blendKey struct {
	ramp  bool
	n     int
	count int
	stops [8]color.Color
}

// blends keeps the gradients and ramps worked out so far. The display asks
// for the same handful on every frame, and blending colors takes a
// conversion to another color space and back for each of them.
var blends = struct {
	sync.Mutex
	m map[blendKey][]color.Color
}{m: map[blendKey][]color.Color{}}

// blended returns the colors that blend works out for the key, a copy for
// the caller to keep or change.
func blended(ramp bool, n int, stops []color.Color, blend func() []color.Color) []color.Color {
	k := blendKey{ramp: ramp, n: n, count: len(stops)}
	if len(stops) > len(k.stops) {
		return blend()
	}
	copy(k.stops[:], stops)
	blends.Lock()
	defer blends.Unlock()
	colors, ok := blends.m[k]
	if !ok {
		if len(blends.m) >= 1<<12 {
			clear(blends.m)
		}
		colors = blend()
		blends.m[k] = colors
	}
	return slices.Clone(colors)
}
