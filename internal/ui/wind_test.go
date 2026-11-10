package ui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// await waits for cond to hold, which the deck brings about as it plays.
func await(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); !cond(); time.Sleep(2 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("waited in vain for %s", what)
		}
	}
}

func TestTheArrowKeysWindTheTape(t *testing.T) {
	m := newTestModel(t, toneLibrary(t, 1, 30*time.Second))
	press(m, "enter")
	await(t, "the track to play", func() bool { return m.engine.Position() > 0 })

	began, from := time.Now(), m.engine.Position()
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if !m.engine.Winding() || m.tickEvery() != frameTick {
		t.Fatalf("the tape does not wind fast forward (drawn every %v)", m.tickEvery())
	}
	await(t, "the tape to get up to speed", func() bool { return m.engine.Speed() > 5 })
	if !m.latched("btn:ff") || m.latched("btn:rew") || !m.latched("btn:play") {
		t.Error("winding forward, the keys FF and PLAY should be down and REW up")
	}
	await(t, "the wind to end", func() bool { return !m.engine.Winding() })
	if m.latched("btn:ff") || m.engine.Speed() != 1 {
		t.Errorf("after the wind FF is down or the tape at a speed of %v", m.engine.Speed())
	}
	// The tape wound 5 s on, and played on from there since.
	got, most := m.engine.Position()-from, 5*time.Second+time.Since(began)+50*time.Millisecond
	if got < 5*time.Second || got > most {
		t.Errorf("the head moved by %v, want 5s to %v", got, most)
	}

	from = m.engine.Position()
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModShift})
	await(t, "the tape to rewind", func() bool { return m.engine.Speed() < -5 })
	if !m.latched("btn:rew") || m.latched("btn:ff") {
		t.Error("rewinding, the key REW should be down and FF up")
	}
	await(t, "the rewind to end", func() bool { return !m.engine.Winding() })
	if got := m.engine.Position(); got > time.Second {
		t.Errorf("the head is at %v after rewinding 30s from %v", got, from)
	}

	// A paused deck winds too, and stays paused where it gets to.
	press(m, "space")
	await(t, "the deck to pause", func() bool { return m.engine.Speed() == 0 })
	from = m.engine.Position()
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	await(t, "the paused deck to wind", func() bool { return m.engine.Speed() > 5 })
	await(t, "the wind to end", func() bool { return !m.engine.Winding() })
	if got := m.engine.Position() - from; got < 4990*time.Millisecond || got > 5010*time.Millisecond {
		t.Errorf("the paused deck wound by %v", got)
	}
	if m.state != statePaused || m.engine.Speed() != 0 {
		t.Errorf("state %v at a speed of %v after winding a paused deck", m.state, m.engine.Speed())
	}
}
