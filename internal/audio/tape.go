package audio

import (
	"math"
	"strings"
)

// Tape is the cassette formulation a track plays from. cassetty picks it by
// the quality of the file: lossless files play from metal tape, the common
// lossy formats from chrome and everything else from normal tape. It is
// printed on the cassette label.
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
	tapeStart = 0.25 // the motor gets up to speed
	tapeStop  = 0.25 // the tape winds down when it pauses or stops
)

// The motor gains and loses speed by these steps per frame, to get up to
// speed in tapeStart and to wind down in tapeStop.
const (
	startStep = 1 / (tapeStart * float64(SampleRate))
	stopStep  = 1 / (tapeStop * float64(SampleRate))
)

// tapeSettle is the largest speed correction, in frames per frame, that
// moves the read position back onto the frames once the motor is up to
// speed.
const tapeSettle = 0.0005

const tapeBuf = 512 // frames the transport reads from the deck at a time

// transport moves the decoded audio past the playback head. It reads the
// deck through a varispeed reader, so the tape can wind up when it starts
// and wind down when it pauses. With the motor at full speed the frames
// pass through unchanged.
type transport struct {
	// motor is the tape speed, from 0 (standing still) to 1 (full speed).
	// While the tape winds it is the speed of that: many times the full
	// speed, and negative in reverse.
	motor float64
	// wind runs the tape while it winds fast forward or back, in place of
	// the varispeed reader; nil otherwise.
	wind *winder

	// The varispeed reader: frames read from the deck and the read
	// position in them. buf[0] is the frame before the first unread one,
	// which the interpolation needs.
	buf [tapeBuf + 2][2]float64
	n   int
	pos float64
	dry bool // the deck has run out of frames
}

func newTransport() *transport {
	t := &transport{}
	t.reset()
	return t
}

// reset drops the frames read ahead, after a seek or when a new track is
// loaded.
func (t *transport) reset() {
	t.buf[0] = [2]float64{}
	t.n, t.pos, t.dry = 1, 1, false
}

// unread returns the frames read from the deck that the head has not got to
// yet.
func (t *transport) unread() [][2]float64 {
	n := t.n
	if t.dry {
		n -= 2 // the silence after the last frame
	}
	i := int(math.Ceil(t.pos))
	if i >= n {
		return nil
	}
	return t.buf[i:n]
}

// process fills dst with frames read from the deck through pull, and
// returns how many it filled: fewer than len(dst) means the deck has run
// out, or that the tape has wound to where it was to go (see winder), which
// the caller is to end. While playing is false the motor winds down; the
// caller stops calling once the motor has come to a stop.
func (t *transport) process(dst [][2]float64, playing bool, pull func([][2]float64) int) int {
	target := 0.0
	if playing {
		target = 1
	}
	if t.wind != nil {
		t.wind.prepare()
	}
	for i := range dst {
		if w := t.wind; w != nil {
			if w.done {
				return i
			}
			dst[i] = w.next(playing)
			t.motor = w.speed
			continue
		}
		switch {
		case t.motor < target:
			t.motor = min(target, t.motor+startStep)
		case t.motor > target:
			t.motor = max(target, t.motor-stopStep)
		}

		x, ok := t.read(pull)
		if !ok {
			return i
		}
		if t.motor < 1 {
			// The playback head puts out less the slower the tape moves.
			x[0] *= t.motor
			x[1] *= t.motor
		}
		t.pos += t.motor
		if t.motor == 1 {
			if d := math.Round(t.pos) - t.pos; d != 0 {
				// Getting up to speed left the read position between two
				// frames: glide back onto them, so they pass through
				// unchanged again.
				t.pos += max(-tapeSettle, min(tapeSettle, d))
			}
		}
		dst[i] = x
	}
	return len(dst)
}

// stopping returns how many frames the motor takes to wind down to a stop
// from the speed it is at.
func (t *transport) stopping() int {
	return int(math.Ceil(t.motor/stopStep)) + 1 // and one for the rounding
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
