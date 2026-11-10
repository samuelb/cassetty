package audio

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gopxl/beep/v2"
)

// writeWAV writes a 16 bit stereo sine wave.
func writeWAV(t *testing.T, path string, rate int, d time.Duration) {
	t.Helper()
	n := int(d.Seconds() * float64(rate))
	data := make([]byte, n*4)
	for i := 0; i < n; i++ {
		v := int16(0.5 * 32767 * math.Sin(2*math.Pi*440*float64(i)/float64(rate)))
		binary.LittleEndian.PutUint16(data[i*4:], uint16(v))
		binary.LittleEndian.PutUint16(data[i*4+2:], uint16(v))
	}
	h := make([]byte, 44)
	copy(h, "RIFF")
	binary.LittleEndian.PutUint32(h[4:], uint32(36+len(data)))
	copy(h[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(h[16:], 16)
	binary.LittleEndian.PutUint16(h[20:], 1)
	binary.LittleEndian.PutUint16(h[22:], 2)
	binary.LittleEndian.PutUint32(h[24:], uint32(rate))
	binary.LittleEndian.PutUint32(h[28:], uint32(rate*4))
	binary.LittleEndian.PutUint16(h[32:], 4)
	binary.LittleEndian.PutUint16(h[34:], 16)
	copy(h[36:], "data")
	binary.LittleEndian.PutUint32(h[40:], uint32(len(data)))
	if err := os.WriteFile(path, append(h, data...), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEnginePlaysSeeksAndEnds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tone.wav")
	writeWAV(t, path, 48000, 600*time.Millisecond) // resampled to 44.1k

	src, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if src.Codec != "WAV" || src.SampleRate != 48000 || src.Channels != 2 {
		t.Fatalf("unexpected source info %+v", src)
	}
	if d := src.Length - 600*time.Millisecond; d < -time.Millisecond || d > time.Millisecond {
		t.Fatalf("length %v", src.Length)
	}

	e, _ := NewEngine(true)
	defer e.Close()
	gen := e.Start(src, true)
	time.Sleep(150 * time.Millisecond)
	if p := e.Position(); p <= 0 {
		t.Fatalf("position did not advance: %v", p)
	}

	frames := make([][2]float64, 1024)
	e.Snapshot(frames)
	var energy float64
	for _, f := range frames {
		energy += f[0] * f[0]
	}
	if energy == 0 {
		t.Fatal("tap captured no audio")
	}

	if err := e.Seek(500 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-e.Events():
		if ev.Gen != gen || ev.Err != nil {
			t.Fatalf("unexpected event %+v (gen %d)", ev, gen)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no end-of-track event")
	}

	// Pausing stops the play head, once the tape has wound down.
	if err := e.Seek(0); err != nil {
		t.Fatal(err)
	}
	e.SetPlaying(false)
	deadline := time.Now().Add(time.Second)
	for e.Speed() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("the tape still moves at %v after pausing", e.Speed())
		}
		time.Sleep(time.Millisecond)
	}
	p0 := e.Position()
	time.Sleep(100 * time.Millisecond)
	if p1 := e.Position(); p1 != p0 {
		t.Fatalf("position moved while paused: %v -> %v", p0, p1)
	}
}

// fakeOutput records whether the idle watcher has suspended it.
type fakeOutput struct{ suspended atomic.Bool }

func (o *fakeOutput) Suspend() error { o.suspended.Store(true); return nil }
func (o *fakeOutput) Resume() error  { o.suspended.Store(false); return nil }

func TestEngineSuspendsOutputWhileIdle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tone.wav")
	writeWAV(t, path, 44100, 300*time.Millisecond)
	src, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}

	// The null output pulls the deck in real time, as the device would.
	e, _ := NewEngine(true)
	defer e.Close()
	out := &fakeOutput{}
	done := make(chan struct{})
	defer close(done)
	go e.deck.suspendWhenIdle(out, 20*time.Millisecond, done)

	expect := func(suspended bool, when string) {
		t.Helper()
		deadline := time.Now().Add(time.Second)
		for out.suspended.Load() != suspended {
			if time.Now().After(deadline) {
				t.Fatalf("output suspended = %v %s", !suspended, when)
			}
			time.Sleep(time.Millisecond)
		}
	}
	expect(true, "with nothing loaded")
	e.Start(src, true)
	expect(false, "after starting a track")
	e.SetPlaying(false)
	expect(true, "while paused")
	e.SetPlaying(true)
	expect(false, "after unpausing")
	select {
	case <-e.Events():
	case <-time.After(2 * time.Second):
		t.Fatal("no end-of-track event")
	}
	expect(true, "after the track ended")
	if err := e.Seek(0); err != nil {
		t.Fatal(err)
	}
	expect(false, "after seeking back into the track")
	e.Eject()
	expect(true, "after ejecting")
}

