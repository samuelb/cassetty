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
