package ui

import (
	"fmt"
	"image/color"
	"math"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone/v2"
)

// Seven segment digits built from heavy box drawing characters.
var bigDigits = [10][3]string{
	{"┏━┓", "┃ ┃", "┗━┛"},
	{"  ╻", "  ┃", "  ╹"},
	{"╺━┓", "┏━┛", "┗━╸"},
	{"╺━┓", " ━┫", "╺━┛"},
	{"╻ ╻", "┗━┫", "  ╹"},
	{"┏━╸", "┗━┓", "╺━┛"},
	{"┏━╸", "┣━┓", "┗━┛"},
	{"╺━┓", "  ┃", "  ╹"},
	{"┏━┓", "┣━┫", "┗━┛"},
	{"┏━┓", "┗━┫", "╺━┛"},
}

// ghostDigit is drawn behind lit segments, like the unlit cells of an LCD.
var ghostDigit = [3]string{"┏━┓", "┣━┫", "┗━┛"}

var colonGlyph = [3]string{"╻", " ", "╹"}

const bigTimeWidth = 17 // "mm:ss" with spacing

// bigTime renders d as three rows of seven segment digits.
func (m *Model) bigTime(d time.Duration, lit bool) [3]string {
	t := m.theme
	on := fgbg(t.LCDFg, t.LCDBg)
	off := fgbg(t.LCDGhost, t.LCDBg)
	secs := int(d.Seconds())
	mins := min(99, secs/60)
	digits := []int{mins / 10, mins % 10, -1, secs % 60 / 10, secs % 10}

	var rows [3]lineBuf
	for i, dg := range digits {
		if i > 0 {
			for r := range rows {
				rows[r].add(off, " ")
			}
		}
		for r := range rows {
			if dg < 0 {
				st := on
				if !lit {
					st = off
				}
				rows[r].add(st, colonGlyph[r])
				continue
			}
			glyph := []rune(bigDigits[dg][r])
			ghost := []rune(ghostDigit[r])
			for k := range glyph {
				if lit && glyph[k] != ' ' {
					rows[r].add(on, string(glyph[k]))
				} else {
					rows[r].add(off, string(ghost[k]))
				}
			}
		}
	}
	return [3]string{rows[0].String(), rows[1].String(), rows[2].String()}
}

// renderLCD draws the display panel: time, stream info, marquee, spectrum
// analyzer and VU meters, in exactly h lines of w cells.
func (m *Model) renderLCD(w, h int) []string {
	t := m.theme
	frame := fg(t.LCDFrame)
	bg := fgbg(t.LCDFg, t.LCDBg)
	text := fgbg(t.LCDText, t.LCDBg)
	ghost := fgbg(t.LCDGhost, t.LCDBg)
	inner := w - 4 // frame + one cell padding on each side

	var rows []string
	add := func(lb *lineBuf) {
		lb.padTo(bg, inner)
		rows = append(rows, frame.Render("│")+bg.Render(" ")+lb.String()+bg.Render(" ")+frame.Render("│"))
	}

	// Time block with state glyph and stream information.
	shown := m.displayPos()
	if m.remaining && m.src != nil && m.src.Length > 0 {
		shown = max(0, m.src.Length-shown)
	}
	lit := m.src != nil && !(m.state == statePaused && int(m.blink*2)%2 == 1)
	digits := m.bigTime(shown, lit)
	glyph := map[playState]string{statePlaying: "▶", statePaused: "▮▮", stateStopped: "■"}[m.state]
	if m.src == nil {
		glyph = ""
	}
	gap := 2
	if inner < 34 {
		gap = 1
	}
	infoW := inner - 3 - bigTimeWidth - gap
	info := m.lcdInfo(infoW)
	for r := 0; r < 3; r++ {
		var lb lineBuf
		switch {
		case r == 0:
			lb.add(bg.Bold(true), fit(glyph, 3))
		case r == 1 && m.remaining:
			lb.add(bg, " ━ ")
		default:
			lb.add(bg, "   ")
		}
		lb.raw(zone.Mark(fmt.Sprintf("time:%d", r), digits[r]), bigTimeWidth)
		lb.add(bg, strings.Repeat(" ", gap))
		if infoW > 0 {
			if r == 2 {
				m.lcdFlags(&lb, infoW)
			} else {
				lb.add(text, truncate(info[r], infoW))
			}
		}
		add(&lb)
	}

	// Marquee.
	{
		var lb lineBuf
		lb.add(text, "≋ ")
		lb.add(bg, m.marqueeText(inner-2))
		add(&lb)
	}

	// Spectrum analyzer and VU meters share the remaining rows.
	left := h - 2 - len(rows)
	vuRows := 2
	if left < 4 {
		vuRows = 1
	}
	specRows := left - vuRows
	for _, line := range m.spectrumRows(inner, specRows) {
		var lb lineBuf
		lb.raw(line, inner)
		add(&lb)
	}
	if vuRows == 2 {
		for i, ch := range []string{"L", "R"} {
			var lb lineBuf
			lb.add(text, ch+" ")
			lvl, pk := m.vu.L, m.vu.PeakL
			if i == 1 {
				lvl, pk = m.vu.R, m.vu.PeakR
			}
			m.vuBar(&lb, inner-2, lvl, pk)
			add(&lb)
		}
	} else {
		var lb lineBuf
		half := (inner - 1) / 2
		lb.add(text, "L ")
		m.vuBar(&lb, half-2, m.vu.L, m.vu.PeakL)
		lb.add(ghost, " ").add(text, "R ")
		m.vuBar(&lb, inner-lb.w, m.vu.R, m.vu.PeakR)
		add(&lb)
	}

	top := frame.Render("╭" + strings.Repeat("─", w-2) + "╮")
	bottom := frame.Render("╰" + strings.Repeat("─", w-2) + "╯")
	return append(append([]string{top}, rows...), bottom)
}

