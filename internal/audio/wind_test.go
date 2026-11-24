package audio

import (
	"errors"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/gopxl/beep/v2"
)

// reel is a track whose left channel tells the frame it is: it rises from 0
// at the first frame to 1 at the last. Its right channel tells which way the
// tape runs: a sawtooth that rises slowly and falls at once. The reel counts
// how often it was told to seek, and how many frames it gave.
type reel struct {
	frames, pos int
	seeks, read int
	noSeek      bool // seeking fails
	panics      bool // seeking panics, as the decoders may on a damaged file
	rough       int  // frames after a seek that are not those of the track, as from an MP3 decoder
	wrong       int  // how many of them are still to come
}

func (r *reel) frame(i int) [2]float64 {
	return [2]float64{float64(i) / float64(r.frames), float64(i%4096)/4096 - 0.5}
}

// rising returns the share of the frames in which the right channel rises.
func rising(out [][2]float64) float64 {
	n := 0
	for i := 1; i < len(out); i++ {
		if out[i][1] > out[i-1][1] {
			n++
		}
	}
	return float64(n) / float64(len(out)-1)
}

// The play head as the engine tells it is ahead of the frame that plays by
// what the transport has read ahead.
const readAhead = 15 * time.Millisecond

func (r *reel) Stream(p [][2]float64) (int, bool) {
	n := min(len(p), r.frames-r.pos)
	for i := range p[:n] {
		p[i] = r.frame(r.pos + i)
		if r.wrong > 0 {
			p[i], r.wrong = [2]float64{9, 9}, r.wrong-1
		}
	}
	r.pos += n
	r.read += n
	return n, n > 0
}
func (r *reel) Err() error    { return nil }
func (r *reel) Len() int      { return r.frames }
func (r *reel) Position() int { return r.pos }
func (r *reel) Close() error  { return nil }
func (r *reel) Seek(p int) error {
	if r.panics {
		panic("seek on a damaged file")
	}
	if r.noSeek {
		return errors.New("no seeking on this reel")
	}
	r.seeks++
	r.pos, r.wrong = p, r.rough
	return nil
}

// reelSource returns a source that plays a reel of the length d, and the
// reel.
func reelSource(d time.Duration) (*Source, *reel) {
	r := &reel{frames: SampleRate.N(d)}
	return &Source{stream: r, Length: d, format: beep.Format{SampleRate: SampleRate, NumChannels: 2, Precision: 2}}, r
}

// reelEngine returns an engine without an output with a reel of the length
// d loaded, and the reel. A deck that plays is at speed.
func reelEngine(d time.Duration, play bool) (*Engine, *reel) {
	src, r := reelSource(d)
	e := newTestEngine()
	e.Start(src, play)
	if play {
		e.upToSpeed()
	}
	return e, r
}

// pull plays d of audio from the deck and returns it.
func (e *Engine) pull(d time.Duration) [][2]float64 {
	out := make([][2]float64, SampleRate.N(d))
	for i := 0; i < len(out); i += 441 {
		e.deck.Stream(out[i:min(len(out), i+441)])
	}
	return out
}

// wound plays the deck until the wind is over, and returns what it played
// and the highest and the lowest speed of the tape meanwhile.
func (e *Engine) wound(t *testing.T) (out [][2]float64, fastest, slowest float64) {
	t.Helper()
	fastest, slowest = e.Speed(), e.Speed()
	for e.deck.tape.wind != nil {
		if len(out) > SampleRate.N(20*time.Second) {
			t.Fatalf("the tape still winds after %d frames, at %v", len(out), e.Position())
		}
		out = append(out, e.pull(10*time.Millisecond)...)
		fastest, slowest = max(fastest, e.Speed()), min(slowest, e.Speed())
	}
	return out, fastest, slowest
}

func near(got, want, by time.Duration) bool {
	return got >= want-by && got <= want+by
}

