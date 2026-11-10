package ui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/samuelb/cassetty/internal/viz"
)

func TestDisplaySlowsDownWhileUnfocused(t *testing.T) {
	m := newTestModel(t, toneLibrary(t, 2, 5*time.Second))
	if !m.View().ReportFocus {
		t.Fatal("the terminal is not asked to tell of the focus")
	}
	if got := m.tickEvery(); got != idleTick {
		t.Fatalf("an idle deck draws every %v", got)
	}
	press(m, "enter")
	if m.state != statePlaying || m.tickEvery() != frameTick {
		t.Fatalf("a playing deck draws every %v (state %v)", m.tickEvery(), m.state)
	}

	_, _ = m.Update(tea.BlurMsg{})
	if got := m.tickEvery(); got != unfocusedTick {
		t.Fatalf("unfocused, the playing deck draws every %v, want %v", got, unfocusedTick)
	}
	tick := m.tickID
	_, cmd := m.Update(tea.FocusMsg{})
	if m.tickEvery() != frameTick {
		t.Fatalf("focused again, it draws every %v", m.tickEvery())
	}
	if cmd == nil || m.tickID == tick {
		t.Fatal("getting the focus back does not start drawing at full speed at once")
	}

	// A paused or idle deck draws as rarely as it does anyway.
	press(m, "space")
	for m.engine.Speed() > 0 { // the tape winds down first
		time.Sleep(10 * time.Millisecond)
	}
	m.spin = 0
	m.vu, m.spectrum.Levels, m.spectrum.Peaks = viz.VU{}, nil, nil
	_, _ = m.Update(tea.BlurMsg{})
	if got := m.tickEvery(); got != pausedTick {
		t.Fatalf("unfocused and paused, the deck draws every %v, want %v", got, pausedTick)
	}
}

func TestDisplayKeepsItsPaceWhenAskedTo(t *testing.T) {
	m := newTestModel(t, toneLibrary(t, 2, 5*time.Second), func(o *Options) { o.AnimateUnfocused = true })
	if m.View().ReportFocus {
		t.Error("the terminal is asked to tell of a focus that makes no difference")
	}
	press(m, "enter")
	_, _ = m.Update(tea.BlurMsg{})
	if got := m.tickEvery(); got != frameTick {
		t.Fatalf("unfocused, the playing deck draws every %v, want %v", got, frameTick)
	}
}
