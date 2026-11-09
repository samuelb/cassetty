package ui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParentFolderEntry(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"Artist/Album/01 One.mp3", "Artist/Album/02 Two.mp3", "Single.flac"} {
		touch(t, filepath.Join(root, p), 1)
	}
	os.Mkdir(filepath.Join(root, "Artist", "Empty"), 0o755)
	m := newTestModel(t, root)

	// The root has no way up.
	if got := names(m); !slices.Equal(got, []string{"Artist", "Single.flac"}) {
		t.Fatalf("root lists %q", got)
	}
	if f := checkFrame(t, m, 100, 30); strings.Contains(f, "◂ ..") {
		t.Errorf("the root shows the way up:\n%s", f)
	}

	// A folder below lists it first, with the cursor on the first entry.
	press(m, "enter")
	if got := names(m); !slices.Equal(got, []string{"..", "Album", "Empty"}) {
		t.Fatalf("Artist lists %q", got)
	}
	if e, ok := m.selectedEntry(); !ok || e.Name != "Album" {
		t.Fatalf("entering selects %q", e.Name)
	}
	f := checkFrame(t, m, 100, 30)
	if !strings.Contains(f, "◂ .. ") || !strings.Contains(f, " 1/2 ") {
		t.Errorf("the way up or the count of entries does not show:\n%s", f)
	}

	// On it nothing is selected to add or play, and enter goes up.
	press(m, "k")
	if _, ok := m.selectedEntry(); ok || !m.onParent() {
		t.Fatal("the way up should select no entry")
	}
	if f := checkFrame(t, m, 100, 30); !strings.Contains(f, " –/2 ") {
		t.Errorf("the count does not leave out the way up:\n%s", f)
	}
	press(m, "a")
	if len(m.playlist) != 0 {
		t.Fatalf("a on the way up added %d tracks", len(m.playlist))
	}
	press(m, "enter")
	if m.dir != root {
		t.Fatalf("enter on the way up went to %s", m.dir)
	}
	if e, _ := m.selectedEntry(); e.Name != "Artist" {
		t.Fatalf("back on %q, want the folder it came from", e.Name)
	}

	// A filter matches entries only.
	press(m, "enter")
	press(m, "/")
	press(m, ".")
	if got := names(m); len(got) != 0 {
		t.Fatalf("the filter . matches %q", got)
	}
	press(m, "esc")
	if got := names(m); !slices.Equal(got, []string{"..", "Album", "Empty"}) {
		t.Fatalf("clearing the filter lists %q", got)
	}

	// An empty folder still says so, below the way up.
	run(m, m.loadDir(filepath.Join(root, "Artist", "Empty"), ""))
	if !m.onParent() {
		t.Fatal("in an empty folder the cursor should be on the way up")
	}
	f = checkFrame(t, m, 100, 30)
	if !strings.Contains(f, "◂ ..") || !strings.Contains(f, "no audio files or folders here") {
		t.Errorf("the empty folder does not show:\n%s", f)
	}
	press(m, "/")
	press(m, "enter")
	if !strings.HasSuffix(m.dir, "Empty") {
		t.Fatalf("enter on an empty filter left the folder for %s", m.dir)
	}
	press(m, "enter")
	if m.dir != filepath.Join(root, "Artist") {
		t.Fatalf("enter in the empty folder went to %s", m.dir)
	}
}
