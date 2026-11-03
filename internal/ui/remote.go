package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/samuelb/cassetty/internal/nowplaying"
)

// The system's media controls: media keys, headphone buttons and the Now
// Playing widget send commands, and the widget shows the loaded track.

type remoteMsg nowplaying.Command

func (m *Model) waitRemote() tea.Cmd {
	if m.remote == nil {
		return nil
	}
	c := m.remote.C
	return func() tea.Msg { return remoteMsg(<-c) }
}

func (m *Model) handleRemote(c nowplaying.Command) tea.Cmd {
	switch c.Kind {
	case nowplaying.TogglePlayPause:
		return m.togglePlay()
	case nowplaying.Play:
		if m.state != statePlaying {
			return m.togglePlay()
		}
	case nowplaying.Pause:
		if m.state == statePlaying {
			return m.setState(statePaused)
		}
	case nowplaying.Stop:
		return m.stop()
	case nowplaying.Next:
		return m.next()
	case nowplaying.Previous:
		return m.prev()
	case nowplaying.Seek:
		m.seekTo(c.Position)
		return m.kickTick()
	}
	return nil
}

// syncNowPlaying tells the system what is playing. It runs after every
// message; the session passes on only real changes.
func (m *Model) syncNowPlaying() {
	if m.remote == nil {
		return
	}
	if m.src == nil {
		m.remote.Update(nowplaying.Info{})
		return
	}
	artist, album := m.trackArtistAlbum()
	info := nowplaying.Info{
		Title:    m.trackTitle(),
		Artist:   artist,
		Album:    album,
		Length:   m.src.Length,
		Position: m.pos,
	}
	switch m.state {
	case statePlaying:
		info.State = nowplaying.Playing
	case statePaused:
		info.State = nowplaying.Paused
	}
	m.remote.Update(info)
}
