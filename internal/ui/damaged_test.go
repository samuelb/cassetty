package ui

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// breakOff tells the model that the loaded track broke off while playing,
// as the deck does for a damaged file.
func breakOff(m *Model) {
	_, cmd := m.Update(audioEventMsg{Gen: m.gen, Err: errors.New("damaged file (index out of range)")})
	run(m, cmd)
}

func TestABrokenTrackIsMarkedAndTheNextOnePlays(t *testing.T) {
	m := newTestModel(t, toneLibrary(t, 3, 5*time.Second))
	press(m, "enter")
	first := m.cur
	breakOff(m)
	if !first.Failed {
		t.Error("the track that broke off is not marked")
	}
	if !m.flashErr || !strings.Contains(m.flash, "01: damaged file") {
		t.Errorf("flash %q does not name the track and what happened", m.flash)
	}
	if m.cur != m.playlist[1] || m.state != statePlaying {
		t.Fatalf("the second track is not playing: state %v", m.state)
	}
}

func TestABrokenTrackIsNotRepeated(t *testing.T) {
	m := newTestModel(t, toneLibrary(t, 3, 5*time.Second))
	press(m, "enter")
	press(m, "r")
	press(m, "r")
	if m.repeat != repeatOne {
		t.Fatalf("repeat %v", m.repeat)
	}
	first := m.cur
	breakOff(m)
	if m.cur != first || m.state != stateStopped || !first.Failed {
		t.Fatalf("state %v, failed %v: the broken track should stop the deck", m.state, first.Failed)
	}
}

func TestPlaybackStopsWhenEveryTrackBreaks(t *testing.T) {
	m := newTestModel(t, toneLibrary(t, 2, 5*time.Second))
	press(m, "enter")
	press(m, "r") // repeat all: it would go round for good
	breakOff(m)
	if m.cur != m.playlist[1] || m.state != statePlaying {
		t.Fatalf("the second track is not playing: state %v", m.state)
	}
	breakOff(m)
	if m.state != stateStopped {
		t.Fatalf("state %v after every track broke off, want the deck stopped", m.state)
	}
	// A track that plays to its end clears the count.
	press(m, "enter")
	_, cmd := m.Update(audioEventMsg{Gen: m.gen})
	run(m, cmd)
	if m.broken != 0 {
		t.Errorf("%d broken tracks counted after one played to its end", m.broken)
	}
}
