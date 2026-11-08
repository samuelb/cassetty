package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/samuelb/cassetty/internal/library"
)

// --- library navigation ----------------------------------------------------

func (m *Model) loadDir(dir, focus string) tea.Cmd {
	formats, hidden := m.opts.Formats, m.opts.ShowHidden
	return func() tea.Msg {
		entries, err := library.ReadDir(dir, formats, hidden)
		return dirMsg{dir: dir, entries: entries, err: err, focus: focus}
	}
}

func (m *Model) applyDir(msg dirMsg) tea.Cmd {
	if msg.refresh {
		return m.applyRefresh(msg)
	}
	if msg.err != nil {
		if msg.dir == m.dir && m.entries == nil {
			m.dirErr = msg.err
		}
		m.setFlash(msg.err.Error(), true)
		return nil
	}
	if m.dir != msg.dir {
		if e, ok := m.selectedEntry(); ok {
			m.memory[m.dir] = e.Name
		}
	}
	m.dir, m.entries, m.dirErr = msg.dir, msg.entries, nil
	m.filter, m.filtering = "", false
	m.refilter()
	focus := msg.focus
	if focus == "" {
		focus = m.memory[m.dir]
	}
	m.cursor, m.offset = 0, 0
	for i, idx := range m.visible {
		if m.entries[idx].Name == focus {
			m.cursor = i
		}
	}
	m.ensureVisible()
	return m.watchDir()
}

func (m *Model) refilter() {
	m.visible = m.visible[:0]
	terms := strings.Fields(strings.ToLower(m.filter))
outer:
	for i, e := range m.entries {
		name := strings.ToLower(e.Name)
		for _, t := range terms {
			if !strings.Contains(name, t) {
				continue outer
			}
		}
		m.visible = append(m.visible, i)
	}
	m.cursor = min(m.cursor, max(0, len(m.visible)-1))
	m.ensureVisible()
}

func (m *Model) selectedEntry() (library.Entry, bool) {
	if m.cursor < 0 || m.cursor >= len(m.visible) {
		return library.Entry{}, false
	}
	return m.entries[m.visible[m.cursor]], true
}

func (m *Model) listRows() int { return max(1, m.lay.listRows) }

func (m *Model) ensureVisible() {
	rows := m.listRows()
	m.cursor = max(0, min(m.cursor, len(m.visible)-1))
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+rows {
		m.offset = m.cursor - rows + 1
	}
	m.offset = max(0, min(m.offset, len(m.visible)-rows))
}

func (m *Model) ensurePlaylistVisible() {
	rows := m.listRows()
	m.plCursor = max(0, min(m.plCursor, len(m.playlist)-1))
	if m.plCursor < m.plOffset {
		m.plOffset = m.plCursor
	}
	if m.plCursor >= m.plOffset+rows {
		m.plOffset = m.plCursor - rows + 1
	}
	m.plOffset = max(0, min(m.plOffset, len(m.playlist)-rows))
}

// open enters a folder or plays a file (the whole folder becomes the
// playlist, starting at the file).
func (m *Model) open() tea.Cmd {
	e, ok := m.selectedEntry()
	if !ok {
		return nil
	}
	if e.IsDir {
		return m.loadDir(e.Path, "")
	}
	return m.playFromDir(e)
}

func (m *Model) back() tea.Cmd {
	if m.dir == m.root || !within(m.root, m.dir) {
		m.setFlash("already at the library root", false)
		return nil
	}
	return m.loadDir(filepath.Dir(m.dir), filepath.Base(m.dir))
}

func (m *Model) enqueue() tea.Cmd {
	e, ok := m.selectedEntry()
	if !ok {
		return nil
	}
	if e.IsDir {
		return m.collect(e.Path, collectAppend)
	}
	m.setFlash("added "+trackName(e.Path), false)
	return m.appendTracks([]*Track{{Path: e.Path}})
}