func TestWindingForwardPlaysTheTapeFast(t *testing.T) {
	e, r := reelEngine(60*time.Second, true)
	before := e.pull(time.Second)
	from := e.Position()
	if err := e.Wind(5 * time.Second); err != nil {
		t.Fatal(err)
	}
	out, fastest, slowest := e.wound(t)

	if took := SampleRate.D(len(out)); took > time.Second {
		t.Errorf("winding 5 s forward took %v", took)
	}
	if fastest < 10 || fastest > windSpeed || slowest < 1 {
		t.Errorf("the tape wound at %v to %v times the playing speed", slowest, fastest)
	}
	if e.Speed() != 1 {
		t.Errorf("speed %v after the wind", e.Speed())
	}
	if got := e.Position(); !near(got, from+5*time.Second, readAhead) {
		t.Errorf("the head is at %v, want %v", got, from+5*time.Second)
	}
	if r.seeks != 0 {
		t.Errorf("the decoder was told to seek %d times to wind forward", r.seeks)
	}

	// The music is heard all the way: it carries on from the frame that
	// played last, races by, quieter than it plays, and plays on from where
	// the head got to as it is on the tape.
	last := before[len(before)-1]
	for i, f := range out {
		if f[0] == 0 {
			t.Fatalf("frame %d of the wind is silent", i)
		}
	}
	if step := math.Hypot(out[0][0]-last[0], out[0][1]-last[1]); step > 0.002 {
		t.Errorf("the wind starts %v from the frame before it", step)
	}
	if got := rising(out); got < 0.9 {
		t.Errorf("the tape runs forward in %.0f%% of the wind", got*100)
	}
	if quiet, loud := rms(out[len(out)/2-500:len(out)/2+500]), rms(before); quiet > loud/2 {
		t.Errorf("the tape is as loud as %v at full speed, and %v when it plays", quiet, loud)
	}
	after := e.pull(100 * time.Millisecond)
	at := int(math.Round(after[0][0] * float64(r.frames)))
	// The wind ended somewhere in the 10 ms that were played last.
	if got := SampleRate.D(at); got < 6*time.Second || got > 6*time.Second+11*time.Millisecond {
		t.Errorf("the tape plays on from %v", got)
	}
	for i, f := range after {
		if f != r.frame(at+i) {
			t.Fatalf("frame %d after the wind is %v, want %v", i, f, r.frame(at+i))
		}
	}
}

func TestRewindingPlaysTheTapeInReverse(t *testing.T) {
	e, r := reelEngine(60*time.Second, true)
	e.Seek(20 * time.Second)
	e.pull(time.Second)
	from := e.Position()
	if err := e.Wind(-5 * time.Second); err != nil {
		t.Fatal(err)
	}
	out, fastest, slowest := e.wound(t)

	if slowest > -10 || fastest > 1 {
		t.Errorf("the tape wound at %v to %v times the playing speed", slowest, fastest)
	}
	if got := rising(out); got > 0.1 {
		t.Errorf("the tape runs forward in %.0f%% of the rewind", got*100)
	}
	if e.Speed() != 1 {
		t.Errorf("speed %v after the rewind", e.Speed())
	}
	want := from - 5*time.Second
	if got := e.Position(); !near(got, want, 20*time.Millisecond) {
		t.Errorf("the head is at %v, want %v", got, want)
	}
	after := e.pull(100 * time.Millisecond)
	at := int(math.Round(after[0][0] * float64(r.frames)))
	if got := SampleRate.D(at); !near(got, want, 20*time.Millisecond) {
		t.Errorf("the tape plays on from %v, want %v", got, want)
	}
	for i, f := range after {
		if f != r.frame(at+i) {
			t.Fatalf("frame %d after the rewind is %v, want %v", i, f, r.frame(at+i))
		}
	}
}

func TestRewindingLetsTheDecoderFindItsFeet(t *testing.T) {
	e, r := reelEngine(60*time.Second, true)
	e.Seek(30 * time.Second)
	r.rough = 1500
	e.pull(time.Second)
	e.Wind(-10 * time.Second)
	out, _, _ := e.wound(t)
	out = append(out, e.pull(3*time.Second)...) // past where the decoder took over again
	for i, f := range out {
		if math.Abs(f[0]) > 1 || math.Abs(f[1]) > 1 {
			t.Fatalf("frame %d is %v: one of those the decoder gives right after a seek", i, f)
		}
	}
	if r.seeks < 10 {
		t.Errorf("the decoder was told to seek %d times to rewind 10 s", r.seeks)
	}
	last := out[len(out)-1]
	if at := int(math.Round(last[0] * float64(r.frames))); last != r.frame(at) {
		t.Errorf("the tape plays %v after the rewind, which is no frame of it", last)
	}
}

