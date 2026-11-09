package ui

import (
	"sync"
	"testing"
	"time"

	"github.com/samuelb/cassetty/internal/config"
)

// saves collects what the model saves while it runs.
type saves struct {
	mu     sync.Mutex
	states []config.State
}

func (s *saves) save(st config.State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states = append(s.states, st)
	return nil
}

func (s *saves) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.states)
}

func (s *saves) last(t *testing.T) config.State {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.states) == 0 {
		t.Fatal("nothing was saved")
	}
	return s.states[len(s.states)-1]
}

func TestStateIsSavedWhileRunning(t *testing.T) {
	delay := saveDelay
	saveDelay = time.Millisecond
	t.Cleanup(func() { saveDelay = delay })

	var kept saves
	root := toneLibrary(t, 3, 5*time.Second)
	m := newTestModel(t, root, func(o *Options) { o.Save = kept.save })
	if kept.count() != 0 {
		t.Fatalf("%d saves before anything changed", kept.count())
	}

	press(m, "+")
	if got := kept.last(t); got.Volume == nil || *got.Volume != 75 {
		t.Fatalf("the volume was not saved: %+v", got.Volume)
	}
	n := kept.count()
	press(m, "down") // moving the cursor is nothing to remember
	press(m, "?")
	press(m, "?")
	if kept.count() != n {
		t.Fatalf("%d saves for nothing worth saving", kept.count()-n)
	}

	press(m, "enter") // plays the folder
	pb := kept.last(t).Playback
	if len(pb.Playlist) != 3 || pb.Track != m.cur.Path {
		t.Fatalf("the playlist and the track were not saved: %+v", pb)
	}

	n = kept.count()
	press(m, "tab")
	press(m, "d") // removes a track
	if kept.count() == n || len(kept.last(t).Playback.Playlist) != 2 {
		t.Fatalf("the change to the playlist was not saved: %+v", kept.last(t).Playback)
	}

	n = kept.count()
	press(m, "space") // pausing keeps the position
	if kept.count() == n {
		t.Fatal("pausing did not save the position")
	}

	// While a track plays its position is saved once a minute.
	press(m, "space")
	n = kept.count()
	m.pos = 61 * time.Second
	run(m, m.keepState())
	if kept.count() == n {
		t.Fatal("the position was not saved after a minute of playing")
	}
	n = kept.count()
	m.pos = 62 * time.Second
	run(m, m.keepState())
	if kept.count() != n {
		t.Fatal("the position was saved again within the same minute")
	}
}

func TestNothingIsSavedWithoutASaver(t *testing.T) {
	m := newTestModel(t, toneLibrary(t, 1, time.Second))
	press(m, "+")
	if m.saveDue || m.saving {
		t.Fatal("a save is scheduled with nowhere to save to")
	}
}