func TestOpenRejectsUnknown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.mp3")
	os.WriteFile(path, []byte("definitely not an mp3"), 0o644)
	if _, err := Open(path); err == nil {
		t.Fatal("expected an error for a broken file")
	}
}

// TestDevice exercises the real sound device at zero volume when
// CASSETTY_TEST_DEVICE is set.
func TestDevice(t *testing.T) {
	if os.Getenv("CASSETTY_TEST_DEVICE") == "" {
		t.Skip("CASSETTY_TEST_DEVICE not set")
	}
	path := filepath.Join(t.TempDir(), "tone.wav")
	writeWAV(t, path, 44100, time.Second)
	e, err := NewEngine(false)
	if err != nil {
		t.Fatalf("device init: %v", err)
	}
	defer e.Close()
	if e.Silent() {
		t.Fatal("engine fell back to silent mode")
	}
	e.SetVolume(0)
	src, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	e.Start(src, true)
	time.Sleep(400 * time.Millisecond)
	if p := e.Position(); p < 200*time.Millisecond {
		t.Fatalf("device is not pulling samples, position %v", p)
	}
}

// newTestEngine returns an engine without an output, whose deck the test
// pulls by hand.
func newTestEngine() *Engine {
	e := &Engine{events: make(chan Event, 8)}
	e.deck = newDeck(e.events)
	return e
}

// upToSpeed has the tape run at the playing speed at once, rather than get
// up to it, for the tests that are about something else.
func (e *Engine) upToSpeed() {
	e.deck.mu.Lock()
	e.deck.tape.motor = 1
	e.deck.publish()
	e.deck.mu.Unlock()
}

func nextEvent(t *testing.T, e *Engine) Event {
	t.Helper()
	select {
	case ev := <-e.Events():
		return ev
	default:
		t.Fatal("no event")
		return Event{}
	}
}

func TestDeckRollsOnToTheQueuedTrackWithoutAGap(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.wav"), filepath.Join(dir, "b.wav")
	writeWAV(t, a, 44100, 100*time.Millisecond)
	writeWAV(t, b, 48000, 100*time.Millisecond) // resampled
	srcA, err := Open(a)
	if err != nil {
		t.Fatal(err)
	}
	srcB, err := Open(b)
	if err != nil {
		t.Fatal(err)
	}

	e := newTestEngine()
	genA := e.Start(srcA, true)
	e.upToSpeed()
	genB := e.Queue(srcB)
	out := make([][2]float64, SampleRate.N(300*time.Millisecond))
	e.deck.Stream(out)

	if ev := nextEvent(t, e); ev.Gen != genA || ev.Next != genB || ev.Err != nil {
		t.Fatalf("event %+v, want track %d ending into %d", ev, genA, genB)
	}
	if ev := nextEvent(t, e); ev.Gen != genB || ev.Next != 0 {
		t.Fatalf("event %+v, want track %d ending the tape", ev, genB)
	}
	// Both 440 Hz tones play back to back: no run of silence until the end
	// of the second, which is the length of both.
	end := 0
	quiet := 0
	for i, f := range out {
		if math.Abs(f[0]) > 0.01 {
			end = i + 1
			quiet = 0
			continue
		}
		if quiet++; quiet > 2 && end > 0 && i < SampleRate.N(195*time.Millisecond) {
			t.Fatalf("gap at frame %d", i)
		}
	}
	if want := SampleRate.N(200 * time.Millisecond); end < want-2 || end > want+2 {
		t.Fatalf("music ends at frame %d, want about %d", end, want)
	}
	if p := e.Position(); p != srcB.Length {
		t.Fatalf("position %v at the end of the second track, want %v", p, srcB.Length)
	}
}

