package nowplaying

import (
	"os"
	"testing"
	"time"

	"github.com/ebitengine/purego/objc"
)

// TestMain runs the tests the way cassetty runs: with the main thread
// serving its run loop.
func TestMain(m *testing.M) {
	code := 0
	Main(func() { code = m.Run() })
	os.Exit(code)
}

// onMainWait runs fn on the main thread and waits for it.
func onMainWait(t *testing.T, fn func()) {
	t.Helper()
	done := make(chan struct{})
	onMain(func() {
		defer close(done)
		fn()
	})
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the main thread did not run the queued func")
	}
}

func TestMainThread(t *testing.T) {
	if err := load(); err != nil {
		t.Fatal(err)
	}
	for _, k := range []objc.ID{keyTitle, keyArtist, keyAlbum, keyDuration, keyElapsed, keyRate, keyMediaType} {
		if k == 0 {
			t.Fatal("a Now Playing key did not resolve")
		}
	}
	var order []int
	for i := range 3 {
		onMain(func() { order = append(order, i) })
	}
	onMainWait(t, func() {
		if pthreadMainNP() == 0 {
			t.Error("queued func ran off the main thread")
		}
	})
	if len(order) != 3 || order[0] != 0 || order[1] != 1 || order[2] != 2 {
		t.Errorf("queued funcs ran as %v", order)
	}
}

func TestNSString(t *testing.T) {
	onMainWait(t, func() {
		for in, want := range map[string]string{
			"Für Elise":      "Für Elise",
			"bad \xff byte":  "bad � byte",
			"nul\x00 inside": "nul inside",
		} {
			s := nsString(in)
			if s == 0 {
				t.Errorf("nsString(%q) = nil", in)
				continue
			}
			if got := goString(s); got != want {
				t.Errorf("nsString(%q) = %q, want %q", in, got, want)
			}
		}
		if nsString("") != 0 {
			t.Error(`nsString("") is not nil`)
		}
	})
}

// TestSession publishes to the real Now Playing center, which shows in
// Control Center, so it only runs on request:
//
//	CASSETTY_TEST_NOWPLAYING=1 go test ./internal/nowplaying
func TestSession(t *testing.T) {
	if os.Getenv("CASSETTY_TEST_NOWPLAYING") == "" {
		t.Skip("set CASSETTY_TEST_NOWPLAYING=1 to publish to the Now Playing center")
	}
	s, err := Start()
	if err != nil {
		t.Fatal(err)
	}
	s.Update(Info{Path: "/tmp/test-tone.wav", Title: "Test Tone", Artist: "cassetty", Length: time.Minute, Position: 5 * time.Second, State: Paused})
	onMainWait(t, func() {
		center := send(class("MPNowPlayingInfoCenter"), "defaultCenter")
		info := send(center, "nowPlayingInfo")
		if got := goString(send(info, "objectForKey:", keyTitle)); got != "Test Tone" {
			t.Errorf("published title %q", got)
		}
		if got := objc.Send[float64](send(info, "objectForKey:", keyElapsed), objc.RegisterName("doubleValue")); got != 5 {
			t.Errorf("published position %v", got)
		}
		if got := objc.Send[uint](center, objc.RegisterName("playbackState")); got != mpPaused {
			t.Errorf("published state %d", got)
		}
	})
	s.Close()
	onMainWait(t, func() {
		center := send(class("MPNowPlayingInfoCenter"), "defaultCenter")
		if send(center, "nowPlayingInfo") != 0 {
			t.Error("Close left the track published")
		}
	})
}

func goString(s objc.ID) string {
	if s == 0 {
		return ""
	}
	return objc.Send[string](s, objc.RegisterName("UTF8String"))
}
