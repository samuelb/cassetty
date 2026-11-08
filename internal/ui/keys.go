package ui

import (
	"slices"

	"charm.land/bubbles/v2/key"
	"github.com/charmbracelet/x/ansi"
)

type keyMap struct {
	PlayPause, Stop, Prev, Next                key.Binding
	SeekBack, SeekFwd, SeekBackFar, SeekFwdFar key.Binding
	VolUp, VolDown, Mute                       key.Binding
	Shuffle, Repeat, Eject, Theme, Remaining   key.Binding
	Tape                                       key.Binding

	Up, Down, PageUp, PageDown, Top, Bottom key.Binding
	Open, Back, Root, Enqueue, PlayAll      key.Binding
	Filter, Locate, Tab                     key.Binding
	Remove, Clear, MoveUp, MoveDown         key.Binding

	Help, Quit key.Binding
}

func newKeyMap() keyMap {
	b := func(keys []string, k, desc string) key.Binding {
		return key.NewBinding(key.WithKeys(keys...), key.WithHelp(k, desc))
	}
	return keyMap{
		PlayPause: b([]string{"space", "p"}, "space", "play/pause"),
		Stop:      b([]string{"s", "v"}, "s/v", "stop"),
		Prev:      b([]string{"<", "z"}, "</z", "previous"),
		Next:      b([]string{">", "b", "n"}, ">/b/n", "next"),

		SeekBack:    b([]string{"left"}, "←", "rewind"),
		SeekFwd:     b([]string{"right"}, "→", "fast forward"),
		SeekBackFar: b([]string{"shift+left", "["}, "⇧← [", "rewind more"),
		SeekFwdFar:  b([]string{"shift+right", "]"}, "⇧→ ]", "forward more"),

		VolUp:   b([]string{"+", "="}, "+", "volume up"),
		VolDown: b([]string{"-", "_"}, "-", "volume down"),
		Mute:    b([]string{"m"}, "m", "mute"),

		Shuffle:   b([]string{"S"}, "S", "shuffle"),
		Repeat:    b([]string{"r"}, "r", "repeat mode"),
		Eject:     b([]string{"e"}, "e", "eject"),
		Theme:     b([]string{"t"}, "t", "next theme"),
		Remaining: b([]string{"i"}, "i", "elapsed/remaining"),
		Tape:      b([]string{"T"}, "T", "tape sound"),

		Up:       b([]string{"up", "k"}, "↑/k", "up"),
		Down:     b([]string{"down", "j"}, "↓/j", "down"),
		PageUp:   b([]string{"pgup", "ctrl+b"}, "pgup", "page up"),
		PageDown: b([]string{"pgdown", "ctrl+f"}, "pgdn", "page down"),
		Top:      b([]string{"home", "g"}, "g", "top"),
		Bottom:   b([]string{"end", "G"}, "G", "bottom"),

		Open:    b([]string{"enter", "l"}, "⏎/l", "open · play"),
		Back:    b([]string{"backspace", "h"}, "⌫/h", "parent folder"),
		Root:    b([]string{"~"}, "~", "library root"),
		Enqueue: b([]string{"a"}, "a", "add to playlist"),
		PlayAll: b([]string{"A"}, "A", "play, replacing playlist"),
		Filter:  b([]string{"/"}, "/", "filter"),
		Locate:  b([]string{"f"}, "f", "find playing track"),
		Tab:     b([]string{"tab", "shift+tab"}, "tab", "library/playlist"),

		Remove:   b([]string{"d", "x", "delete"}, "d/x", "remove"),
		Clear:    b([]string{"c"}, "c", "clear playlist"),
		MoveUp:   b([]string{"K", "shift+up"}, "K", "move up"),
		MoveDown: b([]string{"J", "shift+down"}, "J", "move down"),

		Help: b([]string{"?"}, "?", "help"),
		Quit: b([]string{"q", "ctrl+c"}, "q", "quit"),
	}
}

// footerHint is a key hint in the footer. A lower rank is more important:
// when space is short the highest ranked hints are dropped first, while rank 0
// hints always show.
type footerHint struct {
	key  key.Binding
	rank int
}

// shortHelp lists the footer hints in display order.
func (m *Model) shortHelp() []footerHint {
	k := m.keys
	if m.tab == tabPlaylist {
		return []footerHint{
			{k.PlayPause, 1}, {k.Open, 2}, {k.SeekFwd, 7}, {k.Next, 5}, {k.Remove, 4},
			{k.MoveDown, 6}, {k.Tab, 3}, {k.Theme, 8}, {k.Help, 0}, {k.Quit, 0},
		}
	}
	return []footerHint{
		{k.PlayPause, 1}, {k.Open, 2}, {k.Back, 6}, {k.Enqueue, 4}, {k.SeekFwd, 8}, {k.Next, 5},
		{k.VolUp, 9}, {k.Filter, 7}, {k.Tab, 3}, {k.Theme, 10}, {k.Help, 0}, {k.Quit, 0},
	}
}

// shortHelpLine renders the footer hints in at most w cells, leaving out the
// least important ones that do not fit.
func (m *Model) shortHelpLine(w int) string {
	hints := m.shortHelp()
	keys := func() []key.Binding {
		ks := make([]key.Binding, len(hints))
		for i, h := range hints {
			ks[i] = h.key
		}
		return ks
	}
	m.help.SetWidth(0)
	for ansi.StringWidth(m.help.ShortHelpView(keys())) > w {
		i := 0
		for j, h := range hints {
			if h.rank > hints[i].rank {
				i = j
			}
		}
		if hints[i].rank == 0 {
			break
		}
		hints = slices.Delete(hints, i, i+1)
	}
	m.help.SetWidth(w)
	return m.help.ShortHelpView(keys())
}