func (m *Model) playAll() tea.Cmd {
	e, ok := m.selectedEntry()
	if !ok {
		return m.collect(m.dir, collectPlay)
	}
	if e.IsDir {
		return m.collect(e.Path, collectPlay)
	}
	return m.playFromDir(e)
}

// locate jumps the browser to the folder of the playing track.
func (m *Model) locate() tea.Cmd {
	if m.cur == nil || !within(m.root, m.cur.Path) {
		return nil
	}
	m.tab = tabLibrary
	return m.loadDir(filepath.Dir(m.cur.Path), filepath.Base(m.cur.Path))
}

func (m *Model) handleLibraryKey(msg tea.KeyPressMsg) tea.Cmd {
	k := m.keys
	switch {
	case key.Matches(msg, k.Up):
		m.cursor--
	case key.Matches(msg, k.Down):
		m.cursor++
	case key.Matches(msg, k.PageUp):
		m.cursor -= m.listRows()
	case key.Matches(msg, k.PageDown):
		m.cursor += m.listRows()
	case key.Matches(msg, k.Top):
		m.cursor = 0
	case key.Matches(msg, k.Bottom):
		m.cursor = len(m.visible) - 1
	case key.Matches(msg, k.Open):
		return m.open()
	case key.Matches(msg, k.Back):
		return m.back()
	case key.Matches(msg, k.Root):
		return m.loadDir(m.root, "")
	case key.Matches(msg, k.Enqueue):
		return m.enqueue()
	case key.Matches(msg, k.PlayAll):
		return m.playAll()
	case key.Matches(msg, k.Locate):
		return m.locate()
	case key.Matches(msg, k.Filter):
		m.filtering = true
	case msg.String() == "esc" && m.filter != "":
		m.filter = ""
		m.refilter()
	}
	m.ensureVisible()
	return nil
}

func (m *Model) handleFilterKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.filter, m.filtering = "", false
	case "enter":
		m.filtering = false
		if len(m.visible) == 1 {
			return m.open()
		}
		return nil
	case "backspace":
		if r := []rune(m.filter); len(r) > 0 {
			m.filter = string(r[:len(r)-1])
		}
	case "ctrl+u":
		m.filter = ""
	case "up", "ctrl+p":
		m.cursor--
		m.ensureVisible()
		return nil
	case "down", "ctrl+n":
		m.cursor++
		m.ensureVisible()
		return nil
	case "ctrl+c":
		return tea.Quit
	default:
		if t := msg.Key().Text; t != "" {
			m.filter += t
		}
	}
	m.refilter()
	return nil
}

func (m *Model) handlePlaylistKey(msg tea.KeyPressMsg) tea.Cmd {
	k := m.keys
	switch {
	case key.Matches(msg, k.Up):
		m.plCursor--
	case key.Matches(msg, k.Down):
		m.plCursor++
	case key.Matches(msg, k.PageUp):
		m.plCursor -= m.listRows()
	case key.Matches(msg, k.PageDown):
		m.plCursor += m.listRows()
	case key.Matches(msg, k.Top):
		m.plCursor = 0
	case key.Matches(msg, k.Bottom):
		m.plCursor = len(m.playlist) - 1
	case key.Matches(msg, k.Open):
		if m.plCursor < len(m.playlist) {
			m.failures = 0
			return m.playTrack(m.playlist[m.plCursor], true)
		}
	case key.Matches(msg, k.Remove):
		m.removeFromPlaylist(m.plCursor)
	case key.Matches(msg, k.Clear):
		if len(m.playlist) > 0 {
			m.playlist = nil
			m.resetOrder()
			m.plCursor, m.plOffset = 0, 0
			m.setFlash("playlist cleared", false)
		}
	case key.Matches(msg, k.MoveUp):
		m.movePlaylist(m.plCursor, -1)
	case key.Matches(msg, k.MoveDown):
		m.movePlaylist(m.plCursor, 1)
	case key.Matches(msg, k.Locate):
		if i := m.currentIndex(); i >= 0 {
			m.plCursor = i
		}
	case key.Matches(msg, k.Back):
		m.tab = tabLibrary
	}
	m.ensurePlaylistVisible()
	return nil
}

