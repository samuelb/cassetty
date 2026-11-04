package ui

import (
	"math"

	tea "charm.land/bubbletea/v2"

	"github.com/samuelb/cassetty/internal/nowplaying"
)

// The system's media controls: media keys, headphone buttons, the Now
// Playing widget on macOS and MPRIS clients elsewhere send commands, and get
// told what plays.

type remoteMsg nowplaying.Command

// remoteRepeat maps the repeat modes to the media controls' ones.
var remoteRepeat = map[repeatMode]nowplaying.Repeat{
	repeatOff: nowplaying.RepeatOff,
	repeatAll: nowplaying.RepeatAll,
	repeatOne: nowplaying.RepeatOne,
}

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
	case nowplaying.SeekBy:
		return m.seekBy(c.Offset)
	case nowplaying.SetVolume:
		m.setVolume(int(math.Round(c.Volume * 100)))
	case nowplaying.SetShuffle:
		if c.Shuffle != m.shuffle {
			m.setShuffle(c.Shuffle)
		}
	case nowplaying.SetRepeat:
		for mode, r := range remoteRepeat {
			if r == c.Repeat && mode != m.repeat {
				m.setRepeat(mode)
			}
		}
	case nowplaying.Quit:
		return tea.Quit
	}
	return nil
}

// syncNowPlaying tells the system what is playing. It runs after every
// message; the session passes on only real changes.
func (m *Model) syncNowPlaying() {
	if m.remote == nil {
		return
	}
	info := nowplaying.Info{
		Volume:  float64(m.volume) / 100,
		Shuffle: m.shuffle,
		Repeat:  remoteRepeat[m.repeat],
	}
	if m.muted {
		info.Volume = 0
	}
	if m.src != nil && m.cur != nil {
		info.Path = m.cur.Path
		info.Title = m.trackTitle()
		info.Artist, info.Album = m.trackArtistAlbum()
		info.Length = m.src.Length
		info.Position = m.pos
		switch m.state {
		case statePlaying:
			info.State = nowplaying.Playing
		case statePaused:
			info.State = nowplaying.Paused
		}
	}
	m.remote.Update(info)
}
