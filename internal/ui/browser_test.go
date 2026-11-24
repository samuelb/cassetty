package ui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
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

	// A folder below lists it first, with the cursor on it. On it nothing is
	// selected to add or play, and enter goes up.
	press(m, "enter")
	if got := names(m); !slices.Equal(got, []string{"..", "Album", "Empty"}) {
		t.Fatalf("Artist lists %q", got)
	}
	if _, ok := m.selectedEntry(); ok || !m.onParent() {
		t.Fatal("entering should put the cursor on the way up, which selects no entry")
	}
	f := checkFrame(t, m, 100, 30)
	if !strings.Contains(f, "◂ .. ") || !strings.Contains(f, " –/2 ") {
		t.Errorf("the way up or the count leaving it out does not show:\n%s", f)
	}
	press(m, "j")
	if e, ok := m.selectedEntry(); !ok || e.Name != "Album" {
		t.Fatalf("below the way up is %q", e.Name)
	}
	if f := checkFrame(t, m, 100, 30); !strings.Contains(f, " 1/2 ") {
		t.Errorf("the count of entries does not show:\n%s", f)
	}
	press(m, "k")
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

	// A folder left from an entry starts at the top again.
	press(m, "enter")
	press(m, "j")
	press(m, "j") // Empty
	press(m, "backspace")
	press(m, "enter")
	if !m.onParent() {
		e, _ := m.selectedEntry()
		t.Fatalf("back in Artist on %q, want the way up", e.Name)
	}
	press(m, "backspace")

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

func TestFileInfoColumns(t *testing.T) {
	root := t.TempDir()
	files := map[string]int64{"a.mp3": 5, "b.flac": 2000, "c.wav": 12_345_678, "d.ogg": 999_000}
	for name, size := range files {
		p := filepath.Join(root, name)
		touch(t, p, 0)
		if err := os.Truncate(p, size); err != nil {
			t.Fatal(err)
		}
	}
	m := newTestModel(t, root)
	f := checkFrame(t, m, 100, 30)
	extCol, sizeEnd := -1, -1
	for name := range files {
		var row string
		for _, l := range strings.Split(f, "\n") {
			if strings.Contains(l, name) {
				row = l
			}
		}
		if row == "" {
			t.Fatalf("no row for %s in\n%s", name, f)
		}
		at := strings.Index(row, name) + len(name)
		ext := strings.ToUpper(strings.TrimPrefix(filepath.Ext(name), "."))
		i := at + strings.Index(row[at:], " "+ext+" ") + 1
		j := at + strings.Index(row[at:], "B ") + 1
		col, end := ansi.StringWidth(row[:i]), ansi.StringWidth(row[:j])
		if extCol == -1 {
			extCol, sizeEnd = col, end
		}
		if col != extCol || end != sizeEnd {
			t.Errorf("%s: format at %d and size ending at %d, others at %d and %d:\n%s", name, col, end, extCol, sizeEnd, f)
		}
	}
}

func TestHumanSize(t *testing.T) {
	cases := map[int64]string{
		0: "0 B", 999: "999 B", 1000: "1.0 KB", 1536: "1.5 KB",
		1_023_900: "999.9 KB", 1_023_950: "1.0 MB", 1 << 20: "1.0 MB",
		12_345_678: "11.8 MB", 1 << 40: "1.0 TB",
	}
	for n, want := range cases {
		got := humanSize(n)
		if got != want {
			t.Errorf("humanSize(%d) = %q, want %q", n, got, want)
		}
		if len(got) > sizeW {
			t.Errorf("humanSize(%d) = %q is wider than %d", n, got, sizeW)
		}
	}
}

func TestBrowserIcons(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"Artist/Album/01 One.mp3", "Artist/Single.flac"} {
		touch(t, filepath.Join(root, p), 1)
	}
	for _, nerd := range []bool{false, true} {
		m := newTestModel(t, root, func(o *Options) { o.NerdFont = nerd })
		press(m, "enter")
		want := plainIcons
		if nerd {
			want = nerdIcons
		}
		for _, w := range []int{72, 100} {
			f := checkFrame(t, m, w, 30)
			for _, row := range []string{want.parent + " ..", want.folder + " Album/", want.track + " Single.flac"} {
				if !strings.Contains(f, row) {
					t.Errorf("nerd font %v, %d wide: no %q in\n%s", nerd, w, row, f)
				}
			}
		}
	}
}
