package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// Names in characters that take two cells each, and one of emoji.
const (
	japanese = "\u65e5\u672c\u8a9e\u306e\u3068\u3066\u3082\u9577\u3044\u66f2\u540d\u304c\u3053\u3053\u306b\u5165\u308a\u307e\u3059\u3088\u3068\u3066\u3082\u9577\u3044\u66f2\u540d"
	korean   = "\ud55c\uad6d\uc5b4 \ub178\ub798 \uc81c\ubaa9\uc774 \uc5ec\uae30\uc5d0 \ub4e4\uc5b4\uac11\ub2c8\ub2e4 \uc544\uc8fc \uae34 \uc81c\ubaa9"
	emoji    = "\U0001f3b5\U0001f3b6 emoji title \U0001f3a7\U0001f3a4\U0001f3b8\U0001f941\U0001f3b9\U0001f3ba\U0001f3bb and more \U0001f3bc\U0001f3b7"
)

// tonesNamed returns a library of tone files with the given relative paths.
func tonesNamed(t *testing.T, names ...string) string {
	t.Helper()
	tone, err := os.ReadFile(filepath.Join(toneLibrary(t, 1, 2*time.Second), "01.wav"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for _, name := range names {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, tone, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestFramesFitWideCharacters(t *testing.T) {
	for _, name := range []string{japanese, korean, emoji} {
		m := newTestModel(t, tonesNamed(t, name+"/"+name+".wav"))
		press(m, "enter") // into the folder
		press(m, "enter") // plays the track
		if m.src == nil {
			t.Fatalf("%+q is not loaded: %s", name, m.flash)
		}
		for _, tab := range []tab{tabLibrary, tabPlaylist} {
			m.tab = tab
			for _, size := range [][2]int{{72, 22}, {80, 24}, {100, 30}, {120, 40}, {160, 50}} {
				for step := 0.0; step < 12; step++ {
					m.marquee = step
					checkFrame(t, m, size[0], size[1])
				}
			}
		}
	}
}

func TestScroll(t *testing.T) {
	for _, c := range []struct {
		s      string
		off, w int
		want   string
	}{
		{"abcdef", 0, 3, "abc"},
		{"abcdef", 4, 4, "efab"},
		{"abcdef", 6, 2, "ab"},
		{"abc", 1, 8, "bcabcabc"},
		{"\u65e5\u672c\u8a9ex", 0, 4, "\u65e5\u672c"},
		{"\u65e5\u672c\u8a9ex", 1, 4, " \u672c "}, // both edges cut a character in half
		{"\u65e5\u672c\u8a9ex", 2, 5, "\u672c\u8a9ex"},
		{"\u65e5\u672c\u8a9ex", 5, 4, " x\u65e5"},
		{"A\u0308rzte", 0, 3, "A\u0308rz"}, // an accent takes no cell
		{"", 0, 5, ""},
		{"abc", 0, 0, ""},
	} {
		if got := scroll(c.s, c.off, c.w); got != c.want {
			t.Errorf("scroll(%+q, %d, %d) = %+q, want %+q", c.s, c.off, c.w, got, c.want)
		}
	}
	// However far it has run, it fills its cells.
	for _, s := range []string{japanese, korean, emoji, "plain  ***  "} {
		for off := 0; off < 3*ansi.StringWidth(s); off++ {
			for _, w := range []int{1, 2, 7, 30} {
				if got := ansi.StringWidth(scroll(s, off, w)); got != w {
					t.Fatalf("scroll(%+q, %d, %d) is %d cells wide", s, off, w, got)
				}
			}
		}
	}
}
