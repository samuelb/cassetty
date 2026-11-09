package ui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/samuelb/cassetty/internal/library"
)

func searchLibrary(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, p := range []string{
		"Miles Davis/Kind of Blue/01 So What.mp3",
		"Miles Davis/Kind of Blue/02 Freddie Freeloader.mp3",
		"Pink Floyd/Animals/01 Dogs.mp3",
		"Pink Floyd/The Wall/01 In the Flesh.mp3",
		"Pink Floyd/The Wall/02 The Thin Ice.mp3",
		"Single.flac",
	} {
		p = filepath.Join(root, p)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("x"), 0o644)
	}
	return root
}

func typeText(m *Model, s string) {
	for _, r := range s {
		press(m, string(r))
	}
}

// results returns the paths of the listed entries relative to the root,
// folders with a trailing slash.
func results(m *Model) []string {
	var out []string
	for _, i := range m.visible {
		e := m.shown()[i]
		rel, _ := filepath.Rel(m.root, e.Path)
		if e.IsDir {
			rel += "/"
		}
		out = append(out, rel)
	}
	return out
}

func TestSearchTheWholeLibrary(t *testing.T) {
	root := searchLibrary(t)
	m := newTestModel(t, root)
	press(m, "enter") // into Miles Davis
	press(m, "down")  // Kind of Blue
	if !strings.HasSuffix(m.dir, "Miles Davis") {
		t.Fatalf("in %s", m.dir)
	}

	press(m, "F")
	if !m.search.on || !m.filtering || !m.search.ready {
		t.Fatalf("F should search the read library: on %v, typing %v, ready %v", m.search.on, m.filtering, m.search.ready)
	}
	if len(m.visible) != 0 {
		t.Fatalf("lists %q before anything is typed", results(m))
	}
	typeText(m, "floyd wall")
	want := []string{"Pink Floyd/The Wall/", "Pink Floyd/The Wall/01 In the Flesh.mp3", "Pink Floyd/The Wall/02 The Thin Ice.mp3"}
	if got := results(m); !slices.Equal(got, want) {
		t.Fatalf("found %q, want %q", got, want)
	}
	for _, w := range []int{72, 100, 160} {
		f := checkFrame(t, m, w, 30)
		if !strings.Contains(f, "searching") || !strings.Contains(f, "search: floyd wall") {
			t.Errorf("width %d: the search does not show", w)
		}
		if w >= 100 && !strings.Contains(f, "01 In the Flesh.mp3  Pink Floyd › The Wall") {
			t.Errorf("width %d: the result does not name its folder", w)
		}
	}

	// Enter accepts the query; enter on a track plays its folder from it.
	press(m, "enter")
	press(m, "down")
	press(m, "enter")
	if len(m.playlist) != 2 || filepath.Base(m.playlist[m.plCursor].Path) != "01 In the Flesh.mp3" {
		t.Fatalf("playlist of %d tracks from %d", len(m.playlist), m.plCursor)
	}
	if !m.search.on {
		t.Fatal("playing a track ended the search")
	}

	// Tab takes the query to the folder, esc clears it there.
	press(m, "/")
	press(m, "tab")
	if m.search.on || m.filter != "floyd wall" || len(m.visible) != 0 {
		t.Fatalf("tab should filter the folder by the query: on %v, filter %q", m.search.on, m.filter)
	}
	press(m, "tab")
	if !m.search.on || len(m.visible) != 3 {
		t.Fatalf("tab should search the library again: on %v, %q", m.search.on, results(m))
	}
	press(m, "esc")
	if m.search.on || m.filter != "" || m.filtering {
		t.Fatalf("esc should end the search: on %v, filter %q", m.search.on, m.filter)
	}
	if e, _ := m.selectedEntry(); e.Name != "Kind of Blue" {
		t.Fatalf("back on %q, want the entry selected before", e.Name)
	}

	// Opening a folder ends the search there. A folder matches with what
	// is in it.
	press(m, "F")
	typeText(m, "animals")
	if got := results(m); !slices.Equal(got, []string{"Pink Floyd/Animals/", "Pink Floyd/Animals/01 Dogs.mp3"}) {
		t.Fatalf("found %q", got)
	}
	press(m, "enter")
	press(m, "enter")
	if m.search.on || !strings.HasSuffix(m.dir, filepath.Join("Pink Floyd", "Animals")) {
		t.Fatalf("in %s, searching %v", m.dir, m.search.on)
	}

	// Each search reads the library afresh.
	os.WriteFile(filepath.Join(m.dir, "02 Pigs.mp3"), []byte("x"), 0o644)
	press(m, "F")
	typeText(m, "pigs")
	if got := results(m); !slices.Equal(got, []string{"Pink Floyd/Animals/02 Pigs.mp3"}) {
		t.Fatalf("found %q", got)
	}
	// Enter on the only match plays it, from the read library.
	press(m, "enter")
	if len(m.playlist) != 2 || filepath.Base(m.playlist[m.plCursor].Path) != "02 Pigs.mp3" {
		t.Fatalf("playlist of %d tracks from %d", len(m.playlist), m.plCursor)
	}
	press(m, "backspace")
	if m.search.on {
		t.Fatal("backspace should end the search")
	}
}