func TestDeckStopsWhenTheQueueIsCleared(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.wav"), filepath.Join(dir, "b.wav")
	writeWAV(t, a, 44100, 50*time.Millisecond)
	writeWAV(t, b, 44100, 50*time.Millisecond)
	srcA, _ := Open(a)
	srcB, _ := Open(b)

	e := newTestEngine()
	gen := e.Start(srcA, true)
	e.upToSpeed()
	e.Queue(srcB)
	e.Queue(nil)
	e.deck.Stream(make([][2]float64, SampleRate.N(100*time.Millisecond)))
	if ev := nextEvent(t, e); ev.Gen != gen || ev.Next != 0 {
		t.Fatalf("event %+v, want track %d ending the tape", ev, gen)
	}
	if e.Speed() != 0 {
		t.Fatal("the deck plays on without a queued track")
	}
}

// stalledStream is a decoder whose read does not return until it is
// released, like one reading from a disk that has to spin up first.
type stalledStream struct {
	entered chan struct{} // closed once a read is under way
	release chan struct{}
	once    sync.Once
	pos     int
}

func (s *stalledStream) Stream(p [][2]float64) (int, bool) {
	s.once.Do(func() { close(s.entered) })
	<-s.release
	clear(p)
	s.pos += len(p)
	return len(p), true
}
func (s *stalledStream) Err() error     { return nil }
func (s *stalledStream) Len() int       { return 1 << 30 }
func (s *stalledStream) Position() int  { return s.pos }
func (s *stalledStream) Seek(int) error { return nil }
func (s *stalledStream) Close() error   { return nil }

// stalledEngine returns an engine whose output is held up in a read of the
// loaded track until release is closed; pulled is closed once it returned.
func stalledEngine(t *testing.T) (e *Engine, release chan struct{}, pulled chan struct{}) {
	t.Helper()
	st := &stalledStream{entered: make(chan struct{}), release: make(chan struct{})}
	src := &Source{stream: st, format: beep.Format{SampleRate: SampleRate, NumChannels: 2, Precision: 2}}
	e = newTestEngine()
	e.Start(src, true)
	e.upToSpeed()
	pulled = make(chan struct{})
	go func() {
		e.deck.Stream(make([][2]float64, 256))
		close(pulled)
	}()
	<-st.entered
	return e, st.release, pulled
}

func TestAStalledReadDoesNotHoldUpThePlayer(t *testing.T) {
	e, release, pulled := stalledEngine(t)

	// What the player asks for on every frame must not wait for the read.
	asked := make(chan struct{})
	go func() {
		e.Position()
		e.Speed()
		e.Ended()
		e.SetVolume(0.5)
		e.Snapshot(make([][2]float64, 64))
		close(asked)
	}()
	select {
	case <-asked:
	case <-time.After(2 * time.Second):
		t.Fatal("the engine does not answer while a read is stalled")
	}
	if e.Speed() != 1 {
		t.Errorf("speed %v while a read is stalled, want the tape still running", e.Speed())
	}

	close(release)
	<-pulled
	if p := e.Position(); p <= 0 {
		t.Errorf("position %v after the read returned", p)
	}
}

func TestAStalledReadDoesNotHoldUpQuitting(t *testing.T) {
	e, release, pulled := stalledEngine(t)
	began := time.Now()
	e.Close()
	if took := time.Since(began); took > 4*closeTimeout {
		t.Errorf("closing the engine took %v while a read is stalled", took)
	}
	close(release)
	<-pulled
}