// lcdInfo returns the two stream info lines, in a compact form when the
// display is narrow.
func (m *Model) lcdInfo(w int) [2]string {
	s := m.src
	if s == nil {
		return [2]string{"NO TAPE", ""}
	}
	khz := fmt.Sprintf("%g", math.Round(float64(s.SampleRate)/100)/10)
	ch := "MONO"
	switch {
	case s.Channels == 2:
		ch = "STEREO"
	case s.Channels > 2:
		ch = fmt.Sprintf("%d CH", s.Channels)
	}
	if w < 17 {
		first := s.Codec
		if s.Bitrate > 0 {
			first += fmt.Sprintf(" %dk", s.Bitrate)
		}
		return [2]string{first, khz + "k " + ch[:min(2, len(ch))]}
	}
	first := s.Codec
	if s.Bitrate > 0 {
		first += fmt.Sprintf(" · %d kbps", s.Bitrate)
	}
	return [2]string{first, khz + " kHz · " + ch}
}

// lcdFlags draws the clickable SHUF, TAPE and REP indicators. TAPE comes
// before REP, whose label changes width, and is left out on the narrowest
// displays.
func (m *Model) lcdFlags(lb *lineBuf, w int) {
	t := m.theme
	on := fgbg(t.LCDFg, t.LCDBg).Bold(true)
	off := fgbg(t.LCDGhost, t.LCDBg)
	st := func(b bool) lipgloss.Style {
		if b {
			return on
		}
		return off
	}
	shuf, tape := "SHUF", "TAPE"
	rep := map[repeatMode]string{repeatOff: "REP", repeatAll: "REP·ALL", repeatOne: "REP·ONE"}[m.repeat]
	if w < len([]rune("SHUF TAPE REP·ALL")) {
		shuf, tape = "SH", "TP"
		rep = map[repeatMode]string{repeatOff: "RP", repeatAll: "RP·A", repeatOne: "RP·1"}[m.repeat]
		if w < len([]rune("SH TP RP·A")) {
			tape = ""
		}
	}
	if w < len(shuf)+1+len([]rune(rep)) {
		return
	}
	lb.raw(zone.Mark("shuffle", st(m.shuffle).Render(shuf)), len(shuf))
	lb.add(off, " ")
	if tape != "" {
		lb.raw(zone.Mark("tape", st(m.tape).Render(tape)), len(tape))
		lb.add(off, " ")
	}
	lb.raw(zone.Mark("repeat", st(m.repeat != repeatOff).Render(rep)), len([]rune(rep)))
}

// marqueeText returns the Winamp style scrolling title, in at most w cells.
func (m *Model) marqueeText(w int) string {
	if m.src == nil {
		return truncate("cassetty · insert a tape to begin", w)
	}
	title := m.trackDisplay()
	if n := m.currentIndex(); n >= 0 {
		title = fmt.Sprintf("%d. %s", n+1, title)
	}
	if m.src.Length > 0 {
		title += " (" + fmtDur(m.src.Length) + ")"
	}
	if ansi.StringWidth(title) <= w {
		return title
	}
	return scroll(title+"  ***  ", int(m.marquee), w)
}

