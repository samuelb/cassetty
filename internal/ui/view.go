package ui

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone/v2"
)

const (
	minWidth  = 72
	minHeight = 22
)

type layout struct {
	tooSmall     bool
	cassW, cassH int
	lcdW         int
	playerH      int
	listH        int
	listRows     int
	specBars     int
}

func (m *Model) relayout() {
	w, h := m.width, m.height
	l := layout{}
	if w < minWidth || h < minHeight {
		l.tooSmall = true
		m.lay = l
		return
	}
	l.cassH = cassetteHeight(h)
	content := w - 4
	l.cassW = 44
	if w >= 120 {
		l.cassW = 50
	}
	if content-l.cassW-2 < 34 {
		l.cassW = max(34, content-2-34)
	}
	l.lcdW = content - l.cassW - 2
	l.playerH = l.cassH + 4
	l.listH = h - l.playerH - 1
	l.listRows = l.listH - 2
	l.specBars, _ = spectrumLayout(l.lcdW - 4)
	m.lay = l
	m.ensureVisible()
	m.ensurePlaylistVisible()
}

func (m *Model) View() tea.View {
	var v tea.View
	v.AltScreen = true
	v.MouseMode = tea.MouseModeAllMotion
	v.WindowTitle = "cassetty"
	if m.src != nil {
		v.WindowTitle = map[playState]string{statePlaying: "▶ ", statePaused: "▮▮ ", stateStopped: "■ "}[m.state] +
			m.trackDisplay() + " · cassetty"
	}
	if m.opts.PaintBackground || m.theme.Light {
		v.BackgroundColor = m.theme.Bg
	}
	if m.width == 0 {
		return v
	}
	if m.lay.tooSmall {
		v.SetContent(m.renderTooSmall())
		return v
	}
	var content string
	if m.showHelp {
		content = m.renderHelp(m.width, m.height-1) + "\n" + m.renderFooter()
	} else {
		content = m.renderPlayer() + "\n" + m.renderBrowser(m.width, m.lay.listH) + "\n" + m.renderFooter()
	}
	v.SetContent(zone.Scan(content))
	return v
}

