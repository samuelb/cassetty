package audio

import (
	"math"
	"strings"
)

// Tape is the cassette formulation a track plays from. cassetty picks it by
// the quality of the file: lossless files play from metal tape, the common
// lossy formats from chrome and everything else from normal tape. With the
// tape sound on, the formulation sets how much the tape hisses, how soon its
// treble fades and how early it saturates.
type Tape int

const (
	TapeNormal Tape = iota // type I, ferric oxide
	TapeChrome             // type II, chromium dioxide
	TapeMetal              // type IV, metal particle
)

// TapeFor returns the tape a file with the given codec plays from.
func TapeFor(codec string) Tape {
	switch codec {
	case "FLAC", "WAV", "ALAC", "AIFF", "AIF":
		return TapeMetal
	case "MP3", "OGG", "VORBIS", "OPUS":
		return TapeChrome
	}
	if strings.HasPrefix(codec, "PCM_") { // uncompressed audio decoded by ffmpeg, such as AIFF
		return TapeMetal
	}
	return TapeNormal
}

// String returns the tape type as printed on the cassette label.
func (t Tape) String() string {
	switch t {
	case TapeMetal:
		return "TYPE IV · METAL"
	case TapeChrome:
		return "TYPE II · CrO2"
	}
	return "TYPE I · NORMAL"
}

// Tape returns the tape the source plays from.
func (s *Source) Tape() Tape { return TapeFor(s.Codec) }

// Timing of the tape transport, in seconds.
const (
	tapeFade  = 0.15 // the tape sound fades in or out
	tapeStart = 0.3  // the motor gets up to speed
	tapeStop  = 0.5  // the tape winds down when it pauses
	tapeEase  = 0.05 // the sound moves to that of another formulation
)

// tapeSettle is the largest speed correction, in frames per frame, that
// moves the read position back onto the frames after the tape sound is off.
const tapeSettle = 0.0005

// Speed deviations of the transport, as fractions of the nominal speed.
const (
	wowDepth     = 0.0016 // slow wow from the capstan and the reels
	flutterDepth = 0.0004 // faster flutter from the pinch roller
	driftDepth   = 0.0008 // slow random wander of the motor speed
)

// wowFreqs are the frequencies of the two wow and the flutter oscillators,
// in Hz at full speed.
var wowFreqs = [3]float64{0.55, 0.87, 6.3}

const (
	tapeBuf    = 512                      // frames the transport reads from the deck at a time
	driftEvery = int(SampleRate) * 3 / 20 // frames between drift targets (150 ms)
	bumpGain   = 0.15                     // head bump: +1.2 dB in the low bass
)

var (
	driftGlide = onePole(0.7) // the drift glides towards its target
	easeCoef   = onePole(1 / (2 * math.Pi * tapeEase))
	bassCoef   = onePole(110)  // corner of the head bump
	hissCoef   = onePole(1500) // the hiss is high-passed here
)

// onePole returns the coefficient of a one-pole low-pass filter with the
// given corner frequency at the output sample rate.
func onePole(hz float64) float64 {
	return 1 - math.Exp(-2*math.Pi*hz/float64(SampleRate))
}

// tapeSound is the character of one formulation.
type tapeSound struct {
	hiss   float64 // amplitude of the hiss
	treble float64 // coefficient of the one-pole treble roll-off
	drive  float64 // saturation drive: higher saturates earlier
}

// hissNorm scales high-passed white noise in [-1, 1) to an RMS level of 1.
const hissNorm = 2.04

func newTapeSound(hissDB, cutoff, drive float64) tapeSound {
	return tapeSound{hiss: hissNorm * math.Pow(10, hissDB/20), treble: onePole(cutoff), drive: drive}
}

