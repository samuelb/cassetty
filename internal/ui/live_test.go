package ui

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/samuelb/cassetty/internal/library"
)

func names(m *Model) []string {
	var out []string
	for _, i := range m.visible {
		out = append(out, m.entries[i].Name)
	}
	return out
}

func touch(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestShownFolderUpdatesLive(t *testing.T) {
	root := t.TempDir()
	touch(t, filepath.Join(root, "Album", "01.mp3"), 1)
	touch(t, filepath.Join(root, "m.mp3"), 1)
	m := newTestModel(t, root)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if m.watcher == nil {
		t.Skip("no folder watcher on this platform")
	}
	press(m, "down") // select m.mp3
	if got := names(m); !slices.Equal(got, []string{"Album", "m.mp3"}) {
		t.Fatalf("initial listing %q", got)
	}

	// A new file and folder appear without any key press: the watcher
	// notification leads to a debounced reload.
	touch(t, filepath.Join(root, "a.mp3"), 10)
	os.Mkdir(filepath.Join(root, "New Album"), 0o755)
	msg := make(chan tea.Msg, 1)
	go func() { msg <- m.waitWatch()() }()
	select {
	case got := <-msg:
		if _, ok := got.(watchMsg); !ok {
			t.Fatalf("expected a watch message, got %T", got)
		}
		_, cmd := m.Update(got)
		run(m, cmd)
	case <-time.After(2 * time.Second):
		t.Fatal("no notification from the folder watcher")
	}
	if got := names(m); !slices.Equal(got, []string{"Album", "New Album", "a.mp3", "m.mp3"}) {
		t.Fatalf("after creating entries: %q", got)
	}
	if e, _ := m.selectedEntry(); e.Name != "m.mp3" {
		t.Fatalf("selection moved to %q", e.Name)
	}
	if m.refreshAt.IsZero() {
		t.Fatal("new entries should schedule a settle re-check")
	}

	// Removing the selected file keeps the cursor in place.
	os.Remove(filepath.Join(root, "m.mp3"))
	run(m, m.reloadDir())
	if got := names(m); !slices.Equal(got, []string{"Album", "New Album", "a.mp3"}) {
		t.Fatalf("after removing: %q", got)
	}
	if e, _ := m.selectedEntry(); e.Name != "a.mp3" {
		t.Fatalf("selection after removal: %q", e.Name)
	}

	// An active filter stays active and applies to new entries.
	m.filter = "song"
	m.refilter()
	touch(t, filepath.Join(root, "new song.mp3"), 1)
	run(m, m.reloadDir())
	if got := names(m); !slices.Equal(got, []string{"new song.mp3"}) {
		t.Fatalf("filtered listing: %q", got)
	}
	m.filter = ""
	m.refilter()

	// When the shown folder disappears the browser retreats to its parent.
	run(m, m.loadDir(filepath.Join(root, "Album"), ""))
	if m.dir != filepath.Join(root, "Album") {
		t.Fatalf("not in Album: %s", m.dir)
	}
	os.RemoveAll(filepath.Join(root, "Album"))
	run(m, m.reloadDir())
	if m.dir != root {
		t.Fatalf("expected to retreat to the root, in %s", m.dir)
	}
	if slices.Contains(names(m), "Album") {
		t.Fatal("removed folder still listed")
	}
}

func TestListingGrew(t *testing.T) {
	e := func(name string, size int64) library.Entry { return library.Entry{Name: name, Size: size} }
	old := []library.Entry{e("a.mp3", 10), e("b.mp3", 20)}
	cases := []struct {
		cur  []library.Entry
		want bool
	}{
		{[]library.Entry{e("a.mp3", 10), e("b.mp3", 20)}, false},
		{[]library.Entry{e("a.mp3", 10)}, false},                               // removal settles at once
		{[]library.Entry{e("a.mp3", 10), e("b.mp3", 25)}, true},                // still being copied
		{[]library.Entry{e("a.mp3", 10), e("b.mp3", 20), e("c.mp3", 0)}, true}, // new entry
	}
	for i, c := range cases {
		if got := listingGrew(old, c.cur); got != c.want {
			t.Errorf("case %d: got %v, want %v", i, got, c.want)
		}
	}
}
