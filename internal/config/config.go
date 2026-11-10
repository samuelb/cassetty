// Package config loads the user configuration (TOML) and the small piece of
// state cassetty remembers between runs (JSON).
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/samuelb/cassetty/internal/library"
)

// Config is the user editable configuration file.
type Config struct {
	// MusicDir is the root folder of the media library. When empty,
	// DefaultMusicDir is used.
	MusicDir string `toml:"music_dir"`
	// Theme, when set, is used at every start instead of the theme
	// remembered from the last run: one of "classic", "amber", "vfd",
	// "synthwave", "sunbleached", "silverface", "cyberdeck". Without either,
	// cassetty starts in "sunbleached".
	Theme string `toml:"theme"`
	// Volume is the initial volume in percent (used until one is remembered).
	Volume int `toml:"volume"`
	// ShowHidden lists dot files and dot folders.
	ShowHidden bool `toml:"show_hidden"`
	// NerdFont draws the icons of the browser from a Nerd Font; without it
	// the browser uses symbols that every font has.
	NerdFont bool `toml:"nerd_font"`
	// PaintBackground sets the terminal background to the theme color.
	PaintBackground bool `toml:"paint_background"`
	// AnimateUnfocused draws the deck in full motion also while the
	// terminal is not focused. Without it the display slows down to four
	// frames a second then, which takes far less of the processor.
	AnimateUnfocused bool `toml:"animate_unfocused"`
	// SeekStep is the seek distance in seconds for the arrow keys.
	SeekStep int `toml:"seek_step"`
}

// Default returns the built-in configuration.
func Default() Config {
	return Config{
		Volume:          70,
		NerdFont:        true,
		PaintBackground: true,
		SeekStep:        5,
	}
}

// Path returns the config file location: $CASSETTY_CONFIG, or
// $XDG_CONFIG_HOME/cassetty/config.toml, or ~/.config/cassetty/config.toml.
func Path() string {
	if p := os.Getenv("CASSETTY_CONFIG"); p != "" {
		return ExpandHome(p)
	}
	return filepath.Join(xdgDir("XDG_CONFIG_HOME", ".config"), "cassetty", "config.toml")
}

// Load reads the config file at path on top of the defaults. A missing file
// is not an error; a key that is none of the config's is one, as a
// misspelled setting would otherwise silently not apply.
func Load(path string) (Config, error) {
	cfg := Default()
	md, err := toml.DecodeFile(path, &cfg)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return cfg, err
	}
	if unknown := md.Undecoded(); len(unknown) > 0 {
		return cfg, unknownKey(unknown[0].String())
	}
	cfg.MusicDir = ExpandHome(cfg.MusicDir)
	if cfg.SeekStep <= 0 {
		cfg.SeekStep = 5
	}
	cfg.Volume = max(0, min(100, cfg.Volume))
	return cfg, nil
}

// Keys returns the keys of the config file, in the order of the sample.
func Keys() []string {
	t := reflect.TypeFor[Config]()
	keys := make([]string, t.NumField())
	for i := range keys {
		keys[i] = t.Field(i).Tag.Get("toml")
	}
	return keys
}

// unknownKey is the error for a key the config does not have. It names the
// key that was most likely meant, or else all of them.
func unknownKey(key string) error {
	keys := Keys()
	best, dist := "", len(key)
	for _, k := range keys {
		if d := editDistance(key, k); d < dist {
			best, dist = k, d
		}
	}
	if dist <= max(2, len(key)/4) {
		return fmt.Errorf("unknown key %q: did you mean %s?", key, best)
	}
	return fmt.Errorf("unknown key %q (the keys are %s)", key, strings.Join(keys, ", "))
}

// editDistance counts the letters to add, drop or change to turn a into b.
func editDistance(a, b string) int {
	row := make([]int, len(b)+1)
	for j := range row {
		row[j] = j
	}
	for i := 1; i <= len(a); i++ {
		diag := row[0]
		row[0] = i
		for j := 1; j <= len(b); j++ {
			above := row[j]
			if a[i-1] != b[j-1] {
				diag++
			}
			row[j] = min(diag, above+1, row[j-1]+1)
			diag = above
		}
	}
	return row[len(b)]
}

// Sample is a commented default config file.
const Sample = `# cassetty configuration

# Root folder of your music library. The browser never leaves this folder.
# Unset, cassetty opens ~/Music if it exists, otherwise the current folder.
# music_dir = "~/Music"

# Color theme: "sunbleached" (faded rainbow, the default), "classic" (Winamp green),
# "amber", "vfd" (cyan display), "synthwave", "silverface" (silver hi-fi) or
# "cyberdeck" (neon).
# Press t inside cassetty to cycle through them; cassetty remembers the last one
# unless a theme is set here, which then applies at every start.
# theme = "sunbleached"

# Initial volume in percent. cassetty remembers the last volume you used.
volume = 70

# List dot files and dot folders.
show_hidden = false

# Draw the folder and track icons of the browser from a Nerd Font
# (https://www.nerdfonts.com). If they show as empty boxes or question marks,
# your terminal has no Nerd Font: set this to false for symbols every font has.
nerd_font = true

# Paint the terminal background with the theme color while running. The light
# silverface theme always paints it, as its dark text needs the light background.
paint_background = true

# Draw the deck in full motion also while the terminal is not focused: in a
# tab or a window behind another, or next to the one you work in. Unfocused,
# the display otherwise slows down to four frames a second, which takes far
# less of the processor and of the battery. Set this to true if you watch
# the deck from another window. It makes no difference in a terminal that
# does not tell programs of the focus.
animate_unfocused = false

# Seek distance in seconds for the left/right arrow keys (shift = 6x).
seek_step = 5
`

