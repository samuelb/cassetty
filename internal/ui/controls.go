package ui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone/v2"
)

type transportButton struct {
	id, icon, label string
}

var transport = []transportButton{
	{"btn:prev", "▮◀", "PREV"},
	{"btn:rew", "◀◀", "REW"},
	{"btn:play", "▶", "PLAY"},
	{"btn:pause", "▮▮", "PAUSE"},
	{"btn:stop", "■", "STOP"},
	{"btn:ff", "▶▶", "FF"},
	{"btn:next", "▶▮", "NEXT"},
	{"btn:eject", "▲", "EJECT"},
}

// latched reports whether a deck key is "pressed down", like the mechanical
// piano keys of a tape deck.
func (m *Model) latched(id string) bool {
	switch id {
	case "btn:play":
		return m.state == statePlaying
	case "btn:pause":
		return m.state == statePaused
	case "btn:stop":
		return m.src != nil && m.state == stateStopped
	case "btn:rew":
		return m.spin < -0.5 || m.winding() < 0
	case "btn:ff":
		return m.spin > 0.5 || m.winding() > 0
	}
	return false
}

func (m *Model) buttonText(b transportButton, labels bool) string {
	if labels || b.id == "btn:play" {
		return " " + b.icon + " " + b.label + " "
	}
	return " " + b.icon + " "
}

func (m *Model) transportWidth(labels bool) int {
	w := 0
	for i, b := range transport {
		if i > 0 {
			w++
		}
		w += ansi.StringWidth(m.buttonText(b, labels))
	}
	return w
}

const volumeWidth = 22 // "VOL " + wedge + " 100%"

// renderControls draws the deck keys and the volume wedge in one line.
func (m *Model) renderControls(w int) string {
	t := m.theme
	labels := m.transportWidth(true)+volumeWidth+2 <= w
	var lb lineBuf
	for i, b := range transport {
		if i > 0 {
			lb.add(lipgloss.NewStyle(), " ")
		}
		st := lipgloss.NewStyle().Foreground(t.BtnFg).Background(t.BtnBg)
		switch {
		case m.latched(b.id):
			st = lipgloss.NewStyle().Foreground(t.OnAccent).Background(t.Accent).Bold(true)
		case m.hover == b.id:
			st = lipgloss.NewStyle().Foreground(t.Text).Background(t.BtnHover)
		}
		txt := m.buttonText(b, labels)
		lb.raw(zone.Mark(b.id, st.Render(txt)), ansi.StringWidth(txt))
	}
	if gap := w - lb.w - volumeWidth; gap > 0 {
		lb.add(lipgloss.NewStyle(), strings.Repeat(" ", gap))
	}
	m.renderVolume(&lb)
	return lb.String()
}

const wedgeWidth = 12

func (m *Model) renderVolume(lb *lineBuf) {
	t := m.theme
	lb.add(fg(t.Muted), "VOL ")
	vol := m.volume
	if m.drag == "vol" {
		vol = int(math.Round(m.dragFrac * 100))
	}
	lit := int(math.Round(float64(vol) / 100 * wedgeWidth))
	cols := gradient(wedgeWidth, t.SpecLow, t.SpecMid, t.SpecHigh)
	if len(t.Meter) > 0 {
		cols = ramp(wedgeWidth, t.Meter...)
	}
	var wedge lineBuf
	for i := 0; i < wedgeWidth; i++ {
		h := 1 + i*7/(wedgeWidth-1)
		st := fg(t.Faint)
		if i < lit && !m.muted {
			st = fg(cols[i])
		}
		wedge.add(st, barChars[h])
	}
	lb.raw(zone.Mark("vol", wedge.String()), wedgeWidth)
	label := fmt.Sprintf(" %3d%%", vol)
	st := fg(t.Text)
	if m.muted {
		label, st = " MUTE", fg(t.Error).Bold(true)
	}
	lb.add(st, fit(label, volumeWidth-4-wedgeWidth))
}

// renderSeek draws "elapsed ━━━━●──── total" across w cells.
func (m *Model) renderSeek(w int) string {
	t := m.theme
	length := time.Duration(0)
	if m.src != nil {
		length = m.src.Length
	}
	pos := m.displayPos()
	left := fmtDur(pos)
	leftStyle := fg(t.Text)
	if m.hover == "seek" && length > 0 && m.drag == "" {
		left = fmtDur(time.Duration(m.hoverFrac * float64(length)))
		leftStyle = fg(t.Accent)
	}
	right := "--:--"
	if length > 0 {
		right = fmtDur(length)
	}
	lw := max(5, len(left))
	rw := max(5, len(right))
	barW := w - lw - rw - 4
	var lb lineBuf
	lb.add(leftStyle, fmt.Sprintf(" %*s ", lw, left))

	frac := 0.0
	if length > 0 {
		frac = math.Max(0, math.Min(1, float64(pos)/float64(length)))
	}
	knob := int(math.Round(frac * float64(barW-1)))
	var bar lineBuf
	if barW > 0 {
		cols := gradient(max(1, knob), t.Accent2, t.Accent)
		for i := 0; i < barW; i++ {
			switch {
			case m.src == nil:
				bar.add(fg(t.Faint), "─")
			case i < knob:
				bar.add(fg(cols[i]), "━")
			case i == knob:
				bar.add(fg(t.Text).Bold(true), "●")
			default:
				bar.add(fg(t.Faint), "─")
			}
		}
		lb.raw(zone.Mark("seek", bar.String()), barW)
	}
	lb.add(fg(t.Muted), fmt.Sprintf(" %-*s ", rw, right))
	return lb.String()
}
