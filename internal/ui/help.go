package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type helpSection struct {
	title string
	rows  [][2]string
}

func (m *Model) helpSections() []helpSection {
	step := int(m.opts.SeekStep.Seconds())
	playback := [][2]string{
		{"space p", "play / pause"},
		{"s v", "stop"},
		{"z b", "previous · next  (< > n)"},
		{"← →", fmt.Sprintf("rewind · forward %ds", step)},
		{"⇧← ⇧→", fmt.Sprintf("the same by %ds  ([ ])", 6*step)},
		{"+ - m", "volume · mute"},
		{"e", "eject the tape"},
	}
	if m.remote != nil {
		playback = append(playback, [2]string{"media keys", "play · previous · next"})
	}
	return []helpSection{
		{"PLAYBACK", playback},
		{"LIBRARY", [][2]string{
			{"↑↓ jk", "move  (pgup pgdn g G)"},
			{"⏎ l", "open folder · play track"},
			{"⌫ h ~", "parent folder · root"},
			{"a", "add to playlist"},
			{"A", "play, replacing playlist"},
			{"/", "filter  (esc clears)"},
			{"F", "search the whole library"},
			{"f", "find the playing track"},
		}},
		{"PLAYLIST", [][2]string{
			{"tab 1 2", "library · playlist"},
			{"⏎", "play selected track"},
			{"d x", "remove track"},
			{"c", "clear all"},
			{"K J", "move track up · down"},
			{"u", "undo the last change"},
		}},
		{"MODES", [][2]string{
			{"S", "shuffle"},
			{"r", "repeat off · all · one"},
			{"i", "elapsed · remaining time"},
			{"T", "tape sound"},
			{"t", "next theme"},
			{"q", "quit"},
		}},
		{"MOUSE", [][2]string{
			{"click", "deck keys · tabs · path"},
			{"drag", "seek bar · volume"},
			{"wheel", "scroll lists · volume"},
			{"dbl-click", "open folder · play"},
			{"right-click", "add to playlist"},
			{"cassette", "play / pause"},
			{"LCD", "time mode · SHUF · TAPE · REP"},
		}},
	}
}

// helpBlock renders one section as fixed-width lines.
func (m *Model) helpBlock(s helpSection) []string {
	t := m.theme
	kw, dw := 0, 0
	for _, r := range s.rows {
		kw = max(kw, ansi.StringWidth(r[0]))
		dw = max(dw, ansi.StringWidth(r[1]))
	}
	w := kw + 2 + dw
	lines := []string{fit(fg(t.Accent2).Bold(true).Render(s.title), w)}
	for _, r := range s.rows {
		lines = append(lines, fit(fg(t.Accent).Render(fit(r[0], kw))+"  "+fg(t.Muted).Render(r[1]), w))
	}
	return lines
}

// renderHelp draws the full screen key reference. Sections flow into as many
// columns as fit; what does not fit vertically can be scrolled.
func (m *Model) renderHelp(w, h int) string {
	t := m.theme
	inner, rows := w-4, h-2
	var blocks [][]string
	colW, total := 0, 0
	for _, s := range m.helpSections() {
		b := m.helpBlock(s)
		blocks = append(blocks, b)
		colW = max(colW, ansi.StringWidth(b[0]))
		total += len(b) + 1
	}
	const gap = 4
	ncols := max(1, min(len(blocks), (inner+gap)/(colW+gap)))
	target := max(rows, (total+ncols-1)/ncols)

	var cols [][]string
	var col []string
	for _, b := range blocks {
		if len(col) > 0 && len(col)+1+len(b) > target && len(cols) < ncols-1 {
			cols = append(cols, col)
			col = nil
		}
		if len(col) > 0 {
			col = append(col, "")
		}
		col = append(col, b...)
	}
	cols = append(cols, col)

	height := 0
	for _, c := range cols {
		height = max(height, len(c))
	}
	m.helpOffset = max(0, min(m.helpOffset, height-rows))
	pad := max(0, (inner-(len(cols)*(colW+gap)-gap))/2)
	body := make([]string, 0, rows)
	for y := m.helpOffset; y < height && len(body) < rows; y++ {
		var sb strings.Builder
		sb.WriteString(strings.Repeat(" ", pad+1))
		for i, c := range cols {
			if i > 0 {
				sb.WriteString(strings.Repeat(" ", gap))
			}
			line := ""
			if y < len(c) {
				line = c[y]
			}
			sb.WriteString(fit(line, colW))
		}
		body = append(body, sb.String())
	}

	p := panel{
		width: w, height: h, focused: true,
		title: lipgloss.NewStyle().Foreground(t.OnAccent).Background(t.Accent).Bold(true).Render(" ◉◉ CASSETTY · KEYS "),
		footR: fg(t.Muted).Render(" ? or esc to close "),
		total: height, offset: m.helpOffset, visible: rows,
	}
	if height > rows {
		p.footL = fg(t.Muted).Render(" ↑↓ scroll ")
	}
	return m.renderPanel(p, body)
}