func (m *Model) renderTooSmall() string {
	t := m.theme
	lines := make([]string, m.height)
	for i := range lines {
		lines[i] = strings.Repeat(" ", m.width)
	}
	mid := m.height / 2
	if mid-1 >= 0 {
		lines[mid-1] = fg(t.Accent).Bold(true).Render(center("◉◉ CASSETTY", m.width))
	}
	if mid < m.height {
		lines[mid] = fg(t.Muted).Render(center(fmt.Sprintf("enlarge the terminal to at least %d×%d (now %d×%d)",
			minWidth, minHeight, m.width, m.height), m.width))
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderPlayer() string {
	t := m.theme
	l := m.lay
	cass := m.renderCassette(l.cassW, l.cassH)
	lcd := m.renderLCD(l.lcdW, l.cassH)
	body := make([]string, 0, l.cassH+2)
	for i := 0; i < l.cassH; i++ {
		body = append(body, " "+cass[i]+"  "+lcd[i])
	}
	body = append(body, " "+m.renderControls(m.width-4))
	body = append(body, " "+m.renderSeek(m.width-4))

	title := fg(t.Accent).Bold(true).Render(" ◉◉ CASSETTY ")
	right := ""
	switch {
	case m.engine.Silent() && m.opts.Notice != "":
		right = fg(t.Error).Render(" no audio device · silent ")
	case m.engine.Silent():
		right = fg(t.Muted).Render(" silent mode ")
	case m.loading:
		right = fg(t.Muted).Render(" loading… ")
	}
	return m.renderPanel(panel{width: m.width, height: l.playerH, title: title, right: right}, body)
}

func (m *Model) renderFooter() string {
	t := m.theme
	w := m.width
	right := ""
	if m.flash != "" && time.Now().Before(m.flashTill) {
		st := fg(t.Accent2)
		if m.flashErr {
			st = fg(t.Error)
		}
		right = st.Render(truncate(m.flash, w/2)) + " "
	}
	rw := ansi.StringWidth(right)
	var left string
	if m.filtering {
		k, d := fg(t.Accent), fg(t.Muted)
		left = " " + k.Render("type") + d.Render(" to filter · ") + k.Render("⏎") + d.Render(" accept · ") +
			k.Render("esc") + d.Render(" clear · ") + k.Render("↑↓") + d.Render(" move")
	} else {
		left = " " + m.shortHelpLine(max(10, w-rw-2))
	}
	gap := 0
	if rw > 0 {
		gap = 2 // keep the hints clear of the message
	}
	left = ansi.Truncate(left, max(0, w-rw-gap), "")
	return left + strings.Repeat(" ", max(0, w-ansi.StringWidth(left)-rw)) + right
}

func zoneMark(id, s string) string { return zone.Mark(id, s) }

// --- mouse -------------------------------------------------------------------

func inZone(id string, msg tea.MouseMsg) bool { return zone.Get(id).InBounds(msg) }

// zoneFrac returns the horizontal position of the mouse inside zone id as a
// fraction in [0, 1], clamped, even when the pointer left the zone.
func zoneFrac(id string, msg tea.MouseMsg) (float64, bool) {
	z := zone.Get(id)
	if z.IsZero() {
		return 0, false
	}
	w := z.EndX - z.StartX
	if w <= 0 {
		return 0, false
	}
	x := msg.Mouse().X - z.StartX
	return max(0, min(1, float64(x)/float64(w))), true
}

// listIndex maps a mouse position to a row of the browser list.
func (m *Model) listIndex(msg tea.MouseMsg) (int, bool) {
	mo := msg.Mouse()
	top := m.lay.playerH + 1
	if mo.Y < top || mo.Y >= top+m.lay.listRows || mo.X < 1 || mo.X >= m.width-1 {
		return 0, false
	}
	off := m.offset
	if m.tab == tabPlaylist {
		off = m.plOffset
	}
	return off + mo.Y - top, true
}

func (m *Model) overCassette(msg tea.MouseMsg) bool {
	mo := msg.Mouse()
	return mo.X >= 2 && mo.X < 2+m.lay.cassW && mo.Y >= 1 && mo.Y < 1+m.lay.cassH
}

func (m *Model) crumbAt(msg tea.MouseMsg) (string, bool) {
	for d := m.dir; within(m.root, d); {
		if inZone(crumbID(d), msg) {
			return d, true
		}
		if d == m.root {
			break
		}
		d = filepath.Dir(d)
	}
	return "", false
}

func (m *Model) handleMouse(msg tea.MouseMsg) tea.Cmd {
	if m.lay.tooSmall {
		return nil
	}
	if m.showHelp {
		if w, ok := msg.(tea.MouseWheelMsg); ok {
			if w.Button == tea.MouseWheelUp {
				m.helpOffset = max(0, m.helpOffset-2)
			} else if w.Button == tea.MouseWheelDown {
				m.helpOffset += 2
			}
		}
		return nil
	}
	switch msg := msg.(type) {
	case tea.MouseMotionMsg:
		if m.drag != "" {
			m.updateDrag(msg)
			return nil
		}
		m.updateHover(msg)
	case tea.MouseReleaseMsg:
		return m.endDrag(msg)
	case tea.MouseWheelMsg:
		return m.wheel(msg)
	case tea.MouseClickMsg:
		return m.click(msg)
	}
	return nil
}

func (m *Model) updateHover(msg tea.MouseMsg) {
	m.hover, m.hoverRow = "", -1
	for _, b := range transport {
		if inZone(b.id, msg) {
			m.hover = b.id
			return
		}
	}
	for _, id := range []string{"tab:lib", "tab:pl", "vol"} {
		if inZone(id, msg) {
			m.hover = id
			return
		}
	}
	if f, ok := zoneFrac("seek", msg); ok && inZone("seek", msg) {
		m.hover, m.hoverFrac = "seek", f
		return
	}
	if d, ok := m.crumbAt(msg); ok {
		m.hover = crumbID(d)
		return
	}
	if i, ok := m.listIndex(msg); ok && !m.showHelp {
		m.hover, m.hoverRow = "list", i
	}
}

func (m *Model) updateDrag(msg tea.MouseMsg) {
	if f, ok := zoneFrac(m.drag, msg); ok {
		m.dragFrac = f
	}
}

func (m *Model) endDrag(msg tea.MouseMsg) tea.Cmd {
	if m.drag == "" {
		return nil
	}
	m.updateDrag(msg)
	switch m.drag {
	case "seek":
		if m.src != nil {
			m.seekTo(time.Duration(m.dragFrac * float64(m.src.Length)))
		}
	case "vol":
		m.setVolume(int(m.dragFrac*100 + 0.5))
	}
	m.drag = ""
	return nil
}

func (m *Model) pressButton(id string) tea.Cmd {
	switch id {
	case "btn:prev":
		return m.prev()
	case "btn:rew":
		return m.seekBy(-6 * m.opts.SeekStep)
	case "btn:play":
		return m.play()
	case "btn:pause":
		return m.pause()
	case "btn:stop":
		return m.stop()
	case "btn:ff":
		return m.seekBy(6 * m.opts.SeekStep)
	case "btn:next":
		return m.next()
	case "btn:eject":
		return m.eject()
	}
	return nil
}

func (m *Model) click(msg tea.MouseClickMsg) tea.Cmd {
	mo := msg.Mouse()
	if mo.Button == tea.MouseRight {
		if i, ok := m.listIndex(msg); ok && m.tab == tabLibrary && !m.showHelp && i < len(m.visible) {
			m.cursor = i
			return m.enqueue()
		}
		return nil
	}
	if mo.Button != tea.MouseLeft {
		return nil
	}

	for _, b := range transport {
		if inZone(b.id, msg) {
			return m.pressButton(b.id)
		}
	}
	switch {
	case inZone("seek", msg):
		if m.src == nil || m.src.Length <= 0 {
			return nil
		}
		m.drag = "seek"
		m.updateDrag(msg)
		return m.kickTick()
	case inZone("vol", msg):
		m.drag = "vol"
		m.updateDrag(msg)
		m.setVolume(int(m.dragFrac*100 + 0.5))
		return m.kickTick()
	case inZone("shuffle", msg):
		m.shuffle = !m.shuffle
		m.resetOrder()
		return nil
	case inZone("repeat", msg):
		m.repeat = (m.repeat + 1) % 3
		return nil
	case inZone("tape", msg):
		m.toggleTape()
		return nil
	case inZone("time:0", msg), inZone("time:1", msg), inZone("time:2", msg):
		m.remaining = !m.remaining
		return nil
	case inZone("tab:lib", msg):
		m.showHelp = false
		m.tab = tabLibrary
		return nil
	case inZone("tab:pl", msg):
		m.showHelp = false
		if m.tab != tabPlaylist {
			m.switchTab()
		}
		return nil
	case m.overCassette(msg):
		return m.togglePlay()
	}
	if d, ok := m.crumbAt(msg); ok {
		return m.loadDir(d, "")
	}

	i, ok := m.listIndex(msg)
	if !ok || m.showHelp {
		return nil
	}
	clickKey := fmt.Sprintf("%d:%d", m.tab, i)
	double := clickKey == m.lastClickKey && time.Since(m.lastClick) < 450*time.Millisecond
	m.lastClick, m.lastClickKey = time.Now(), clickKey
	if double {
		m.lastClickKey = ""
	}
	if m.tab == tabLibrary {
		if i >= len(m.visible) {
			return nil
		}
		m.cursor = i
		if double {
			return m.open()
		}
		return nil
	}
	if i >= len(m.playlist) {
		return nil
	}
	m.plCursor = i
	if double {
		m.failures = 0
		return m.playTrack(m.playlist[i], true)
	}
	return nil
}

func (m *Model) wheel(msg tea.MouseWheelMsg) tea.Cmd {
	mo := msg.Mouse()
	delta := 0
	switch mo.Button {
	case tea.MouseWheelUp:
		delta = -1
	case tea.MouseWheelDown:
		delta = 1
	default:
		return nil
	}
	if mo.Y < m.lay.playerH {
		switch {
		case inZone("seek", msg):
			return m.seekBy(time.Duration(-delta) * m.opts.SeekStep)
		default:
			m.setVolume(m.volume - delta*2)
		}
		return nil
	}
	if _, ok := m.listIndex(msg); !ok || m.showHelp {
		return nil
	}
	rows := m.listRows()
	if m.tab == tabLibrary {
		m.offset = max(0, min(m.offset+3*delta, len(m.visible)-rows))
		m.cursor = max(m.offset, min(m.cursor, m.offset+rows-1))
	} else {
		m.plOffset = max(0, min(m.plOffset+3*delta, len(m.playlist)-rows))
		m.plCursor = max(m.plOffset, min(m.plCursor, m.plOffset+rows-1))
	}
	m.updateHover(msg)
	return nil
}

// Run starts the interface and blocks until the user quits. It returns the
// state to remember for the next run.
func Run(opts Options) (*Model, error) {
	zone.NewGlobal()
	defer zone.Close()
	m := New(opts)
	defer m.Close()
	p := tea.NewProgram(m)
	// Closing the terminal hangs up on cassetty, which would otherwise end
	// it on the spot: quit as usual instead, so that the state is saved.
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-hup:
			p.Quit()
		case <-done:
		}
	}()
	_, err := p.Run()
	return m, err
}
