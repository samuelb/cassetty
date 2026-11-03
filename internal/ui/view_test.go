package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone/v2"

	"github.com/samuelb/cassetty/internal/audio"
	"github.com/samuelb/cassetty/internal/library"
)

func testRoot(t *testing.T) string {
	if r := os.Getenv("CASSETTY_TEST_MUSIC"); r != "" {
		return r
	}
	root := t.TempDir()
	for _, p := range []string{"Artist/Album/01 One.mp3", "Artist/Album/02 Two.mp3", "Single.flac"} {
		os.MkdirAll(filepath.Join(root, filepath.Dir(p)), 0o755)
		os.WriteFile(filepath.Join(root, p), []byte("x"), 0o644)
	}
	return root
}

func newTestModel(t *testing.T, root string) *Model {
	t.Helper()
	zone.NewGlobal()
	e, _ := audio.NewEngine(true)
	t.Cleanup(e.Close)
	m := New(Options{
		Root: root, Theme: "classic", Volume: 70, SeekStep: 5 * time.Second,
		Formats: library.NewFormats(audio.SupportedFormats()), Engine: e,
	})
	t.Cleanup(m.Close)
	run(m, m.loadDir(m.dir, ""))
	return m
}

// run executes cmd (and nested batches) synchronously, feeding results back
// into the model. Commands that block (like the audio event wait) are
// abandoned after a short timeout; animation ticks are dropped.
func run(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-ch:
	case <-time.After(300 * time.Millisecond):
		return
	}
	switch msg := msg.(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			run(m, c)
		}
	case tickMsg, nil:
	default:
		_, next := m.Update(msg)
		run(m, next)
	}
}

func press(m *Model, k string) {
	var msg tea.KeyPressMsg
	switch k {
	case "enter":
		msg = tea.KeyPressMsg{Code: tea.KeyEnter}
	case "space":
		msg = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "down":
		msg = tea.KeyPressMsg{Code: tea.KeyDown}
	case "backspace":
		msg = tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "tab":
		msg = tea.KeyPressMsg{Code: tea.KeyTab}
	default:
		msg = tea.KeyPressMsg{Code: rune(k[0]), Text: k}
	}
	_, cmd := m.Update(msg)
	run(m, cmd)
}

func checkFrame(t *testing.T, m *Model, w, h int) string {
	t.Helper()
	_, _ = m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	content := m.View().Content
	lines := strings.Split(content, "\n")
	if len(lines) != h {
		t.Errorf("%dx%d (%s): %d lines", w, h, m.theme.Name, len(lines))
	}
	for i, l := range lines {
		if got := ansi.StringWidth(l); got != w {
			t.Errorf("%dx%d (%s): line %d is %d wide: %q", w, h, m.theme.Name, i, got, ansi.Strip(l))
		}
	}
	return ansi.Strip(content)
}

func TestFrameSizes(t *testing.T) {
	m := newTestModel(t, testRoot(t))
	sizes := [][2]int{{72, 22}, {80, 24}, {100, 30}, {120, 40}, {160, 50}, {230, 70}}
	for ti := range themes {
		m.setTheme(ti)
		for _, s := range sizes {
			checkFrame(t, m, s[0], s[1])
		}
	}
	m.showHelp = true
	for _, s := range sizes {
		if f := checkFrame(t, m, s[0], s[1]); !strings.Contains(f, "PLAYBACK") {
			t.Errorf("%dx%d: help screen missing", s[0], s[1])
		}
	}
	m.showHelp = false
	if !strings.Contains(checkFrame(t, m, 60, 15), "enlarge the terminal") {
		t.Error("expected the too-small notice")
	}

	// Load a track (enter on the first file found) and check again.
	press(m, "down")
	press(m, "down")
	for _, s := range sizes {
		checkFrame(t, m, s[0], s[1])
	}
}

func TestFooterDropsHintsByImportance(t *testing.T) {
	m := newTestModel(t, testRoot(t))
	for _, tb := range []tab{tabLibrary, tabPlaylist} {
		m.tab = tb
		for _, w := range []int{72, 80, 100, 120, 140, 160, 200} {
			lines := strings.Split(checkFrame(t, m, w, 30), "\n")
			footer := lines[len(lines)-1]
			var kept, dropped []footerHint
			for _, h := range m.shortHelp() {
				if strings.Contains(footer, h.key.Help().Key+" "+h.key.Help().Desc) {
					kept = append(kept, h)
				} else {
					dropped = append(dropped, h)
				}
			}
			for _, d := range dropped {
				if d.rank == 0 {
					t.Errorf("tab %d, width %d: footer %q lacks %q", tb, w, footer, d.key.Help().Desc)
				}
				for _, k := range kept {
					if k.rank > d.rank {
						t.Errorf("tab %d, width %d: footer %q keeps %q over %q", tb, w, footer, k.key.Help().Desc, d.key.Help().Desc)
					}
				}
			}
			if len(dropped) > 0 && strings.Contains(footer, "t next theme") {
				t.Errorf("tab %d, width %d: footer %q drops a hint but keeps the theme", tb, w, footer)
			}
		}
	}
	m.tab = tabLibrary
	lines := strings.Split(checkFrame(t, m, 200, 30), "\n")
	if footer := lines[len(lines)-1]; !strings.Contains(footer, "t next theme") {
		t.Errorf("wide footer %q lacks the theme hint", footer)
	}
}

func TestNavigationAndPlayback(t *testing.T) {
	root := testRoot(t)
	m := newTestModel(t, root)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// Enter the first folder, keep entering until a file is selected.
	for i := 0; i < 5; i++ {
		e, ok := m.selectedEntry()
		if !ok || !e.IsDir {
			break
		}
		press(m, "enter")
	}
	e, ok := m.selectedEntry()
	if !ok || e.IsDir {
		t.Fatalf("expected a file, got %+v", e)
	}
	press(m, "enter")
	if len(m.playlist) == 0 {
		t.Fatal("enter on a file should fill the playlist with its folder")
	}
	if os.Getenv("CASSETTY_TEST_MUSIC") != "" {
		if m.src == nil || m.state != statePlaying {
			t.Fatalf("expected the track to play, state=%v src=%v flash=%q", m.state, m.src, m.flash)
		}
		press(m, "space")
		if m.state != statePaused {
			t.Fatalf("space should pause, state=%v", m.state)
		}
		press(m, "b")
		if m.currentIndex() != 1 && len(m.playlist) > 1 {
			t.Fatalf("next should load track 2, at %d", m.currentIndex())
		}
		if testing.Verbose() {
			m.animate(0.04)
			t.Log("\n" + checkFrame(t, m, 100, 30))
		}
	}

	press(m, "backspace")
	if m.dir == root && len(m.entries) == 0 {
		t.Fatal("back navigation broke")
	}
	press(m, "tab")
	if m.tab != tabPlaylist {
		t.Fatal("tab should switch to the playlist")
	}
}
