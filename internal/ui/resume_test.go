package ui

import (
	"fmt"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/samuelb/cassetty/internal/config"
)

// tonePaths returns the paths of the n tracks of a toneLibrary.
func tonePaths(root string, n int) []string {
	var out []string
	for i := range n {
		out = append(out, filepath.Join(root, fmt.Sprintf("%02d.wav", i+1)))
	}
	return out
}

func resumeFrom(pb config.Playback) func(*Options) {
	return func(o *Options) { o.Resume = pb }
}

func TestPicksUpWhereItLeftOff(t *testing.T) {
	root := toneLibrary(t, 3, 5*time.Second)
	paths := tonePaths(root, 3)
	m := newTestModel(t, root, resumeFrom(config.Playback{
		Playlist: paths, Track: paths[1], Index: 1, Position: 2.5,
	}))
	if len(m.playlist) != 3 || m.plCursor != 1 {
		t.Fatalf("playlist of %d tracks with the cursor at %d", len(m.playlist), m.plCursor)
	}
	run(m, m.resume())
	if m.cur != m.playlist[1] || m.src == nil || m.state != statePaused {
		t.Fatalf("second track not loaded paused: cur %v, state %v, flash %q", m.cur, m.state, m.flash)
	}
	if p := m.engine.Position(); p < 2450*time.Millisecond || p > 2550*time.Millisecond {
		t.Fatalf("play head at %v, want 2.5s", p)
	}
	if m.playlist[0].Meta == nil || m.playlist[2].Meta == nil {
		t.Error("tags of the playlist not read")
	}

	press(m, "space")
	if m.state != statePlaying {
		t.Fatalf("space should play on, state %v", m.state)
	}
	time.Sleep(100 * time.Millisecond)
	pb := m.State().Playback
	if !slices.Equal(pb.Playlist, paths) || pb.Track != paths[1] || pb.Index != 1 || pb.Position <= 2.5 || pb.Position > 3.5 {
		t.Fatalf("remembers %+v", pb)
	}
	if pb.Order != nil {
		t.Errorf("remembers a shuffled order without shuffle: %v", pb.Order)
	}

	press(m, "e")
	if pb := m.State().Playback; pb.Track != "" || len(pb.Playlist) != 3 {
		t.Fatalf("after ejecting remembers %+v", pb)
	}
}

func TestRestoreKeepsToTheLibrary(t *testing.T) {
	root := toneLibrary(t, 3, time.Second)
	paths := tonePaths(root, 3)
	outside := filepath.Join(t.TempDir(), "elsewhere.wav")
	m := newTestModel(t, root, resumeFrom(config.Playback{
		Playlist: []string{outside, paths[0], "relative.wav", paths[1], paths[2]},
		Order:    []int{4, 3, 2, 1, 0},
		Track:    paths[1], Index: 3, Position: 0.5,
	}))
	if len(m.playlist) != 3 || m.playlist[1].Path != paths[1] {
		t.Fatalf("restored %d tracks", len(m.playlist))
	}
	if m.order != nil {
		t.Errorf("kept the order %v of a playlist that lost tracks", m.order)
	}
	if m.resuming != m.playlist[1] || m.curHint != 1 || m.plCursor != 1 {
		t.Fatalf("resumes %v at hint %d", m.resuming, m.curHint)
	}
	// Quitting before the track opened remembers the same.
	pb := m.State().Playback
	if !slices.Equal(pb.Playlist, paths) || pb.Track != paths[1] || pb.Index != 1 || pb.Position != 0.5 {
		t.Fatalf("remembers %+v", pb)
	}

	// A track outside the library is not loaded.
	m = newTestModel(t, root, resumeFrom(config.Playback{Playlist: paths, Track: outside, Index: 0}))
	if m.resuming != nil {
		t.Fatalf("resumes %v from outside the library", m.resuming)
	}
}

func TestPicksUpATrackRemovedFromThePlaylist(t *testing.T) {
	root := toneLibrary(t, 3, time.Second)
	paths := tonePaths(root, 3)
	m := newTestModel(t, root, resumeFrom(config.Playback{
		Playlist: []string{paths[0], paths[2]}, Track: paths[1], Index: 1,
	}))
	run(m, m.resume())
	if m.cur == nil || m.cur.Path != paths[1] || m.currentIndex() != -1 || m.state != stateStopped {
		t.Fatalf("loaded %v at %d, state %v", m.cur, m.currentIndex(), m.state)
	}
	if i, ok := m.nextIndex(); !ok || i != 1 {
		t.Fatalf("next is %d, want the track after where it was", i)
	}
	if pb := m.State().Playback; pb.Track != paths[1] || pb.Index != 1 {
		t.Fatalf("remembers %+v", pb)
	}
}

func TestPicksUpTheShuffledOrder(t *testing.T) {
	root := toneLibrary(t, 4, time.Second)
	paths := tonePaths(root, 4)
	order := []int{2, 0, 3, 1}
	m := newTestModel(t, root, func(o *Options) {
		o.Shuffle = true
		o.Resume = config.Playback{Playlist: paths, Order: order, Track: paths[0], Index: 0}
	})
	run(m, m.resume())
	if i, ok := m.nextIndex(); !ok || i != 3 {
		t.Fatalf("next is %d, want 3 from the order %v", i, m.order)
	}
	if pb := m.State().Playback; !slices.Equal(pb.Order, order) {
		t.Fatalf("remembers the order %v, want %v", pb.Order, order)
	}

	// An order that is not one of the playlist is drawn anew.
	m = newTestModel(t, root, func(o *Options) {
		o.Shuffle = true
		o.Resume = config.Playback{Playlist: paths, Order: []int{0, 1, 1, 2}}
	})
	if m.order != nil {
		t.Fatalf("kept the order %v", m.order)
	}
}
