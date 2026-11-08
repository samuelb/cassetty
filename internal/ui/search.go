package ui

import (
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/samuelb/cassetty/internal/library"
)

// Searching the whole library: the library list then picks from every
// folder and track below the root instead of the shown folder's entries,
// matching the query against their paths relative to the root. The shown
// folder stays loaded underneath, and the list returns to it when the
// search ends.

// librarySearch is the state of the search of the whole library.
type librarySearch struct {
	on bool // the library list shows the search

	index    []library.Entry // every folder and track of the library
	keys     []string        // the lowercased path of each, relative to the root
	ready    bool            // index has been read
	scanning bool
	err      error // of the last read, while none succeeded

	// where the list of the shown folder was
	sel    string
	offset int
}

type scanMsg struct {
	entries []library.Entry
	keys    []string
	err     error
}

// shown returns the entries the library list picks from: the shown
// folder's, or the whole library's while searching.
func (m *Model) shown() []library.Entry {
	if m.search.on {
		return m.search.index
	}
	return m.entries
}

// startSearch turns the library list into a search of the whole library
// for the query typed so far, and reads the library afresh.
func (m *Model) startSearch() tea.Cmd {
	m.filtering = true
	if !m.search.on {
		if e, ok := m.selectedEntry(); ok {
			m.search.sel = e.Name
		} else {
			m.search.sel = ""
		}
		m.search.offset = m.offset
		m.search.on = true
		m.cursor, m.offset = 0, 0
		m.refilter()
	}
	return m.scanLibrary()
}

// endSearch returns the library list to the shown folder, filtered by
// filter.
func (m *Model) endSearch(filter string) {
	if !m.search.on {
		return
	}
	m.search.on = false
	m.filter = filter
	m.refilter()
	m.cursor = 0
	for i, idx := range m.visible {
		if m.entries[idx].Name == m.search.sel {
			m.cursor = i
			break
		}
	}
	m.offset = m.search.offset
	m.ensureVisible()
}

// scanLibrary reads every folder and track of the library, unless that is
// under way already. Each search reads it anew, so it finds what was added
// since the last one; until then it searches what was read before.
func (m *Model) scanLibrary() tea.Cmd {
	if m.search.scanning {
		return nil
	}
	m.search.scanning = true
	root, formats, hidden := m.root, m.opts.Formats, m.opts.ShowHidden
	return func() tea.Msg {
		entries, err := library.Walk(root, formats, hidden)
		keys := make([]string, len(entries))
		for i, e := range entries {
			rel, _ := filepath.Rel(root, e.Path)
			keys[i] = strings.ToLower(rel)
		}
		return scanMsg{entries: entries, keys: keys, err: err}
	}
}

func (m *Model) applyScan(msg scanMsg) tea.Cmd {
	s := &m.search
	s.scanning = false
	if msg.err != nil {
		if !s.ready {
			s.err = msg.err
		}
		if s.on {
			m.setFlash("cannot read the library: "+msg.err.Error(), true)
		}
		return nil
	}
	sel, hadSel := m.selectedEntry()
	s.index, s.keys, s.ready, s.err = msg.entries, msg.keys, true, nil
	if !s.on {
		return nil
	}
	m.refilter()
	if hadSel {
		for i, idx := range m.visible {
			if s.index[idx].Path == sel.Path {
				m.cursor = i
				break
			}
		}
	}
	m.ensureVisible()
	return nil
}

// folderOf names the folder of e relative to the root, like the
// breadcrumb does; "" for the root itself.
func (m *Model) folderOf(e library.Entry) string {
	rel, err := filepath.Rel(m.root, filepath.Dir(e.Path))
	if err != nil || rel == "." {
		return ""
	}
	return strings.ReplaceAll(rel, string(filepath.Separator), " › ")
}