// tapeSounds holds the character of each formulation. The hiss levels are
// RMS levels in dBFS before the treble roll-off.
var tapeSounds = [...]tapeSound{
	TapeNormal: newTapeSound(-62, 9000, 1.1),
	TapeChrome: newTapeSound(-66, 12000, 0.85),
	TapeMetal:  newTapeSound(-70, 15000, 0.6),
}

// transport moves the decoded audio past the playback head. It reads the
// deck through a varispeed reader, so the tape can wow and flutter, wind up
// when it starts and wind down when it pauses, and it colors the sound like
// a cassette while the tape sound is on. With the tape sound off and the
// motor at full speed the frames pass through unchanged.
type transport struct {
	on    bool    // the tape sound is wanted
	mix   float64 // how much of the tape sound is heard; follows on
	motor float64 // tape speed, from 0 (standing still) to 1 (full speed)

	// The varispeed reader: frames read from the deck and the read
	// position in them. buf[0] is the frame before the first unread one,
	// which the interpolation needs.
	buf [tapeBuf + 2][2]float64
	n   int
	pos float64
	dry bool // the deck has run out of frames

	phase   [3]float64 // of the wow and flutter oscillators, in cycles
	drift   float64
	driftTo float64
	driftIn int // frames until the next drift target

	sound, want tapeSound // the character heard, easing towards want
	bass        [2]float64
	hissLow     [2]float64
	treble      [2]float64
	rng         uint64
}

func newTransport() *transport {
	t := &transport{rng: 0x9e3779b97f4a7c15, sound: tapeSounds[TapeNormal], want: tapeSounds[TapeNormal]}
	t.reset()
	return t
}

// reset drops the frames read ahead, after a seek or when a new track is
// loaded.
func (t *transport) reset() {
	t.buf[0] = [2]float64{}
	t.n, t.pos, t.dry = 1, 1, false
}

// load sets the formulation of the track that plays.
func (t *transport) load(tape Tape) {
	t.want = tapeSounds[tape]
}

// setOn turns the tape sound on or off. It fades in and out.
func (t *transport) setOn(on bool) {
	if on && t.mix == 0 {
		t.sound = t.want
		t.bass, t.hissLow, t.treble = [2]float64{}, [2]float64{}, [2]float64{}
	}
	t.on = on
}

// process fills dst with frames read from the deck through pull, and
// returns how many it filled: fewer than len(dst) means the deck has run
// out. While playing is false the motor winds down; the caller stops calling
// once the motor has come to a stop.
func (t *transport) process(dst [][2]float64, playing bool, pull func([][2]float64) int) int {
	const sr = float64(SampleRate)
	target := 0.0
	if playing {
		target = 1
	}
	for i := range dst {
		switch {
		case t.motor < target:
			t.motor = min(target, t.motor+1/(tapeStart*sr))
		case t.motor > target:
			t.motor = max(target, t.motor-1/(tapeStop*sr))
		}
		switch {
		case t.on && t.mix < 1:
			t.mix = min(1, t.mix+1/(tapeFade*sr))
		case !t.on && t.mix > 0:
			t.mix = max(0, t.mix-1/(tapeFade*sr))
		}

		x, ok := t.read(pull)
		if !ok {
			return i
		}
		speed := t.motor
		if t.motor < 1 {
			// The playback head puts out less the slower the tape moves.
			x[0] *= t.motor
			x[1] *= t.motor
		}
		if t.mix > 0 {
			speed *= 1 + t.mix*t.wow()
			x = t.color(x)
		}
		t.pos += speed
		if t.mix == 0 && t.motor == 1 {
			if d := math.Round(t.pos) - t.pos; d != 0 {
				// The wow left the read position between two frames: glide
				// back onto them, so they pass through unchanged again.
				t.pos += max(-tapeSettle, min(tapeSettle, d))
			}
		}
		dst[i] = x
	}
	return len(dst)
}

