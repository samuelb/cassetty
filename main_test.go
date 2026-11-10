package main

import (
	"runtime/debug"
	"testing"
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
