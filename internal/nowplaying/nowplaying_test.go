package nowplaying

import (
	"testing"
	"time"
)

func TestTracker(t *testing.T) {
	t0 := time.Now()
	song := Info{Path: "/music/song.flac", Title: "Song", Artist: "Band", Length: 3 * time.Minute, State: Playing}
	at := func(pos time.Duration, st State) Info {
		i := song
		i.Position, i.State = pos, st
		return i
	}

	var tr tracker
	steps := []struct {
		name          string
		info          Info
		now           time.Duration
		publish, jump bool
	}{
		{"nothing loaded at start", Info{}, 0, false, false},
		{"track loaded", at(0, Playing), 0, true, false},
		{"plays on", at(10*time.Second, Playing), 10 * time.Second, false, false},
		{"slightly late", at(19500*time.Millisecond, Playing), 20 * time.Second, false, false},
		{"seek forward", at(50*time.Second, Playing), 21 * time.Second, true, true},
		{"paused", at(52*time.Second, Paused), 23 * time.Second, true, false},
		{"stays paused", at(52*time.Second, Paused), time.Minute, false, false},
		{"seek while paused", at(90*time.Second, Paused), time.Minute, true, true},
		{"resumed", at(90*time.Second, Playing), 2 * time.Minute, true, false},
		{"volume", func() Info { i := at(100*time.Second, Playing); i.Volume = 0.5; return i }(), 2*time.Minute + 10*time.Second, true, false},
		{"next track", Info{Path: "/other", Title: "Other", State: Playing}, 2 * time.Minute, true, true},
		{"ejected", Info{}, 3 * time.Minute, true, true},
		{"still empty", Info{}, 4 * time.Minute, false, false},
	}
	for _, s := range steps {
		publish, jump := tr.update(s.info, t0.Add(s.now))
		if publish != s.publish || jump != s.jump {
			t.Errorf("%s: publish, jump = %v, %v; want %v, %v", s.name, publish, jump, s.publish, s.jump)
		}
	}
}

func TestText(t *testing.T) {
	for in, want := range map[string]string{
		"Für Elise":      "Für Elise",
		"bad \xff byte":  "bad \uFFFD byte",
		"nul\x00 inside": "nul inside",
	} {
		if got := text(in); got != want {
			t.Errorf("text(%q) = %q, want %q", in, got, want)
		}
	}
}