// --- rendering ---------------------------------------------------------------

// renderBrowser draws the library/playlist panel.
func (m *Model) renderBrowser(w, h int) string {
	t := m.theme
	inner := w - 2

	tabStyle := func(active bool, id string) lipgloss.Style {
		switch {
		case active:
			return lipgloss.NewStyle().Foreground(t.OnAccent).Background(t.Accent).Bold(true)
		case m.hover == id:
			return fg(t.Text)
		}
		return fg(t.Muted)
	}
	libLabel := " LIBRARY "
	plLabel := fmt.Sprintf(" PLAYLIST %d ", len(m.playlist))
	var title lineBuf
	title.raw(zoneMark("tab:lib", tabStyle(m.tab == tabLibrary, "tab:lib").Render(libLabel)), len(libLabel))
	title.add(fg(t.Border), "─")
	title.raw(zoneMark("tab:pl", tabStyle(m.tab == tabPlaylist, "tab:pl").Render(plLabel)), len(plLabel))

	p := panel{width: w, height: h, title: title.String(), focused: true}
	rows := h - 2

	var body []string
	switch {
	case m.tab == tabLibrary:
		p.right = m.breadcrumb(inner - title.w - 6)
		body = m.libraryRows(inner, rows)
		p.total, p.offset, p.visible = len(m.visible), m.offset, rows
		p.footL = m.filterLine()
		if n := len(m.visible); n > 0 {
			p.footR = fg(t.Muted).Render(fmt.Sprintf(" %d/%d ", m.cursor+1, n))
		}
	default:
		body = m.playlistRows(inner, rows)
		p.total, p.offset, p.visible = len(m.playlist), m.plOffset, rows
		if n := len(m.playlist); n > 0 {
			p.right = fg(t.Muted).Render(" " + m.playlistSummary() + " ")
			p.footR = fg(t.Muted).Render(fmt.Sprintf(" %d/%d ", m.plCursor+1, n))
		}
		var modes []string
		if m.shuffle {
			modes = append(modes, "shuffle")
		}
		if m.repeat != repeatOff {
			modes = append(modes, "repeat "+repeatNames[m.repeat])
		}
		if len(modes) > 0 {
			p.footL = fg(t.Accent2).Render(" " + strings.Join(modes, " · ") + " ")
		}
	}
	return m.renderPanel(p, body)
}

func (m *Model) playlistSummary() string {
	var total float64
	known := 0
	for _, tr := range m.playlist {
		if tr.Length > 0 {
			total += tr.Length.Seconds()
			known++
		}
	}
	s := plural(len(m.playlist), "track")
	if known == len(m.playlist) && known > 0 {
		s += " · " + fmtDur(time.Duration(total*float64(time.Second)))
	}
	return s
}

// breadcrumb renders the clickable path of the current folder, relative to
// the library root, shortened from the left to fit w cells.
func (m *Model) breadcrumb(w int) string {
	t := m.theme
	rel, _ := filepath.Rel(m.root, m.dir)
	parts := []string{"♫ " + filepath.Base(m.root)}
	paths := []string{m.root}
	if rel != "." && rel != "" {
		acc := m.root
		for _, s := range strings.Split(rel, string(filepath.Separator)) {
			acc = filepath.Join(acc, s)
			parts = append(parts, s)
			paths = append(paths, acc)
		}
	}
	sep := " › "
	// Drop leading segments until it fits.
	first := 0
	width := func(from int) int {
		n := 0
		if from > 0 {
			n += 2 // "… "
		}
		for i := from; i < len(parts); i++ {
			if i > from {
				n += len([]rune(sep))
			}
			n += ansi.StringWidth(parts[i])
		}
		return n + 2
	}
	for first < len(parts)-1 && width(first) > w {
		first++
	}
	var lb lineBuf
	lb.add(fg(t.Muted), " ")
	if first > 0 {
		lb.add(fg(t.Faint), "… ")
	}
	for i := first; i < len(parts); i++ {
		if i > first {
			lb.add(fg(t.Faint), sep)
		}
		label := parts[i]
		if i == len(parts)-1 {
			label = truncate(label, max(4, w-lb.w-1))
		}
		st := fg(t.Muted)
		switch {
		case i == len(parts)-1:
			st = fg(t.Text).Bold(true)
		case m.hover == crumbID(paths[i]):
			st = fg(t.Accent).Underline(true)
		}
		lb.raw(zoneMark(crumbID(paths[i]), st.Render(label)), ansi.StringWidth(label))
	}
	lb.add(fg(t.Muted), " ")
	return lb.String()
}