// WriteSample writes the sample config to path unless a file exists there.
func WriteSample(path string) error {
	if _, err := os.Stat(path); err == nil {
		return os.ErrExist
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(Sample), 0o644)
}

// State is remembered between runs: the settings, which every library
// shares, and where the library in use was left.
type State struct {
	Volume  *int   `json:"volume,omitempty"`
	Theme   string `json:"theme,omitempty"`
	Shuffle bool   `json:"shuffle"`
	Repeat  string `json:"repeat,omitempty"`
	Tape    bool   `json:"tape"`

	Session
}

// Session is where a library was left: the folder shown, and playback.
type Session struct {
	LastDir  string   `json:"last_dir,omitempty"`
	Playback Playback `json:"playback,omitzero"`
}

// Playback is where playback was when cassetty quit, so that the next run
// picks up from there.
type Playback struct {
	Playlist []string `json:"playlist,omitempty"`
	// Order is the shuffled order of the playlist, as indexes into it.
	Order []int `json:"order,omitempty"`
	// Track is the loaded track and Index its place in the playlist. For a
	// track removed from the playlist, Index is where it used to be, or -1.
	Track string `json:"track,omitempty"`
	Index int    `json:"index,omitempty"`
	// Position is how far into Track playback was, in seconds.
	Position float64 `json:"position,omitempty"`
}

// stateFile is the state as it is stored. The session at its top level is
// from before every library had its own: it is that of whichever library
// cassetty ran on last then, and stays until that library is saved again.
type stateFile struct {
	State
	Libraries map[string]Session `json:"libraries,omitempty"`
}

func statePath() string {
	return filepath.Join(xdgDir("XDG_STATE_HOME", ".local/state"), "cassetty", "state.json")
}

// readState reads the state file; errors yield an empty one.
func readState() stateFile {
	var f stateFile
	if b, err := os.ReadFile(statePath()); err == nil {
		_ = json.Unmarshal(b, &f)
	}
	return f
}

// LoadState reads the remembered state, with the session of the library at
// root; errors yield an empty state. A library without a session of its own
// gets the one from before every library had one: what of it lies outside
// the library is for the caller to leave out.
func LoadState(root string) State {
	f := readState()
	s := f.State
	if session, ok := f.Libraries[filepath.Clean(root)]; ok {
		s.Session = session
	}
	return s
}

// SaveState persists s, with its session as that of the library at root.
// The sessions of the other libraries stay as they are.
func SaveState(root string, s State) error {
	root = filepath.Clean(root)
	f := readState()
	old := f.Session
	f.State = s
	f.Session = Session{}
	if !old.within(root) {
		f.Session = old // of another library, which has not been saved since
	}
	if f.Libraries == nil {
		f.Libraries = map[string]Session{}
	}
	f.Libraries[root] = s.Session
	if s.Session.empty(root) {
		delete(f.Libraries, root)
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(statePath(), b)
}

// empty reports whether the session holds nothing worth keeping for the
// library at root.
func (s Session) empty(root string) bool {
	return (s.LastDir == "" || filepath.Clean(s.LastDir) == root) &&
		len(s.Playback.Playlist) == 0 && s.Playback.Track == ""
}

// within reports whether anything of the session lies in the library at
// root, which makes it a session of that library.
func (s Session) within(root string) bool {
	in := func(path string) bool { return path != "" && library.Within(root, path) }
	return in(s.LastDir) || in(s.Playback.Track) || slices.ContainsFunc(s.Playback.Playlist, in)
}

// writeFile puts data in the file at path in one step: it is written to a
// file next to it, which then takes its place. Whatever happens on the way,
// a full disk or the end of the process, the file at path is the old one or
// the new one, never the start of one.
func writeFile(path string, data []byte) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(f.Name())
		}
	}()
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Chmod(0o644); err != nil {
		return err
	}
	// On the disk before it takes the place of the old file: a rename may
	// outlive a power cut that the data does not.
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// DefaultMusicDir is the library root used when neither the command line nor
// the config names one: ~/Music if it exists, otherwise the current folder.
func DefaultMusicDir() string {
	if dir := ExpandHome("~/Music"); isDir(dir) {
		return dir
	}
	return "."
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// ExpandHome replaces a leading ~ with the home directory.
func ExpandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}

func xdgDir(env, fallback string) string {
	if d := os.Getenv(env); d != "" && filepath.IsAbs(d) {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fallback
	}
	return filepath.Join(home, fallback)
}
