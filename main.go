// Command cassetty is a terminal music player styled after a cassette deck
// and the classic Winamp.
package main

import (
	"cmp"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/samuelb/cassetty/internal/audio"
	"github.com/samuelb/cassetty/internal/config"
	"github.com/samuelb/cassetty/internal/library"
	"github.com/samuelb/cassetty/internal/nowplaying"
	"github.com/samuelb/cassetty/internal/ui"
)

// version is set by the release build, see scripts/release-build.sh.
var version = "dev"

// buildVersion returns the version to tell: the one the release build set,
// or else the one Go recorded for the module, which is how a cassetty built
// by go install knows what it is.
func buildVersion(set string, info *debug.BuildInfo) string {
	if set == "dev" && info != nil && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return set
}

func main() {
	var err error
	// On macOS the main thread delivers the media keys meanwhile.
	nowplaying.Main(func() { err = run() })
	if err != nil {
		fmt.Fprintln(os.Stderr, "cassetty:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		cfgPath    = flag.String("config", config.Path(), "path of the config file")
		dirFlag    = flag.String("dir", "", "music folder (overrides music_dir from the config)")
		themeFlag  = flag.String("theme", "", "color theme: "+strings.Join(ui.ThemeNames(), ", "))
		silent     = flag.Bool("silent", false, "do not open the audio device (decode and visualise only)")
		initConfig = flag.Bool("init-config", false, "write a commented default config file and exit")
		showVer    = flag.Bool("version", false, "print the version and exit")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `cassetty — a cassette deck music player for the terminal

usage: cassetty [flags] [music folder | track]

flags:
`)
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nconfig file: %s\nformats:     %s\n", config.Path(), strings.Join(audio.SupportedFormats(), " "))
	}
	flag.Parse()

	if *showVer {
		info, _ := debug.ReadBuildInfo()
		fmt.Println("cassetty", buildVersion(version, info))
		return nil
	}
	if *initConfig {
		if err := config.WriteSample(*cfgPath); err != nil {
			if errors.Is(err, os.ErrExist) {
				return fmt.Errorf("%s already exists", *cfgPath)
			}
			return err
		}
		fmt.Println("wrote", *cfgPath)
		return nil
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return fmt.Errorf("config %s: %w", *cfgPath, err)
	}

	formats := library.NewFormats(audio.SupportedFormats())
	root, play, err := musicFolder(cfg.MusicDir, *dirFlag, flag.Arg(0), formats)
	if err != nil {
		return err
	}
	if err := ui.CheckRoot(root); err != nil {
		return fmt.Errorf("%w\n  set music_dir in %s (cassetty -init-config creates one)\n  or pass a folder: cassetty ~/Music", err, *cfgPath)
	}
	// The settings, and where this music folder was left.
	state := config.LoadState(root)

	if err := checkTheme(*themeFlag); err != nil {
		return err
	}
	if err := checkTheme(cfg.Theme); err != nil {
		return fmt.Errorf("config %s: %w", *cfgPath, err)
	}
	// -theme beats the config file, which beats the theme remembered from
	// the last run.
	theme := cmp.Or(*themeFlag, cfg.Theme, state.Theme, ui.DefaultTheme)
	volume := cfg.Volume
	if state.Volume != nil {
		volume = *state.Volume
	}

	// There is an engine either way: without a device it runs silent.
	engine, initErr := audio.NewEngine(*silent)
	notice := ""
	if initErr != nil {
		notice = "no audio device (" + initErr.Error() + ") — running silent"
	}
	defer engine.Close()
	// Media keys and the Now Playing widget, where the system has them.
	remote, err := nowplaying.Start()
	if err == nil {
		defer remote.Close()
	}

	// The state is saved on quitting, and while cassetty runs so that little
	// is lost should it not get to quit. In silent mode nothing is saved.
	var keep *keeper
	var save func(config.State) error
	if !*silent {
		keep = &keeper{root: root}
		save = keep.save
	}

	m, err := ui.Run(ui.Options{
		Root:            root,
		StartDir:        state.LastDir,
		Play:            play,
		Theme:           theme,
		Volume:          volume,
		ShowHidden:      cfg.ShowHidden,
		NerdFont:        cfg.NerdFont,
		Shuffle:         state.Shuffle,
		Repeat:          state.Repeat,
		Tape:            state.Tape,
		PaintBackground: cfg.PaintBackground,
		SeekStep:        time.Duration(cfg.SeekStep) * time.Second,
		Formats:         formats,
		Engine:          engine,
		NowPlaying:      remote,
		Notice:          notice,
		Resume:          state.Playback,
		Save:            save,

		AnimateUnfocused: cfg.AnimateUnfocused,
	})
	// Also after an error: the terminal may have gone away mid-session.
	keep.finish(m.State())
	return err
}

// keeper saves the state of the music folder at root. Its saves come from
// the goroutines of the interface and, at the end, from the main one.
type keeper struct {
	root string
	mu   sync.Mutex
	done bool
}

// save stores s, unless the last save was made.
func (k *keeper) save(s config.State) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.done {
		return nil
	}
	return config.SaveState(k.root, s)
}

// finish stores s as the last word: a save still under way from the session
// comes before it or not at all.
func (k *keeper) finish(s config.State) {
	if k == nil {
		return
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	k.done = true
	_ = config.SaveState(k.root, s)
}

// musicFolder returns the music folder to run on, and the track to play
// right away if the command line named one. The folder is the one of the
// -dir flag, else the argument, else the configured one, else the default.
// An argument that is a file is a track to play instead: in that music
// folder if it lies in there, and otherwise with its own folder as the
// music folder.
func musicFolder(configured, dirFlag, arg string, formats library.Formats) (root, play string, err error) {
	if arg != "" {
		path, err := filepath.Abs(config.ExpandHome(arg))
		if err != nil {
			return "", "", err
		}
		if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
			if !formats.Playable(path) {
				return "", "", fmt.Errorf("cannot play %s: cassetty plays %s", arg, strings.Join(audio.SupportedFormats(), " "))
			}
			play, arg = path, ""
		}
	}
	root = cmp.Or(dirFlag, arg, configured)
	if root == "" {
		root = config.DefaultMusicDir()
	}
	if root, err = filepath.Abs(config.ExpandHome(root)); err != nil {
		return "", "", err
	}
	if play != "" && !library.Within(root, play) {
		root = filepath.Dir(play)
	}
	return root, play, nil
}

// checkTheme accepts a known theme name, or "" for none.
func checkTheme(name string) error {
	if name != "" && !slices.Contains(ui.ThemeNames(), name) {
		return fmt.Errorf("unknown theme %q (choose from %s)", name, strings.Join(ui.ThemeNames(), ", "))
	}
	return nil
}