func crumbID(path string) string { return "crumb:" + path }

func (m *Model) filterLine() string {
	t := m.theme
	switch {
	case m.filtering:
		return fg(t.Accent).Render(" / ") + fg(t.Text).Render(m.filter) + fg(t.Accent).Render("▏ ")
	case m.filter != "":
		return fg(t.Muted).Render(" filter: ") + fg(t.Accent2).Render(m.filter) + fg(t.Faint).Render(" (esc clears) ")
	}
	return ""
}

func (m *Model) rowStyle(selected bool, i int) lipgloss.Style {
	t := m.theme
	switch {
	case selected:
		return lipgloss.NewStyle().Background(t.SelBg)
	case m.hoverRow == i && m.hover == "list":
		return lipgloss.NewStyle().Background(t.HoverBg)
	}
	return lipgloss.NewStyle()
}

// nowPlayingGlyph pulses with the music.
func (m *Model) nowPlayingGlyph() string {
	if m.state != statePlaying {
		return map[playState]string{statePaused: "▮", stateStopped: "■"}[m.state]
	}
	lvl := (m.vu.L + m.vu.R) / 2
	return barChars[3+min(5, int(lvl*6))]
}

func (m *Model) libraryRows(w, rows int) []string {
	t := m.theme
	if m.dirErr != nil {
		return m.emptyState(w, rows, fg(t.Error), "cannot read folder", m.dirErr.Error())
	}
	if len(m.visible) == 0 {
		if m.filter != "" {
			return m.emptyState(w, rows, fg(t.Muted), "nothing matches “"+m.filter+"”", "esc clears the filter")
		}
		return m.emptyState(w, rows, fg(t.Muted), "no audio files or folders here", "backspace goes to the parent folder")
	}
	playing := ""
	if m.cur != nil {
		playing = m.cur.Path
	}
	out := make([]string, 0, rows)
	for r := 0; r < rows; r++ {
		i := m.offset + r
		if i >= len(m.visible) {
			break
		}
		e := m.entries[m.visible[i]]
		sel := i == m.cursor
		base := m.rowStyle(sel, i)
		var lb lineBuf
		if sel {
			lb.add(base.Foreground(t.Accent), "▌")
		} else {
			lb.add(base, " ")
		}

		var icon, right string
		nameStyle := base.Foreground(t.Text)
		iconStyle := base.Foreground(t.Muted)
		switch {
		case e.IsDir:
			icon = "▸"
			iconStyle = base.Foreground(t.Accent2)
			nameStyle = nameStyle.Bold(true)
		case e.Path == playing:
			icon = m.nowPlayingGlyph()
			iconStyle = base.Foreground(t.Accent)
			nameStyle = base.Foreground(t.Accent).Bold(true)
			right = strings.ToUpper(e.Ext()) + "  " + humanSize(e.Size)
		default:
			icon = "♪"
			right = strings.ToUpper(e.Ext()) + "  " + humanSize(e.Size)
		}
		lb.add(iconStyle, " "+icon+" ")
		name := e.Name
		if e.IsDir {
			name += "/"
		}
		nameW := w - lb.w - ansi.StringWidth(right) - 3
		m.highlight(&lb, truncate(name, nameW), nameStyle, base)
		lb.padTo(base, w-ansi.StringWidth(right)-2)
		lb.add(base.Foreground(t.Faint), right).padTo(base, w)
		out = append(out, lb.String())
	}
	return out
}

