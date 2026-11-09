package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// The state is saved when cassetty quits, which it does not always get to:
// it may crash, be killed, or lose power. So that such an end costs little
// of the session, the state is also saved a moment after it changed, and
// once a minute while a track plays, for the position in it.

// saveDelay is how long a change waits to be saved, for those that follow
// it at once to be saved with it.
var saveDelay = 2 * time.Second

// stateMark holds what of the model is remembered between runs, as far as
// it takes to tell that there is something new to save. Unlike the state
// itself it is cheap to make and to compare, which happens after every
// message.
type stateMark struct {
	volume, theme int
	shuffle, tape bool
	repeat        repeatMode
	dir           string
	edits         int // changes to the playlist
	cur           *Track
	state         playState
	minute        int // of the play head
}

func (m *Model) mark() stateMark {
	return stateMark{
		volume: m.volume, theme: m.themeIdx,
		shuffle: m.shuffle, tape: m.tape, repeat: m.repeat,
		dir:    m.dir,
		edits:  m.edits,
		cur:    m.cur,
		state:  m.state,
		minute: int(m.pos / time.Minute),
	}
}

type saveMsg struct{}

type savedMsg struct{ mark stateMark }

// keepState schedules a save when the state is no longer as it was saved.
// It runs after every message.
func (m *Model) keepState() tea.Cmd {
	if m.opts.Save == nil || m.saveDue || m.saving || m.mark() == m.stored {
		return nil
	}
	m.saveDue = true
	return tea.Tick(saveDelay, func(time.Time) tea.Msg { return saveMsg{} })
}

// saveState saves the state as it is now, off the goroutine that draws. A
// save that fails is not tried again until the state changes once more, and
// goes unreported like one that fails on quitting.
func (m *Model) saveState() tea.Cmd {
	m.saveDue = false
	if m.opts.Save == nil {
		return nil
	}
	m.saving = true
	save, state, mark := m.opts.Save, m.State(), m.mark()
	return func() tea.Msg {
		_ = save(state)
		return savedMsg{mark: mark}
	}
}
