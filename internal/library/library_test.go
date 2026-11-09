package library

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestNaturalCompare(t *testing.T) {
	in := []string{"10 - c.mp3", "2 - b.mp3", "1 - a.mp3", "Bonus", "abc", "ABD", "track02", "track1"}
	slices.SortFunc(in, NaturalCompare)
	want := []string{"1 - a.mp3", "2 - b.mp3", "10 - c.mp3", "abc", "ABD", "Bonus", "track1", "track02"}
	if !slices.Equal(in, want) {
		t.Fatalf("got %q\nwant %q", in, want)
	}
}

func TestReadDirAndCollect(t *testing.T) {
	root := t.TempDir()
	mk := func(p string) {
		p = filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("b/10.mp3")
	mk("b/2.flac")
	mk("b/notes.txt")
	mk("a/sub/1.ogg")
	mk("z.mp3")
	mk(".hidden/x.mp3")
	mk(".y.mp3")
	f := NewFormats([]string{"mp3", "flac", "ogg"})

	entries, err := ReadDir(root, f, false)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name)
	}
	if want := []string{"a", "b", "z.mp3"}; !slices.Equal(names, want) {
		t.Fatalf("ReadDir = %q, want %q", names, want)
	}

	paths, err := Collect(root, f, false)
	if err != nil {
		t.Fatal(err)
	}
	for i := range paths {
		paths[i], _ = filepath.Rel(root, paths[i])
	}
	want := []string{"z.mp3", "a/sub/1.ogg", "b/2.flac", "b/10.mp3"}
	if !slices.Equal(paths, want) {
		t.Fatalf("Collect = %q, want %q", paths, want)
	}

	walked, err := Walk(root, f, false)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range walked {
		rel, _ := filepath.Rel(root, e.Path)
		if e.IsDir {
			rel += "/"
		}
		got = append(got, rel)
	}
	want = []string{"z.mp3", "a/", "a/sub/", "a/sub/1.ogg", "b/", "b/2.flac", "b/10.mp3"}
	if !slices.Equal(got, want) {
		t.Fatalf("Walk = %q, want %q", got, want)
	}

	// Symlink loops must not hang the walk.
	if err := os.Symlink(root, filepath.Join(root, "a", "loop")); err == nil {
		if _, err := Collect(root, f, false); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNaturalCompareSortsAccentsAmongTheirLetters(t *testing.T) {
	const (
		aerzteNFD = "Die A\u0308rzte" // decomposed, as programs on macOS store it
		aerzteNFC = "Die \u00c4rzte"  // composed
		aerzte    = "\u00c4rzte"
		bjork     = "Bj\u00f6rk"
		oe        = "\u00f6l"
		ecole     = "\u00c9cole"
	)
	in := []string{"Zappa", aerzte, "Bob", bjork, "Abba", "Die Zukunft", aerzteNFD, "Bjarne", oe, "Die Arzte", ecole, "east", "Olaf", aerzteNFC}
	slices.SortFunc(in, NaturalCompare)
	want := []string{"Abba", aerzte, "Bjarne", bjork, "Bob", "Die Arzte", aerzteNFD, aerzteNFC, "Die Zukunft", "east", ecole, oe, "Olaf", "Zappa"}
	if !slices.Equal(in, want) {
		t.Fatalf("got  %+q\nwant %+q", in, want)
	}
	for _, c := range [][2]string{{"a", "A"}, {aerzteNFD, aerzteNFC}, {"track1", "track01"}, {"x", "x"}} {
		if got, back := NaturalCompare(c[0], c[1]), NaturalCompare(c[1], c[0]); got != -back {
			t.Errorf("NaturalCompare(%+q, %+q) = %d, but %d the other way round", c[0], c[1], got, back)
		}
	}
}

func TestReadDirSortsAccentsAmongTheirLetters(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"Zappa", "\u00c4rzte", "Abba", "Bj\u00f6rk", "Bob", "10 Zehn.mp3", "2 Zw\u00f6lf.mp3", "\u00e4.mp3", "b.mp3"} {
		p := filepath.Join(root, name)
		if filepath.Ext(name) == "" {
			p = filepath.Join(p, "x.mp3")
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := ReadDir(root, NewFormats([]string{"mp3"}), false)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, Compose(e.Name)) // the file system may have decomposed them
	}
	want := []string{"Abba", "\u00c4rzte", "Bj\u00f6rk", "Bob", "Zappa", "2 Zw\u00f6lf.mp3", "10 Zehn.mp3", "\u00e4.mp3", "b.mp3"}
	if !slices.Equal(names, want) {
		t.Fatalf("got  %+q\nwant %+q", names, want)
	}
}
