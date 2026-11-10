package main

import (
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/samuelb/cassetty/internal/library"
)

func TestBuildVersion(t *testing.T) {
	module := func(v string) *debug.BuildInfo {
		return &debug.BuildInfo{Main: debug.Module{Path: "github.com/samuelb/cassetty", Version: v}}
	}
	for _, c := range []struct {
		set  string
		info *debug.BuildInfo
		want string
	}{
		{"v1.2.3", module("v9.9.9"), "v1.2.3"}, // a release build knows best
		{"dev", module("v1.4.0"), "v1.4.0"},    // go install …@v1.4.0
		{"dev", module("(devel)"), "dev"},
		{"dev", module(""), "dev"},
		{"dev", nil, "dev"},
	} {
		if got := buildVersion(c.set, c.info); got != c.want {
			t.Errorf("buildVersion(%q, %+v) = %q, want %q", c.set, c.info, got, c.want)
		}
	}
}

func TestMusicFolder(t *testing.T) {
	music, other := t.TempDir(), t.TempDir()
	for _, p := range []string{
		filepath.Join(music, "Artist", "Album", "01.mp3"),
		filepath.Join(other, "single.flac"),
		filepath.Join(other, "cover.jpg"),
	} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	formats := library.NewFormats([]string{"mp3", "flac"})
	track := filepath.Join(music, "Artist", "Album", "01.mp3")
	single := filepath.Join(other, "single.flac")

	for _, c := range []struct {
		what                 string
		configured, dir, arg string
		wantRoot, wantPlay   string
	}{
		{"the configured folder", music, "", "", music, ""},
		{"a folder as the argument", music, "", other, other, ""},
		{"the -dir flag", music, other, "", other, ""},
		{"the -dir flag before the argument", "", other, music, other, ""},
		{"a track of the music folder", music, "", track, music, track},
		{"a track of the -dir folder", "", music, track, music, track},
		{"a track outside the music folder", music, "", single, other, single},
		{"a track outside the -dir folder", "", music, single, other, single},
		{"a folder that is not there", "", "", filepath.Join(other, "nowhere"), filepath.Join(other, "nowhere"), ""},
	} {
		root, play, err := musicFolder(c.configured, c.dir, c.arg, formats)
		if err != nil || root != c.wantRoot || play != c.wantPlay {
			t.Errorf("%s: folder %q, track %q, error %v; want folder %q, track %q", c.what, root, play, err, c.wantRoot, c.wantPlay)
		}
	}

	if _, _, err := musicFolder(music, "", filepath.Join(other, "cover.jpg"), formats); err == nil || !strings.Contains(err.Error(), "cannot play") {
		t.Errorf("a file that is no track: error %v", err)
	}
}
