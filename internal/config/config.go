// Package config loads the user configuration (TOML) and the small piece of
// state cassetty remembers between runs (JSON).
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
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
	// PaintBackground sets the terminal background to the theme color.
	PaintBackground bool `toml:"paint_background"`
	// SeekStep is the seek distance in seconds for the arrow keys.
	SeekStep int `toml:"seek_step"`
}

// Default returns the built-in configuration.
func Default() Config {
	return Config{
		Volume:          70,
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
// is not an error.
func Load(path string) (Config, error) {
	cfg := Default()
	if _, err := toml.DecodeFile(path, &cfg); err != nil && !errors.Is(err, os.ErrNotExist) {
		return cfg, err
	}
	cfg.MusicDir = ExpandHome(cfg.MusicDir)
	if cfg.SeekStep <= 0 {
		cfg.SeekStep = 5
	}
	cfg.Volume = max(0, min(100, cfg.Volume))
	return cfg, nil
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

# Paint the terminal background with the theme color while running. The light
# silverface theme always paints it, as its dark text needs the light background.
paint_background = true

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

// State is remembered between runs.
type State struct {
	Volume  *int   `json:"volume,omitempty"`
	Theme   string `json:"theme,omitempty"`
	Shuffle bool   `json:"shuffle"`
	Repeat  string `json:"repeat,omitempty"`
	Tape    bool   `json:"tape"`
	LastDir string `json:"last_dir,omitempty"`
}

func statePath() string {
	return filepath.Join(xdgDir("XDG_STATE_HOME", ".local/state"), "cassetty", "state.json")
}

// LoadState reads the remembered state; errors yield an empty state.
func LoadState() State {
	var s State
	if b, err := os.ReadFile(statePath()); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

// SaveState persists s.
func SaveState(s State) error {
	p := statePath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
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
