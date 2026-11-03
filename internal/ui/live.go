package ui

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/samuelb/cassetty/internal/library"
)

// Keeping the shown folder up to date: the folder watcher reports changes,
// which trigger a debounced reload. Folders that cannot be watched are
// polled instead.

const (
	refreshDelay = 150 * time.Millisecond // lets bursts of changes settle
	settleDelay  = time.Second            // re-check while files are still growing
	pollInterval = 2 * time.Second        // fallback when watching fails
)

type watchMsg struct{}

type refreshMsg struct{ seq int }

type pollMsg struct{ id int }

func (m *Model) waitWatch() tea.Cmd {
	if m.watcher == nil {
		return nil
	}
	c := m.watcher.C
	return func() tea.Msg {
		if _, ok := <-c; !ok {
			return nil
		}
		return watchMsg{}
	}
}

// watchDir points the watcher at the shown folder. When that fails (no
// watcher, unsupported file system) the folder is polled instead.
func (m *Model) watchDir() tea.Cmd {
	if m.watcher != nil && m.watcher.Watch(m.dir) == nil {
		m.polling = false
		return nil
	}
	if m.polling {
		return nil
	}
	m.polling = true
	m.pollID++
	return m.pollTick()
}

func (m *Model) pollTick() tea.Cmd {
	id := m.pollID
	return tea.Tick(pollInterval, func(time.Time) tea.Msg { return pollMsg{id: id} })
}

// scheduleRefresh reloads the shown folder after d, unless a reload is
// already due sooner.
func (m *Model) scheduleRefresh(d time.Duration) tea.Cmd {
	at := time.Now().Add(d)
	if !m.refreshAt.IsZero() && !m.refreshAt.After(at) {
		return nil
	}
	m.refreshAt = at
	m.refreshSeq++
	seq := m.refreshSeq
	return tea.Tick(d, func(time.Time) tea.Msg { return refreshMsg{seq: seq} })
}

// reloadDir re-reads the shown folder, keeping selection and scroll position.
func (m *Model) reloadDir() tea.Cmd {
	dir, formats, hidden := m.dir, m.opts.Formats, m.opts.ShowHidden
	return func() tea.Msg {
		entries, err := library.ReadDir(dir, formats, hidden)
		return dirMsg{dir: dir, entries: entries, err: err, refresh: true}
	}
}

func (m *Model) handleLive(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case watchMsg:
		return tea.Batch(m.waitWatch(), m.scheduleRefresh(refreshDelay))
	case refreshMsg:
		if msg.seq != m.refreshSeq {
			return nil // superseded by an earlier refresh
		}
		m.refreshAt = time.Time{}
		return m.reloadDir()
	case pollMsg:
		if !m.polling || msg.id != m.pollID {
			return nil
		}
		return tea.Batch(m.reloadDir(), m.pollTick())
	}
	return nil
}

// applyRefresh swaps in a fresh listing of the shown folder.
func (m *Model) applyRefresh(msg dirMsg) tea.Cmd {
	if msg.dir != m.dir {
		return nil // the user moved on meanwhile
	}
	if msg.err != nil {
		if !errors.Is(msg.err, fs.ErrNotExist) {
			return nil // keep showing the last listing
		}
		if m.dir == m.root {
			m.entries, m.dirErr = nil, msg.err
			m.refilter()
			return nil
		}
		// The shown folder was removed or renamed: retreat to the closest
		// folder that still exists.
		m.setFlash(filepath.Base(m.dir)+" is gone", true)
		up := filepath.Dir(m.dir)
		for up != m.root && within(m.root, up) && !isDir(up) {
			up = filepath.Dir(up)
		}
		if !within(m.root, up) {
			up = m.root
		}
		return m.loadDir(up, "")
	}

	grew := listingGrew(m.entries, msg.entries)
	sel, hadSel := m.selectedEntry()
	cursor := m.cursor
	m.entries, m.dirErr = msg.entries, nil
	m.refilter()
	m.cursor = min(cursor, max(0, len(m.visible)-1))
	if hadSel {
		for i, idx := range m.visible {
			if m.entries[idx].Name == sel.Name {
				m.cursor = i
				break
			}
		}
	}
	m.ensureVisible()

	cmd := m.watchDir() // re-arm, the path may refer to a new folder now
	if grew {
		// New files may still be being copied; look again until the sizes
		// stop changing.
		cmd = tea.Batch(cmd, m.scheduleRefresh(settleDelay))
	}
	return cmd
}

// listingGrew reports whether cur has entries that are new or changed size
// compared to old.
func listingGrew(old, cur []library.Entry) bool {
	sizes := make(map[string]int64, len(old))
	for _, e := range old {
		sizes[e.Name] = e.Size
	}
	for _, e := range cur {
		if s, ok := sizes[e.Name]; !ok || s != e.Size {
			return true
		}
	}
	return false
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}
