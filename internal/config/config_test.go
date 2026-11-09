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
	if s := LoadState(); !reflect.DeepEqual(s, State{}) {
		t.Fatalf("without a state file: %+v", s)
	}
	vol := 40
	want := State{
		Volume: &vol, Theme: "amber", Shuffle: true, Repeat: "all", LastDir: "/music/a",
		Playback: Playback{
			Playlist: []string{"/music/a/1.mp3", "/music/a/2.mp3"},
			Order:    []int{1, 0},
			Track:    "/music/a/2.mp3", Index: 1, Position: 83.25,
		},
	}
	if err := SaveState(want); err != nil {
		t.Fatal(err)
	}
	if got := LoadState(); !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadState = %+v, want %+v", got, want)
	}

	// Without a loaded track or playlist nothing of the playback is written.
	if err := SaveState(State{Theme: "vfd"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(statePath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "playback") {
		t.Fatalf("empty playback written: %s", b)
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
	old := State{Theme: "amber", Playback: Playback{Playlist: []string{"/music/1.mp3"}, Track: "/music/1.mp3"}}
	if err := SaveState(old); err != nil {
		t.Fatal(err)
	}
	if err := SaveState(old); err != nil { // over a file that is there
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
	if err := SaveState(State{Theme: "vfd"}); err == nil {
		t.Fatal("saving into a read-only folder reported no error")
	}
	if got := LoadState(); !reflect.DeepEqual(got, old) {
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