func TestADecoderThatSeeksRoughlyPlaysOnByItself(t *testing.T) {
	e, r := reelEngine(60*time.Second, true)
	e.Seek(30 * time.Second)
	e.pull(time.Second)
	e.Wind(-10 * time.Second)
	e.deck.tape.wind.exact = false // as ffmpeg, which starts at a packet near the frame
	e.wound(t)
	// What the winder read and what the decoder gives from where it seeks to
	// may not join, so nothing of the former is left to play.
	if n := len(e.deck.src.ahead); n > tapeBuf {
		t.Errorf("%d frames the winder read are left to play", n)
	}
	if got := e.Position(); !near(got, 21*time.Second, 2*readAhead) {
		t.Errorf("the head is at %v, want 21s", got)
	}
	after := e.pull(100 * time.Millisecond)
	at := int(math.Round(after[0][0] * float64(r.frames)))
	for i, f := range after {
		if f != r.frame(at+i) {
			t.Fatalf("frame %d after the rewind is %v, want %v", i, f, r.frame(at+i))
		}
	}

	// Forward it reads on from where it is, and the frames join.
	seeks := r.seeks
	e.Wind(10 * time.Second)
	e.deck.tape.wind.exact = false
	e.wound(t)
	if r.seeks != seeks {
		t.Errorf("the decoder was told to seek %d times to wind forward", r.seeks-seeks)
	}
}

