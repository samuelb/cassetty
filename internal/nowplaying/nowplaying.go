// Package nowplaying connects the player to the system's media controls.
//
// On macOS the media keys, headphone buttons and the Now Playing widget of
// Control Center reach cassetty, and the widget shows the loaded track.
// macOS hands media commands to the app that is "now playing", which is the
// one that most recently published a playing track. A Session therefore only
// claims the keys once Update has published a track.
//
// On Linux and the BSDs a Session is an MPRIS player on the D-Bus session
// bus, which the desktop's media keys and widgets, playerctl and the like
// control. It can also change the volume, shuffle and repeat, and quit.
package nowplaying

import (
	"errors"
	"strings"
	"time"
)

// ErrUnsupported is returned by Start on platforms without media controls.
var ErrUnsupported = errors.New("media controls are not supported on this platform")

// Kind says what a Command asks for.
type Kind int

const (
	TogglePlayPause Kind = iota
	Play
	Pause
	Stop
	Next
	Previous
	Seek       // jump to Command.Position
	SeekBy     // move by Command.Offset
	SetVolume  // set the volume to Command.Volume
	SetShuffle // switch shuffle to Command.Shuffle
	SetRepeat  // switch repeat to Command.Repeat
	Quit
)

// Command is a request from the system's media controls.
type Command struct {
	Kind     Kind
	Position time.Duration // for Seek
	Offset   time.Duration // for SeekBy
	Volume   float64       // for SetVolume, 0 to 1
	Shuffle  bool          // for SetShuffle
	Repeat   Repeat        // for SetRepeat
}

// State is the play state of the loaded track.
type State int

const (
	Stopped State = iota
	Playing
	Paused
)

// Repeat says what plays after the last track.
type Repeat int

const (
	RepeatOff Repeat = iota
	RepeatAll        // the playlist again
	RepeatOne        // the track again
)

// Info describes the loaded track and the player's settings. An empty Path
// means no track is loaded.
type Info struct {
	Path                 string // file of the loaded track
	Title, Artist, Album string
	Length               time.Duration // 0 if unknown
	Position             time.Duration
	State                State

	// The settings apply with or without a track.
	Volume  float64 // 0 to 1; 0 while muted
	Shuffle bool
	Repeat  Repeat
}

// maxDrift is how far the reported position may stray from where the system
// expects it before it is published again. The system advances the position
// of a playing track by itself, so only jumps need publishing.
const maxDrift = time.Second

// tracker filters the updates worth passing on to the system.
type tracker struct {
	last Info
	at   time.Time // when last was published
}

// update reports whether info should be published at time now, and whether
// its position jumped: moved other than playing moves it. If info is to be
// published, update remembers it as published.
func (t *tracker) update(info Info, now time.Time) (publish, jumped bool) {
	last := t.last
	want := last.Position
	if last.State == Playing {
		want += now.Sub(t.at)
	}
	drift := info.Position - want
	jumped = drift >= maxDrift || drift <= -maxDrift
	a, b := info, last
	a.Position, b.Position = 0, 0
	if a == b && !jumped {
		return false, false
	}
	t.last, t.at = info, now
	return true, jumped
}

// text makes s fit for the system's APIs, which take valid UTF-8 without
// NUL bytes. Tags and file names may hold either.
func text(s string) string {
	return strings.ReplaceAll(strings.ToValidUTF8(s, "�"), "\x00", "")
}
