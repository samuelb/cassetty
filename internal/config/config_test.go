package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDefaultMusicDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if got := DefaultMusicDir(); got != "." {
		t.Fatalf("without ~/Music: got %q, want %q", got, ".")
	}
	music := filepath.Join(home, "Music")
	if err := os.Mkdir(music, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := DefaultMusicDir(); got != music {
		t.Fatalf("with ~/Music: got %q, want %q", got, music)
	}
}

func TestStateRoundTrip(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if s := LoadState("/music"); !reflect.DeepEqual(s, State{}) {
		t.Fatalf("without a state file: %+v", s)
	}
	vol := 40
	want := State{
		Volume: &vol, Theme: "amber", Shuffle: true, Repeat: "all",
		Session: Session{
			LastDir: "/music/a",
			Playback: Playback{
				Playlist: []string{"/music/a/1.mp3", "/music/a/2.mp3"},
				Order:    []int{1, 0},
				Track:    "/music/a/2.mp3", Index: 1, Position: 83.25,
			},
		},
	}
	if err := SaveState("/music", want); err != nil {
		t.Fatal(err)
	}
	if got := LoadState("/music"); !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadState = %+v, want %+v", got, want)
	}

	// Without a loaded track or playlist nothing of the playback is written.
	if err := SaveState("/music", State{Theme: "vfd"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(statePath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "playback") || strings.Contains(string(b), "libraries") {
		t.Fatalf("empty playback written: %s", b)
	}
}

func TestEveryLibraryHasItsSession(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	music := Session{LastDir: "/music/a", Playback: Playback{
		Playlist: []string{"/music/a/1.mp3", "/music/a/2.mp3"}, Track: "/music/a/2.mp3", Index: 1, Position: 12,
	}}
	if err := SaveState("/music", State{Theme: "amber", Session: music}); err != nil {
		t.Fatal(err)
	}

	// Another folder starts without a session, with the settings.
	if got := LoadState("/tmp/album"); got.Theme != "amber" || !reflect.DeepEqual(got.Session, Session{}) {
		t.Fatalf("another folder starts with %+v", got)
	}
	album := Session{LastDir: "/tmp/album", Playback: Playback{Playlist: []string{"/tmp/album/x.mp3"}}}
	if err := SaveState("/tmp/album/", State{Theme: "vfd", Session: album}); err != nil {
		t.Fatal(err)
	}

	// Which leaves the first one's as it was, and changes the settings.
	if got := LoadState("/music"); got.Theme != "vfd" || !reflect.DeepEqual(got.Session, music) {
		t.Fatalf("back in the first folder: %+v, want the session %+v", got, music)
	}
	if got := LoadState("/tmp/album"); !reflect.DeepEqual(got.Session, album) {
		t.Fatalf("the other folder's session: %+v, want %+v", got.Session, album)
	}

	// A session with nothing in it is not kept.
	if err := SaveState("/tmp/album", State{Session: Session{LastDir: "/tmp/album"}}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(statePath()); strings.Contains(string(b), "/tmp/album") {
		t.Fatalf("an empty session is kept: %s", b)
	}
}

// Before every library had its session, there was one for all.
func TestStateFromBeforeLibrariesHadSessions(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	old := `{
  "volume": 55, "theme": "amber", "shuffle": false, "tape": true, "last_dir": "/music/a",
  "playback": {"playlist": ["/music/a/1.mp3", "/music/a/2.mp3"], "track": "/music/a/2.mp3", "index": 1, "position": 7.5}
}`
	if err := os.MkdirAll(filepath.Dir(statePath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath(), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	want := Session{LastDir: "/music/a", Playback: Playback{
		Playlist: []string{"/music/a/1.mp3", "/music/a/2.mp3"}, Track: "/music/a/2.mp3", Index: 1, Position: 7.5,
	}}
	got := LoadState("/music")
	if got.Volume == nil || *got.Volume != 55 || got.Theme != "amber" || !got.Tape || !reflect.DeepEqual(got.Session, want) {
		t.Fatalf("LoadState = %+v", got)
	}

	// A run on another folder leaves it for the library it belongs to.
	if err := SaveState("/tmp/album", State{Theme: "vfd", Session: Session{Playback: Playback{Playlist: []string{"/tmp/album/x.mp3"}}}}); err != nil {
		t.Fatal(err)
	}
	if got := LoadState("/music"); got.Theme != "vfd" || !reflect.DeepEqual(got.Session, want) {
		t.Fatalf("after a run on another folder: %+v", got)
	}

	// Saved for its library, it is that library's session from then on.
	want.Playback.Position = 30
	if err := SaveState("/music", State{Session: want}); err != nil {
		t.Fatal(err)
	}
	if got := LoadState("/music"); !reflect.DeepEqual(got.Session, want) {
		t.Fatalf("the library's session: %+v, want %+v", got.Session, want)
	}
	if got := LoadState("/elsewhere"); !reflect.DeepEqual(got.Session, Session{}) {
		t.Fatalf("a third folder gets the session %+v", got.Session)
	}
}

func TestLoadLeavesMusicDirUnset(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MusicDir != "" {
		t.Fatalf("MusicDir = %q, want empty", cfg.MusicDir)
	}
}

func TestSampleIsDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := WriteSample(path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := Default(); got != want {
		t.Fatalf("the sample config loads as %+v, want the defaults %+v", got, want)
	}
}

func TestSaveStateLeavesNoHalfWrittenFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", home)
	dir := filepath.Dir(statePath())
	old := State{Theme: "amber", Session: Session{Playback: Playback{Playlist: []string{"/music/1.mp3"}, Track: "/music/1.mp3"}}}
	if err := SaveState("/music", old); err != nil {
		t.Fatal(err)
	}
	if err := SaveState("/music", old); err != nil { // over a file that is there
		t.Fatal(err)
	}
	if names := dirNames(t, dir); !reflect.DeepEqual(names, []string{"state.json"}) {
		t.Fatalf("the state folder holds %q, want state.json alone", names)
	}
	if fi, err := os.Stat(statePath()); err != nil || fi.Mode().Perm() != 0o644 {
		t.Fatalf("state file: %v, mode %v", err, fi.Mode().Perm())
	}

	// A save that fails leaves the state as it was, and nothing behind.
	if os.Getuid() == 0 {
		t.Skip("root writes to a read-only folder")
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755)
	if err := SaveState("/music", State{Theme: "vfd"}); err == nil {
		t.Fatal("saving into a read-only folder reported no error")
	}
	if got := LoadState("/music"); !reflect.DeepEqual(got, old) {
		t.Fatalf("after a failed save the state is %+v, want %+v", got, old)
	}
	if names := dirNames(t, dir); !reflect.DeepEqual(names, []string{"state.json"}) {
		t.Fatalf("the failed save left %q behind", names)
	}
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	des, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, de := range des {
		names = append(names, de.Name())
	}
	return names
}