// read returns the frame at the read position, interpolated from the frames
// around it, and reads more from the deck when it gets to the end of buf.
// It reports false once the deck has run out of frames.
func (t *transport) read(pull func([][2]float64) int) ([2]float64, bool) {
	i := int(t.pos)
	if i+2 >= t.n {
		t.refill(pull)
		i = int(t.pos)
		if i+2 >= t.n {
			return [2]float64{}, false
		}
	}
	b := t.buf[i]
	f := t.pos - float64(i)
	if f == 0 {
		return b, true
	}
	a, c, d := t.buf[i-1], t.buf[i+1], t.buf[i+2]
	return [2]float64{hermite(a[0], b[0], c[0], d[0], f), hermite(a[1], b[1], c[1], d[1], f)}, true
}

// refill moves the frames still needed to the front of buf and reads more
// from the deck after them.
func (t *transport) refill(pull func([][2]float64) int) {
	keep := int(t.pos) - 1 // the frame before the read position is needed too
	t.n = copy(t.buf[:], t.buf[keep:t.n])
	t.pos -= float64(keep)
	if t.dry {
		return
	}
	t.n += pull(t.buf[t.n:tapeBuf])
	if t.n < tapeBuf {
		// The deck has run out. Two frames of silence after the last one
		// let the interpolation reach it.
		t.dry = true
		t.buf[t.n], t.buf[t.n+1] = [2]float64{}, [2]float64{}
		t.n += 2
	}
}

// hermite interpolates between x0 and x1 at the fraction f, on the 4-point,
// 3rd-order Hermite (Catmull-Rom) spline through xm1, x0, x1 and x2.
func hermite(xm1, x0, x1, x2, f float64) float64 {
	c1 := 0.5 * (x1 - xm1)
	c2 := xm1 - 2.5*x0 + 2*x1 - 0.5*x2
	c3 := 0.5*(x2-xm1) + 1.5*(x0-x1)
	return ((c3*f+c2)*f+c1)*f + x0
}

// wow returns the speed deviation of the transport at this frame and moves
// its oscillators on.
func (t *transport) wow() float64 {
	step := t.motor / float64(SampleRate) // the oscillators turn with the tape
	for k, f := range wowFreqs {
		if t.phase[k] += f * step; t.phase[k] >= 1 {
			t.phase[k]--
		}
	}
	if t.driftIn--; t.driftIn <= 0 {
		t.driftIn = driftEvery
		t.driftTo = driftDepth * t.noise()
	}
	t.drift += (t.driftTo - t.drift) * driftGlide
	return wowDepth*(0.6*math.Sin(2*math.Pi*t.phase[0])+0.4*math.Sin(2*math.Pi*t.phase[1])) +
		flutterDepth*math.Sin(2*math.Pi*t.phase[2]) + t.drift
}

// color gives the frame the sound of the tape: the head bump in the bass,
// saturation, hiss and the treble roll-off, mixed in by t.mix.
func (t *transport) color(x [2]float64) [2]float64 {
	s, w := &t.sound, &t.want
	s.hiss += (w.hiss - s.hiss) * easeCoef
	s.treble += (w.treble - s.treble) * easeCoef
	s.drive += (w.drive - s.drive) * easeCoef
	for c, v := range x {
		t.bass[c] += bassCoef * (v - t.bass[c])
		y := v + bumpGain*t.bass[c]
		y = math.Tanh(s.drive*y) / s.drive
		nz := t.noise()
		t.hissLow[c] += hissCoef * (nz - t.hissLow[c])
		y += (nz - t.hissLow[c]) * s.hiss * t.motor
		t.treble[c] += s.treble * (y - t.treble[c])
		x[c] = v + t.mix*(t.treble[c]-v)
	}
	return x
}

// noise returns white noise in [-1, 1) from a xorshift generator.
func (t *transport) noise() float64 {
	t.rng ^= t.rng << 13
	t.rng ^= t.rng >> 7
	t.rng ^= t.rng << 17
	return float64(int64(t.rng)) / (1 << 63)
}