func TestWindingAPausedDeckIsSilent(t *testing.T) {
	e, r := reelEngine(60*time.Second, false)
	if err := e.Wind(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	if e.deck.idle() {
		t.Fatal("the deck has nothing to do while the tape winds")
	}
	out, fastest, _ := e.wound(t)
	if fastest < 10 {
		t.Errorf("the tape wound at %v times the playing speed", fastest)
	}
	if level := rms(out); level != 0 {
		t.Errorf("the paused deck put out %v while winding", level)
	}
	if e.Speed() != 0 || !e.deck.idle() {
		t.Errorf("speed %v after the wind, idle %v", e.Speed(), e.deck.idle())
	}
	if got := e.Position(); !near(got, 10*time.Second, time.Millisecond) {
		t.Errorf("the head is at %v, want 10s", got)
	}
	e.SetPlaying(true)
	e.upToSpeed()
	if f := e.pull(10 * time.Millisecond)[0]; f != r.frame(SampleRate.N(10*time.Second)) {
		t.Errorf("the tape plays %v from where it was wound to", f)
	}
}

func TestWindingStopsAtTheEndsOfTheTape(t *testing.T) {
	e, _ := reelEngine(10*time.Second, true)
	e.pull(2 * time.Second)
	e.Wind(-25 * time.Second)
	e.wound(t)
	if got := e.Position(); got > 30*time.Millisecond {
		t.Errorf("rewound past the start, the head is at %v", got)
	}

	gen := e.deck.gen
	e.Wind(25 * time.Second)
	e.wound(t)
	e.pull(50 * time.Millisecond)
	if got := e.Position(); !near(got, 10*time.Second, time.Millisecond) {
		t.Errorf("wound to the end, the head is at %v", got)
	}
	if ev := nextEvent(t, e); ev.Gen != gen || ev.Err != nil {
		t.Errorf("event %+v at the end of the tape", ev)
	}
	if !e.Ended() {
		t.Error("the track is not over at the end of the tape")
	}
}

func TestWindingFindsTheEndOfATrackOfUnknownLength(t *testing.T) {
	r := &reel{frames: SampleRate.N(4 * time.Second)}
	src := &Source{stream: r, format: beep.Format{SampleRate: SampleRate, NumChannels: 2, Precision: 2}}
	e := newTestEngine()
	e.Start(src, true)
	e.pull(time.Second)
	e.Wind(20 * time.Second)
	e.wound(t)
	if got := e.Position(); !near(got, 4*time.Second, time.Millisecond) {
		t.Errorf("the head is at %v, want it at the end", got)
	}
}

func TestAKeyHeldDownWindsNoFurtherThanTheDeckWindsInAMoment(t *testing.T) {
	e, _ := reelEngine(10*time.Minute, true)
	e.pull(time.Second)
	from := e.Position()
	for range 100 { // three seconds of a key that repeats
		e.Wind(5 * time.Second)
		e.pull(30 * time.Millisecond)
	}
	held := e.Position() - from
	if held < 60*time.Second {
		t.Errorf("the tape wound %v while the key was held", held)
	}
	out, _, _ := e.wound(t)
	if coast := SampleRate.D(len(out)); coast > 1500*time.Millisecond {
		t.Errorf("the tape wound on for %v after the key was let go", coast)
	}

	// Taps add up.
	from = e.Position()
	for range 3 {
		e.Wind(5 * time.Second)
		e.pull(100 * time.Millisecond)
	}
	e.wound(t)
	if got := e.Position() - from; !near(got, 15*time.Second, 10*time.Millisecond) {
		t.Errorf("three taps wound the tape by %v", got)
	}
}

func TestBrakingStopsTheTapeAsSoonAsTheReelsSlowDown(t *testing.T) {
	e, _ := reelEngine(30*time.Minute, true)
	e.Seek(15 * time.Minute)
	e.pull(100 * time.Millisecond)
	for _, wind := range []struct {
		name string
		by   func(time.Duration) error
		step time.Duration
		top  float64
	}{
		{"Wind", e.Wind, 5 * time.Second, windSpeed},
		{"Wind", e.Wind, -5 * time.Second, windSpeed},
		{"WindFast", e.WindFast, 30 * time.Second, windFast},
		{"WindFast", e.WindFast, -30 * time.Second, windFast},
	} {
		step, accel := wind.step, windAccel*wind.top/windSpeed
		for range 30 { // a second of a key that repeats
			wind.by(step)
			e.pull(30 * time.Millisecond)
		}
		from, v := e.Position(), math.Abs(e.Speed())
		if v < wind.top {
			t.Fatalf("%s by %v: the tape winds at %v while the key is held", wind.name, step, e.Speed())
		}
		e.Brake()
		out, _, _ := e.wound(t)
		if coast, most := SampleRate.D(len(out)), time.Duration(v/accel*float64(time.Second)); coast > most+20*time.Millisecond {
			t.Errorf("%s by %v: the tape wound on for %v after the key was let go, want %v", wind.name, step, coast, most)
		}
		// From the speed of winding to that of playing on forward, and to
		// standing still before it plays on in reverse.
		on := 1.0
		if step < 0 {
			on = 0
		}
		want := time.Duration((v*v - on*on) / (2 * accel) * float64(time.Second))
		if step < 0 {
			want = -want
		}
		if got := e.Position() - from; !near(got, want, 50*time.Millisecond) {
			t.Errorf("%s by %v: the head went on by %v after the key was let go, want %v", wind.name, step, got, want)
		}
		if e.Speed() != 1 {
			t.Errorf("%s by %v: speed %v after braking", wind.name, step, e.Speed())
		}
	}
}

func TestAFastWindGetsUpToSpeedAsSoonAsAPlainOne(t *testing.T) {
	e, _ := reelEngine(10*time.Minute, true)
	upToSpeed := func(by func(time.Duration) error, top float64) time.Duration {
		t.Helper()
		var took time.Duration
		for ; e.Speed() < top; took += 10 * time.Millisecond {
			if took > time.Second {
				t.Fatalf("the tape winds at %v after %v, want %v", e.Speed(), took, top)
			}
			by(30 * time.Second)
			e.pull(10 * time.Millisecond)
		}
		return took
	}
	e.pull(100 * time.Millisecond)
	plain := upToSpeed(e.Wind, windSpeed)
	e.Seek(0)
	e.pull(100 * time.Millisecond)
	if fast := upToSpeed(e.WindFast, windFast); !near(fast, plain, 20*time.Millisecond) {
		t.Errorf("a fast wind got up to speed in %v, a plain one in %v", fast, plain)
	}

	// Let go of shift, and the key goes on to repeat as a plain one: the
	// tape slows down to the plain speed as fast as it got up to its own,
	// although the plain wind asks for less than it takes to slow down.
	accel := windAccel * windFast / windSpeed
	var took time.Duration
	for v := e.Speed(); v > windSpeed; took += 10 * time.Millisecond {
		if took > time.Second {
			t.Fatalf("the tape winds at %v after %v of a plain wind", v, took)
		}
		e.Wind(5 * time.Second)
		e.pull(10 * time.Millisecond)
		if dv, most := v-e.Speed(), accel*0.01; dv > most+1e-9 || dv < -1e-9 {
			t.Fatalf("the speed of the tape went from %v to %v in 10 ms", v, e.Speed())
		}
		v = e.Speed()
	}
	if most := time.Duration((windFast - windSpeed) / accel * float64(time.Second)); took > most+10*time.Millisecond {
		t.Errorf("the tape took %v to slow down to the plain speed, want %v", took, most)
	}
}

func TestAFastWindTakesTheTapeOffTheHead(t *testing.T) {
	for _, by := range []time.Duration{5 * time.Minute, -5 * time.Minute} {
		e, r := reelEngine(30*time.Minute, true)
		e.Seek(15 * time.Minute)
		r.rough = 1500
		e.pull(time.Second)
		from, read := e.Position(), r.read
		if err := e.WindFast(by); err != nil {
			t.Fatal(err)
		}
		out, fastest, slowest := e.wound(t)

		if v := max(fastest, -slowest); v != windFast {
			t.Errorf("winding by %v, the tape wound at %v at most", by, v)
		}
		want := from + by
		if got := e.Position(); !near(got, want, 20*time.Millisecond) {
			t.Errorf("winding by %v, the head is at %v, want %v", by, got, want)
		}
		// Off the head the tape is silent and the decoder is not read: the
		// music is heard only as the tape gets up to speed and slows down,
		// and then it is the music of the track.
		silent := 0
		for i, f := range out {
			if f == [2]float64{} {
				silent++
			}
			if math.Abs(f[0]) > 1 || math.Abs(f[1]) > 1 {
				t.Fatalf("winding by %v, frame %d is %v, which is not of the track", by, i, f)
			}
		}
		if share := float64(silent) / float64(len(out)); share < 0.5 {
			t.Errorf("winding by %v, %.0f%% of the wind is silent", by, share*100)
		}
		edge := SampleRate.N(20 * time.Millisecond)
		if rms(out[:edge]) == 0 || rms(out[len(out)-edge:]) == 0 {
			t.Errorf("winding by %v, the tape is not heard getting up to speed or slowing down", by)
		}
		if got := SampleRate.D(r.read - read); got > 20*time.Second {
			t.Errorf("winding by %v, the decoder was read for %v of the track", by, got)
		}
		after := e.pull(100 * time.Millisecond)
		at := int(math.Round(after[0][0] * float64(r.frames)))
		if got := SampleRate.D(at); !near(got, want, 20*time.Millisecond) {
			t.Errorf("winding by %v, the tape plays on from %v, want %v", by, got, want)
		}
		for i, f := range after {
			if f != r.frame(at+i) {
				t.Fatalf("winding by %v, frame %d after the wind is %v, want %v", by, i, f, r.frame(at+i))
			}
		}
	}
}

func TestWindingTurnsAround(t *testing.T) {
	e, _ := reelEngine(60*time.Second, true)
	e.Seek(30 * time.Second)
	e.pull(100 * time.Millisecond)
	from := e.Position()
	e.Wind(20 * time.Second)
	e.pull(300 * time.Millisecond)
	if e.Speed() < 10 {
		t.Fatalf("speed %v while winding forward", e.Speed())
	}
	e.Wind(-25 * time.Second)
	_, _, slowest := e.wound(t)
	if slowest > -5 {
		t.Errorf("the tape did not turn around: it wound at %v at least", slowest)
	}
	if got := e.Position(); !near(got, from-5*time.Second, 20*time.Millisecond) {
		t.Errorf("the head is at %v, want %v", got, from-5*time.Second)
	}
}

func TestSeekingBreaksOffAWind(t *testing.T) {
	e, r := reelEngine(60*time.Second, true)
	e.pull(time.Second)
	e.Wind(30 * time.Second)
	e.pull(200 * time.Millisecond)
	if err := e.Seek(40 * time.Second); err != nil {
		t.Fatal(err)
	}
	if e.Speed() != 1 || e.Position() != 40*time.Second {
		t.Fatalf("speed %v at %v after seeking", e.Speed(), e.Position())
	}
	if f := e.pull(10 * time.Millisecond)[0]; f != r.frame(SampleRate.N(40*time.Second)) {
		t.Errorf("the tape plays %v after seeking", f)
	}
}

func TestWindingAResampledTrack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tone.wav")
	writeWAV(t, path, 48000, 20*time.Second)
	src, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	e := newTestEngine()
	e.Start(src, true)
	e.pull(8 * time.Second)
	from := e.Position()
	e.Wind(-5 * time.Second)
	out, _, slowest := e.wound(t)
	if slowest > -10 || rms(out) == 0 {
		t.Errorf("the tape wound at %v at least, at a level of %v", slowest, rms(out))
	}
	if got := e.Position(); !near(got, from-5*time.Second, 40*time.Millisecond) {
		t.Errorf("the head is at %v, want %v", got, from-5*time.Second)
	}
	e.Wind(10 * time.Second)
	e.wound(t)
	if got := e.Position(); !near(got, from+5*time.Second, 60*time.Millisecond) {
		t.Errorf("the head is at %v, want %v", got, from+5*time.Second)
	}
	// It plays on at its pitch: 440 Hz crosses zero 880 times a second.
	after := e.pull(time.Second)
	crossings := 0
	for i := 1; i < len(after); i++ {
		if (after[i-1][0] < 0) != (after[i][0] < 0) {
			crossings++
		}
	}
	if crossings < 870 || crossings > 890 {
		t.Errorf("%d zero crossings in a second of 440 Hz after the wind", crossings)
	}
}