// scroll returns w cells of s going round without end, from off cells into
// it. It goes by cells, not by characters: those of Chinese, Japanese and
// Korean and most emoji take two. One that either edge cuts in half shows
// as a blank, so that the text moves on cell by cell.
func scroll(s string, off, w int) string {
	type cluster struct {
		text  string
		width int
	}
	var loop []cluster
	total := 0
	for rest := s; rest != ""; {
		text, width := ansi.FirstGraphemeCluster(rest, ansi.GraphemeWidth)
		if text == "" {
			break
		}
		rest = rest[len(text):]
		loop = append(loop, cluster{text, width})
		total += width
	}
	if total <= 0 || w <= 0 {
		return ""
	}
	off %= total
	i, at := 0, 0 // the cluster the window starts in, and the cell it starts at
	for at+loop[i].width <= off {
		at += loop[i].width
		i++
	}
	var b strings.Builder
	filled := 0
	if at < off {
		filled = min(w, at+loop[i].width-off)
		b.WriteString(strings.Repeat(" ", filled))
		i++
	}
	for filled < w {
		c := loop[i%len(loop)]
		if filled+c.width > w {
			b.WriteString(strings.Repeat(" ", w-filled))
			break
		}
		b.WriteString(c.text)
		filled += c.width
		i++
	}
	return b.String()
}

var barChars = []string{" ", "▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}

// spectrumLayout returns the number of bars and the gap between them.
func spectrumLayout(w int) (bars, gap int) {
	if w >= 40 {
		return (w + 1) / 2, 1
	}
	return w, 0
}

// spectrumRows renders the analyzer bars into rows lines of w cells.
func (m *Model) spectrumRows(w, rows int) []string {
	t := m.theme
	if rows <= 0 {
		return nil
	}
	bars, gap := spectrumLayout(w)
	colors := gradient(rows, t.SpecHigh, t.SpecMid, t.SpecLow)
	if rows >= 3 {
		colors = gradient(rows, t.SpecHigh, t.SpecMid, t.SpecLow, t.SpecLow)
	}
	peak := fgbg(t.SpecPeak, t.LCDBg)
	ghost := fgbg(t.LCDGhost, t.LCDBg)
	blank := fgbg(t.LCDBg, t.LCDBg)
	// A Meter ramp colors the bars by frequency instead of by level.
	var barStyles []lipgloss.Style
	if len(t.Meter) > 0 {
		for _, col := range ramp(bars, t.Meter...) {
			barStyles = append(barStyles, fgbg(col, t.LCDBg))
		}
	}

	out := make([]string, rows)
	for r := 0; r < rows; r++ {
		var lb lineBuf
		rowStyle := fgbg(colors[r], t.LCDBg)
		lo := float64(rows-1-r) / float64(rows)
		for b := 0; b < bars; b++ {
			if b > 0 && gap > 0 {
				lb.add(blank, " ")
			}
			st := rowStyle
			if barStyles != nil {
				st = barStyles[b]
			}
			lvl, pk := 0.0, 0.0
			if b < len(m.spectrum.Levels) {
				lvl, pk = m.spectrum.Levels[b], m.spectrum.Peaks[b]
			}
			eighths := int(math.Round((lvl - lo) * float64(rows) * 8))
			switch {
			case eighths >= 8:
				lb.add(st, "█")
			case eighths > 0:
				lb.add(st, barChars[eighths])
			default:
				pe := (pk - lo) * float64(rows) * 8
				switch {
				case pk > 0.02 && pe >= 0 && pe < 8:
					ch := "▁"
					if pe >= 4 {
						ch = "▔"
					}
					lb.add(peak, ch)
				case r == rows-1:
					lb.add(ghost, "▁")
				default:
					lb.add(blank, " ")
				}
			}
		}
		lb.padTo(blank, w)
		out[r] = lb.String()
	}
	return out
}

// vuBar draws one LED style VU meter.
func (m *Model) vuBar(lb *lineBuf, w int, level, peak float64) {
	t := m.theme
	if w <= 0 {
		return
	}
	lit := int(math.Round(level * float64(w)))
	pk := int(math.Round(peak*float64(w))) - 1
	var cols []color.Color
	if len(t.Meter) > 0 {
		cols = ramp(w, t.Meter...)
	}
	for i := 0; i < w; i++ {
		f := float64(i) / float64(w)
		col := t.SpecLow
		switch {
		case cols != nil:
			col = cols[i]
		case f >= 0.88:
			col = t.SpecHigh
		case f >= 0.7:
			col = t.SpecMid
		}
		if i < lit || (i == pk && peak > 0.03) {
			lb.add(fgbg(col, t.LCDBg), "▮")
		} else {
			lb.add(fgbg(t.LCDGhost, t.LCDBg), "▮")
		}
	}
}

func fmtDur(d time.Duration) string {
	s := int(d.Seconds())
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}