// highlight writes name, emphasising the filter matches.
func (m *Model) highlight(lb *lineBuf, name string, st, base lipgloss.Style) {
	terms := strings.Fields(strings.ToLower(m.filter))
	if len(terms) == 0 {
		lb.add(st, name)
		return
	}
	runes := []rune(name)
	lower := []rune(strings.ToLower(name))
	mark := make([]bool, len(runes))
	if len(lower) == len(runes) {
		for _, term := range terms {
			tr := []rune(term)
			for i := 0; i+len(tr) <= len(lower); i++ {
				if string(lower[i:i+len(tr)]) == term {
					for j := range tr {
						mark[i+j] = true
					}
				}
			}
		}
	}
	hl := st.Foreground(m.theme.Accent).Underline(true)
	for i, r := range runes {
		if mark[i] {
			lb.add(hl, string(r))
		} else {
			lb.add(st, string(r))
		}
	}
}

func (m *Model) playlistRows(w, rows int) []string {
	t := m.theme
	if len(m.playlist) == 0 {
		return m.emptyState(w, rows, fg(t.Muted), "the playlist is empty",
			"⏎ on a track plays its folder · a adds the selection · A plays it, replacing the playlist")
	}
	cur := m.currentIndex()
	numW := len(fmt.Sprint(len(m.playlist)))
	out := make([]string, 0, rows)
	for r := 0; r < rows; r++ {
		i := m.plOffset + r
		if i >= len(m.playlist) {
			break
		}
		tr := m.playlist[i]
		sel := i == m.plCursor
		base := m.rowStyle(sel, i)
		var lb lineBuf
		if sel {
			lb.add(base.Foreground(t.Accent), "▌")
		} else {
			lb.add(base, " ")
		}
		numStyle := base.Foreground(t.Faint)
		nameStyle := base.Foreground(t.Text)
		icon := " "
		switch {
		case i == cur:
			icon = m.nowPlayingGlyph()
			numStyle = base.Foreground(t.Accent)
			nameStyle = base.Foreground(t.Accent).Bold(true)
		case tr.Failed:
			nameStyle = base.Foreground(t.Error).Strikethrough(true)
		}
		lb.add(base.Foreground(t.Accent), " "+icon+" ")
		lb.add(numStyle, fmt.Sprintf("%*d. ", numW, i+1))

		right := ""
		if tr.Length > 0 {
			right = fmtDur(tr.Length)
		}
		name := trackName(tr.Path)
		sub := ""
		if tr.Meta != nil && tr.Meta.Title != "" {
			name = tr.Meta.Title
			sub = tr.Meta.Artist
		}
		avail := w - lb.w - len(right) - 3
		name = truncate(name, avail)
		lb.add(nameStyle, name)
		if sub != "" && avail-ansi.StringWidth(name) > 6 {
			lb.add(base.Foreground(t.Muted), truncate("  "+sub, avail-ansi.StringWidth(name)))
		}
		lb.padTo(base, w-len(right)-2)
		lb.add(base.Foreground(t.Muted), right).padTo(base, w)
		out = append(out, lb.String())
	}
	return out
}

func (m *Model) emptyState(w, rows int, st lipgloss.Style, title, hint string) []string {
	out := make([]string, rows)
	mid := max(0, rows/2-1)
	out[mid] = st.Bold(true).Render(center(title, w))
	if mid+1 < rows {
		out[mid+1] = fg(m.theme.Faint).Render(center(hint, w))
	}
	return out
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}