func TestSearchFromThePlaylist(t *testing.T) {
	m := newTestModel(t, searchLibrary(t))
	press(m, "tab")
	press(m, "F")
	if m.tab != tabLibrary || !m.search.on {
		t.Fatalf("F in the playlist should search the library: tab %v, on %v", m.tab, m.search.on)
	}
	typeText(m, "single")
	if got := results(m); !slices.Equal(got, []string{"Single.flac"}) {
		t.Fatalf("found %q", got)
	}
	// Results at the root name no folder.
	if f := checkFrame(t, m, 100, 30); !strings.Contains(f, "Single.flac ") {
		t.Errorf("frame lacks the result:\n%s", f)
	}
}

func TestSearchKeepsTheFolderUpToDate(t *testing.T) {
	m := newTestModel(t, searchLibrary(t))
	press(m, "F")
	typeText(m, "so what")
	before := results(m)

	// The shown folder changes meanwhile.
	entries := append(slices.Clone(m.entries), library.Entry{Name: "New.mp3", Path: filepath.Join(m.root, "New.mp3")})
	m.Update(dirMsg{dir: m.dir, entries: entries, refresh: true})
	if got := results(m); !slices.Equal(got, before) {
		t.Fatalf("a refresh of the folder changed the search to %q", got)
	}
	press(m, "esc")
	if len(m.visible) != len(entries) {
		t.Fatalf("the folder lists %d entries, want %d", len(m.visible), len(entries))
	}
}

func TestFilterAndSearchFindAccentedNames(t *testing.T) {
	root := t.TempDir()
	const (
		aerzte = "Die A\u0308rzte" // decomposed, as programs on macOS store it
		bjork  = "Bj\u00f6rk"      // composed, as a keyboard types it
	)
	for _, p := range []string{aerzte + "/Geschwisterliebe.mp3", bjork + "/J\u00f3ga.mp3", "Zappa/Muffin Man.mp3"} {
		p = filepath.Join(root, p)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("x"), 0o644)
	}
	m := newTestModel(t, root)
	for query, want := range map[string]string{
		"\u00e4rzte":   aerzte,
		"a\u0308rzte":  aerzte,
		"arzte":        aerzte,
		"\u00c4RZTE":   aerzte,
		"die \u00e4rz": aerzte,
		"bj\u00f6rk":   bjork,
		"bjo\u0308rk":  bjork,
		"bjork":        bjork,
		"ZAPPA":        "Zappa",
		"z\u00e4ppa":   "Zappa",
	} {
		press(m, "/")
		typeText(m, query)
		if got := results(m); !slices.Equal(got, []string{want + "/"}) {
			t.Errorf("the filter %+q lists %+q, want %+q", query, got, want+"/")
		}
		press(m, "esc")
	}

	press(m, "F")
	typeText(m, "joga")
	if got, want := results(m), []string{bjork + "/J\u00f3ga.mp3"}; !slices.Equal(got, want) {
		t.Errorf("the search lists %+q, want %+q", got, want)
	}
	press(m, "esc")

	// The match is marked in the name, with every accent on its letter.
	m.filter = "arz"
	var lb lineBuf
	m.highlight(&lb, aerzte, fg(m.theme.Text), fg(m.theme.Text))
	if got := ansi.Strip(lb.String()); got != "Die \u00c4rzte" || lb.w != 9 {
		t.Errorf("highlighting made the name %+q, %d cells wide", got, lb.w)
	}
	if strings.Count(lb.String(), "\x1b[4;") != 3 {
		t.Errorf("the three letters of the match are not underlined: %+q", lb.String())
	}
}
