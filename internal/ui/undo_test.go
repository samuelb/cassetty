package ui

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// listed returns the names of the tracks of the playlist.
func listed(m *Model) []string {
	var names []string
	for _, t := range m.playlist {
		names = append(names, trackName(t.Path))
	}
	return names
}

func TestUndoBringsThePlaylistBack(t *testing.T) {
	root := tonesNamed(t, "A/01.wav", "A/02.wav", "A/03.wav", "A/04.wav", "B/x.wav")
	m := newTestModel(t, root)
	press(m, "u")
	if !strings.Contains(m.flash, "nothing to undo") {
		t.Errorf("with nothing to undo the footer says %q", m.flash)
	}

	press(m, "enter") // into A
	press(m, "enter") // plays the album
	album := []string{"01", "02", "03", "04"}
	if !slices.Equal(listed(m), album) {
		t.Fatalf("playlist %q", listed(m))
	}
	playing := m.cur

	check := func(what string, want []string) {
		t.Helper()
		if got := listed(m); !slices.Equal(got, want) {
			t.Fatalf("%s: playlist %q, want %q", what, got, want)
		}
		if m.cur != playing || m.state != statePlaying {
			t.Fatalf("%s: the track in the deck did not stay", what)
		}
	}

	press(m, "tab")
	press(m, "c")
	check("cleared", nil)
	if !strings.Contains(m.flash, "u brings it back") {
		t.Errorf("clearing the playlist says %q", m.flash)
	}
	press(m, "u")
	check("clearing undone", album)
	if m.currentIndex() != 0 || !strings.Contains(m.flash, "undone: playlist cleared") {
		t.Errorf("after undoing: track %d of the playlist is loaded, footer %q", m.currentIndex()+1, m.flash)
	}
	press(m, "u")
	check("clearing redone", nil)
	if !strings.Contains(m.flash, "redone: playlist cleared") {
		t.Errorf("after redoing the footer says %q", m.flash)
	}
	press(m, "u")
	check("clearing undone again", album)

	press(m, "down")
	press(m, "d")
	check("a track removed", []string{"01", "03", "04"})
	press(m, "u")
	check("removal undone", album)
	if m.plCursor != 1 {
		t.Errorf("the cursor is on track %d, want it back on the second", m.plCursor+1)
	}

	press(m, "J")
	check("a track moved", []string{"01", "03", "02", "04"})
	press(m, "u")
	check("move undone", album)

	// Playing another folder replaces the playlist; from the library too,
	// u brings the old one back, and what plays stays in the deck.
	press(m, "tab")
	press(m, "backspace")
	press(m, "down") // B
	press(m, "A")
	playing = m.cur
	check("replaced", []string{"x"})
	press(m, "u")
	check("replacement undone", album)
	if m.currentIndex() != -1 {
		t.Error("the playing track is in the playlist it was not part of")
	}

	press(m, "a") // adds B to the album
	check("added", []string{"01", "02", "03", "04", "x"})
	press(m, "u")
	check("addition undone", album)
}

func TestUndoneTracksGetTheirLengths(t *testing.T) {
	m := newTestModel(t, toneLibrary(t, 3, time.Second))
	m.remember("playlist cleared")
	m.before.tracks = tracksFor(tonePaths(m.root, 3)) // tracks nothing was read of
	press(m, "u")
	for i, tr := range m.playlist {
		if tr.Length != time.Second || tr.Meta == nil {
			t.Errorf("track %d of the playlist brought back: length %v, tags %v", i+1, tr.Length, tr.Meta)
		}
	}
}
