package audio

import (
	"math"
	"path/filepath"
	"testing"
	"time"
)

// sine is an endless 440 Hz tone that counts the frames read from it.
type sine struct {
	amp  float64
	read int
}

func (s *sine) pull(dst [][2]float64) int {
	for i := range dst {
		v := s.amp * math.Sin(2*math.Pi*440*float64(s.read)/float64(SampleRate))
		dst[i] = [2]float64{v, v}
		s.read++
	}
	return len(dst)
}

// run passes d of audio through the transport in output-sized chunks and
// returns what came out.
func (t *transport) run(src *sine, d time.Duration, playing bool) [][2]float64 {
	out := make([][2]float64, SampleRate.N(d))
	for i := 0; i < len(out); i += 441 {
		t.process(out[i:min(len(out), i+441)], playing, src.pull)
	}
	return out
}

func rms(frames [][2]float64) float64 {
	var sum float64
	for _, f := range frames {
		sum += f[0]*f[0] + f[1]*f[1]
	}
	return math.Sqrt(sum / float64(2*len(frames)))
}

func TestTapeFor(t *testing.T) {
	for codec, want := range map[string]Tape{
		"FLAC": TapeMetal, "WAV": TapeMetal, "ALAC": TapeMetal, "PCM_S16BE": TapeMetal,
		"MP3": TapeChrome, "VORBIS": TapeChrome, "OPUS": TapeChrome,
		"AAC": TapeNormal, "WMAV2": TapeNormal,
	} {
		if got := TapeFor(codec); got != want {
			t.Errorf("TapeFor(%q) = %v, want %v", codec, got, want)
		}
	}
}

func TestTransportPassesAudioThrough(t *testing.T) {
	tr := newTransport()
	tr.motor = 1
	src := &sine{amp: 0.5}
	out := tr.run(src, 500*time.Millisecond, true)
	ref := &sine{amp: 0.5}
	want := make([][2]float64, len(out))
	ref.pull(want)
	for i := range out {
		if out[i] != want[i] {
			t.Fatalf("frame %d is %v, want %v", i, out[i], want[i])
		}
	}
}

func TestTapeWindsDownAndUp(t *testing.T) {
	tr := newTransport()
	tr.motor = 1
	src := &sine{amp: 0.5}
	full := rms(tr.run(src, 100*time.Millisecond, true))

	// Pausing winds the tape down: it slows to a stop over tapeStop,
	// getting quieter, and reads about half as many frames as it plays.
	before := src.read
	down := tr.run(src, time.Duration(tapeStop*float64(time.Second)), false)
	if tr.motor != 0 {
		t.Fatalf("motor still at %v after winding down", tr.motor)
	}
	if got, want := float64(src.read-before), float64(len(down))/2; math.Abs(got-want) > want*0.02 {
		t.Errorf("read %v frames while winding down, want about %v", got, want)
	}
	early, late := rms(down[:len(down)/5]), rms(down[len(down)*4/5:])
	if !(early < full && late < early/3) {
		t.Errorf("winding down should fade: full %.3f, early %.3f, late %.3f", full, early, late)
	}

	// Playing again winds it back up to speed over tapeStart.
	before = src.read
	up := tr.run(src, time.Duration(tapeStart*float64(time.Second)), true)
	if tr.motor != 1 {
		t.Fatalf("motor at %v after winding up", tr.motor)
	}
	if got, want := float64(src.read-before), float64(len(up))/2; math.Abs(got-want) > want*0.02 {
		t.Errorf("read %v frames while winding up, want about %v", got, want)
	}
}

func TestEngineWindsTapeDownBeforeSuspending(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tone.wav")
	writeWAV(t, path, 44100, 3*time.Second)
	src, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	e, _ := NewEngine(true)
	defer e.Close()
	out := &fakeOutput{}
	done := make(chan struct{})
	defer close(done)
	go e.deck.suspendWhenIdle(out, 20*time.Millisecond, done)

	e.Start(src, true)
	time.Sleep(500 * time.Millisecond)
	if s := e.Speed(); s != 1 {
		t.Fatalf("speed %v while playing", s)
	}
	e.SetPlaying(false)
	p0 := e.Position()
	time.Sleep(time.Duration(tapeStop * float64(time.Second) / 2))
	if s := e.Speed(); s <= 0 || s >= 1 {
		t.Fatalf("speed %v while winding down", s)
	}
	if out.suspended.Load() {
		t.Fatal("output suspended while the tape winds down")
	}
	deadline := time.Now().Add(2 * time.Second)
	for !out.suspended.Load() {
		if time.Now().After(deadline) {
			t.Fatal("output not suspended after the tape wound down")
		}
		time.Sleep(time.Millisecond)
	}
	if e.Speed() != 0 || e.Position() <= p0 {
		t.Fatalf("speed %v, position %v after winding down from %v", e.Speed(), e.Position(), p0)
	}

	// Seeking while paused does not play the new position.
	if err := e.Seek(0); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if p := e.Position(); p != 0 {
		t.Fatalf("position moved to %v after seeking while paused", p)
	}
}

// BenchmarkTransport measures one second of audio through the transport.
func BenchmarkTransport(b *testing.B) {
	tr := newTransport()
	tr.motor = 1
	src := &sine{amp: 0.5}
	buf := make([][2]float64, 441)
	for b.Loop() {
		for range 100 {
			tr.process(buf, true, src.pull)
		}
	}
}