func TestATrackThatCannotSeekJumpsOrEnds(t *testing.T) {
	// Winding forward takes no seeking.
	e, r := reelEngine(60*time.Second, true)
	r.noSeek = true
	e.pull(time.Second)
	e.Wind(5 * time.Second)
	e.wound(t)
	if got := e.Position(); !near(got, 6*time.Second, readAhead) || e.Ended() {
		t.Errorf("the head is at %v, ended %v", got, e.Ended())
	}

	// Rewinding does, and a track that cannot be put anywhere is over.
	gen := e.deck.gen
	e.Wind(-5 * time.Second)
	e.wound(t)
	if ev := nextEvent(t, e); ev.Gen != gen || ev.Err == nil {
		t.Errorf("event %+v after a rewind that failed", ev)
	}
	if !e.Ended() || e.Speed() != 0 {
		t.Errorf("ended %v at a speed of %v after a rewind that failed", e.Ended(), e.Speed())
	}

	// A decoder that panics is one that fails.
	e, r = reelEngine(60*time.Second, true)
	r.panics = true
	gen = e.deck.gen
	e.pull(time.Second)
	e.Wind(-500 * time.Millisecond)
	e.wound(t)
	if ev := nextEvent(t, e); ev.Gen != gen || ev.Err == nil {
		t.Errorf("event %+v after a rewind that panicked", ev)
	}
}

// BenchmarkWind measures one second of output while the tape winds at full
// speed, without what the decoder takes.
func BenchmarkWind(b *testing.B) {
	for name, by := range map[string]time.Duration{"forward": time.Minute, "reverse": -time.Minute} {
		b.Run(name, func(b *testing.B) {
			e, _ := reelEngine(40*time.Hour, true)
			buf := make([][2]float64, 441)
			for b.Loop() {
				if p := e.Position(); p < time.Hour || p > 39*time.Hour {
					e.Seek(20 * time.Hour)
				}
				e.Wind(by)
				for range 100 {
					e.deck.Stream(buf)
				}
			}
		})
	}
}
