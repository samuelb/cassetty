package audio

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gopxl/beep/v2"
)

// brokenStream plays good frames and then panics, as the decoders do on
// some damaged files.
type brokenStream struct{ good int }

func (s *brokenStream) Stream(p [][2]float64) (int, bool) {
	if s.good <= 0 {
		panic("index out of range [45] with length 45")
	}
	n := min(len(p), s.good)
	for i := range p[:n] {
		p[i] = [2]float64{0.25, 0.25}
	}
	s.good -= n
	return n, true
}
func (s *brokenStream) Err() error    { return nil }
func (s *brokenStream) Len() int      { return 1 << 20 }
func (s *brokenStream) Position() int { return 0 }
func (s *brokenStream) Seek(int) error {
	panic("seek on a damaged file")
}
func (s *brokenStream) Close() error { return nil }

func brokenSource(good int) *Source {
	return &Source{
		Path:   "damaged.ogg",
		stream: &brokenStream{good: good},
		format: beep.Format{SampleRate: SampleRate, NumChannels: 2, Precision: 2},
	}
}

// withoutFFmpeg leaves only the native decoders for the test.
func withoutFFmpeg(t *testing.T) {
	t.Helper()
	lookupFFmpeg()
	ffmpeg, ffprobe := ffmpegPath, ffprobePath
	ffmpegPath, ffprobePath = "", ""
	t.Cleanup(func() { ffmpegPath, ffprobePath = ffmpeg, ffprobe })
}

func TestDeckSurvivesADecoderPanic(t *testing.T) {
	e := newTestEngine()
	gen := e.Start(brokenSource(1000), true)
	out := make([][2]float64, 4096)
	e.deck.Stream(out) // must not panic

	ev := nextEvent(t, e)
	if ev.Gen != gen || ev.Next != 0 || ev.Err == nil {
		t.Fatalf("event %+v, want track %d ending with an error", ev, gen)
	}
	if !strings.Contains(ev.Err.Error(), "index out of range") {
		t.Errorf("the error %q does not say what went wrong", ev.Err)
	}
	if out[0][0] == 0 {
		t.Error("the frames before the damage were not played")
	}
	if e.Speed() != 0 {
		t.Error("the deck plays on after the track broke")
	}
	// The deck is still usable.
	if err := e.Seek(0); err == nil {
		t.Error("seeking in the damaged track reported no error")
	}
	e.Eject()
}

func TestDeckRollsOnPastADamagedTrack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "good.wav")
	writeWAV(t, path, 44100, 50*time.Millisecond)
	good, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	e := newTestEngine()
	genA := e.Start(brokenSource(100), true)
	genB := e.Queue(good)
	e.deck.Stream(make([][2]float64, SampleRate.N(200*time.Millisecond)))

	if ev := nextEvent(t, e); ev.Gen != genA || ev.Next != genB || ev.Err == nil {
		t.Fatalf("event %+v, want track %d breaking off into %d", ev, genA, genB)
	}
	if ev := nextEvent(t, e); ev.Gen != genB || ev.Err != nil {
		t.Fatalf("event %+v, want track %d ending cleanly", ev, genB)
	}
}

// damagedWAV writes a tone whose header has the bytes at the given offsets
// changed.
func damagedWAV(t *testing.T, rate int, damage map[int]byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "damaged.wav")
	writeWAV(t, path, rate, 100*time.Millisecond)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for at, b := range damage {
		data[at] = b
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestOpenSurvivesDamagedHeaders(t *testing.T) {
	for name, damage := range map[string]map[int]byte{
		"a negative sample rate":   {27: 0xe2},
		"a sample rate of zero":    {24: 0, 25: 0, 26: 0, 27: 0},
		"an oversized fmt chunk":   {16: 0x3c},
		"an absurd fmt chunk size": {19: 0xff},
		"no channels":              {22: 0},
		"frames of no bytes":       {32: 0},
	} {
		t.Run(name, func(t *testing.T) {
			path := damagedWAV(t, 48000, damage)

			// With ffmpeg to fall back on the file may still play; either
			// way nothing may panic, and what opens must be playable.
			src, err := Open(path)
			if err == nil {
				e := newTestEngine()
				e.Start(src, true)
				e.Queue(nil)
				e.deck.Stream(make([][2]float64, 4096))
				e.Eject()
			}

			withoutFFmpeg(t)
			src, err = Open(path)
			if err != nil {
				return // refused, as it should be
			}
			if src.SampleRate <= 0 {
				t.Fatalf("opened with a sample rate of %d", src.SampleRate)
			}
			e := newTestEngine()
			e.Start(src, true)
			e.deck.Stream(make([][2]float64, 4096))
			e.Eject()
		})
	}
}

func TestQueueingADamagedTrackLeavesTheDeckUsable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "good.wav")
	writeWAV(t, path, 44100, 50*time.Millisecond)
	good, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	// A source with a sample rate no resampler can be built for.
	bad := brokenSource(100)
	bad.format.SampleRate = -5

	e := newTestEngine()
	e.Start(good, true)
	func() {
		defer func() { _ = recover() }()
		e.Queue(bad)
	}()
	done := make(chan struct{})
	go func() {
		e.Position()
		e.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the deck is locked up after queueing a damaged track")
	}
}
