package audio

import (
	"math"
	"time"
)

// Fast forward and rewind move the play head the way a deck does: the tape
// runs past the head at many times the playing speed, forward or in reverse,
// and while the deck plays the head stays on it, so the music is heard
// racing by, high and garbled ("cue and review"). The reels take a moment to
// get up to speed and to slow down again, which is heard as well.
const (
	windSpeed   = 32.0 // the fastest the tape winds, in multiples of the playing speed
	windAccel   = 60.0 // how fast the reels gain and lose speed, in such multiples per second
	windSlowest = 4.0  // the tape winds at least this fast, whatever the decoder takes
	windLoad    = 0.5  // the share of the time that decoding may take while winding
	windMute    = 0.4  // the head puts out less the faster the tape winds: by speed^-windMute
	cueFade     = 0.05 // seconds in which the head comes on or off the tape

	// windReach is how far the head may be asked to go, in seconds of the
	// track. A key that is held asks for more with every repeat, and the
	// tape should stop soon after it is released.
	windReach = 30.0
	// windLead is how long before the decoder is read that it is moved,
	// see winder.prepare.
	windLead = 0.15

	windRead    = 8192                // frames read at a time when winding forward
	windBack    = 2 * 8192            // and in reverse, where every read takes a seek
	windKeep    = 4096                // frames kept behind the head when winding forward
	windBackFar = 4 * int(SampleRate) // as windBack, from a decoder that takes long to seek
	// windRoll is how many frames before the ones wanted the decoder is
	// moved to and reads first: the first frames an MP3 decoder gives
	// after a seek are not those of the track, as they depend on the ones
	// before. In reverse, where the chunks are a fraction of a second, that
	// would be heard as a rattle.
	windRoll = 2304
)

// winder runs the tape of one track past the head at the speed of a fast
// forward or a rewind, until the head is at the target. It reads the decoder
// of the track itself, in the frames of the track: where the head moves in
// reverse the decoder cannot follow, so the winder keeps the frames around
// the head and reads those before them in chunks.
type winder struct {
	src  *Source
	rate float64 // frames of the track that pass the head per frame put out, at playing speed

	// buf holds the frames start to start+len(buf) of the track.
	buf   [][2]float64
	start int
	at    int // the frame the decoder reads next
	end   int // the number of frames of the track, -1 until it is known
	chunk int // frames read at a time in reverse
	// exact tells that the decoder seeks to the very frame it is asked for,
	// so that what it reads from there joins on to the frames before.
	// ffmpeg starts at a packet near it.
	exact bool

	pos     float64 // the head, in frames of the track
	target  float64 // where it is to go
	speed   float64 // of the tape, in multiples of the playing speed; negative in reverse
	cue     float64 // how much the head picks up, from 0 (off the tape) to 1
	arrived bool    // the head is at the target and the tape eases to rest
	done    bool    // the wind is over: the deck takes the tape back, see deck.land
	err     error   // of the decoder; ends the wind

	cost float64 // seconds the decoder takes per second of the track, averaged
}

// newWinder returns a winder for src with the head where the source is read
// next, the tape moving at speed.
func newWinder(src *Source, speed float64, playing bool) *winder {
	w := &winder{
		src:   src,
		rate:  float64(src.format.SampleRate) / float64(SampleRate),
		end:   -1,
		chunk: windBack,
		exact: true,
		speed: speed,
	}
	if _, ok := src.stream.(*ffmpegStream); ok {
		w.chunk, w.exact = windBackFar, false
	}
	if playing {
		w.cue = 1
	}
	if src.Length > 0 {
		w.end = src.stream.Len()
	}
	// The frames the source has read ahead are the first ones under the head.
	w.start = src.position()
	w.at = w.start + len(src.ahead)
	w.buf = make([][2]float64, len(src.ahead), max(len(src.ahead), 2*w.chunk+windRead+windKeep))
	copy(w.buf, src.ahead)
	src.ahead = nil
	w.pos = float64(w.start)
	w.target = w.pos
	return w
}

// aim moves the target by d.
func (w *winder) aim(d time.Duration) {
	perSec := w.rate * float64(SampleRate)
	by := d.Seconds() * perSec
	reach := max(windReach*perSec, 2*math.Abs(by))
	t := max(w.pos-reach, min(w.pos+reach, w.target+by))
	if w.end >= 0 {
		t = min(t, float64(w.end))
	}
	w.target = max(0, t)
	w.arrived = false
}

// top returns the fastest the tape may wind for now: slower than windSpeed
// when the decoder does not keep up with that.
func (w *winder) top() float64 {
	if w.cost*windSpeed <= windLoad {
		return windSpeed
	}
	return max(windSlowest, windLoad/w.cost)
}

// slew moves v towards to by step at most.
func slew(v, to, step float64) float64 {
	if v < to {
		return min(to, v+step)
	}
	return max(to, v-step)
}

// next returns the frame under the head and moves the tape on by one frame
// of output. The head is on the tape while the deck plays.
func (w *winder) next(playing bool) [2]float64 {
	const sr = float64(SampleRate)
	rest := 0.0 // the speed of the tape once the wind is over
	if playing {
		rest = 1
	}
	w.cue = slew(w.cue, rest, 1/(cueFade*sr))

	left := w.target - w.pos
	if !w.arrived {
		dir := 1.0
		if left < 0 {
			dir = -1
		}
		v := slew(w.speed, dir*w.top(), windAccel/sr)
		if v*dir > 0 {
			// Slow down in time to be at the target at the speed the tape
			// goes on at from there, or standing when it turns around.
			on := 0.0
			if dir > 0 {
				on = rest
			}
			if brake := math.Sqrt(on*on + 2*windAccel*math.Abs(left)/(w.rate*sr)); math.Abs(v) > brake {
				v = dir * brake
			}
			w.arrived = math.Abs(left) <= max(1, math.Abs(v)*w.rate) // there within this frame
			if w.arrived && !w.exact && w.at != w.start+len(w.buf) {
				w.move(int(w.target)) // it plays on from the decoder, see release
			}
		}
		w.speed = v
	}
	if w.arrived {
		w.speed = slew(w.speed, rest, windAccel/sr)
		if w.speed == rest {
			w.done = true
		}
	}

	// The head reads what passes it during the frame, and never less than
	// one frame of the track: at the playing speed these are the frames as
	// they are, and a tape that hardly moves is interpolated.
	step := w.speed * w.rate
	width := max(1, math.Abs(step))
	from := w.pos
	if step < 0 {
		from -= width
	}
	x := w.mean(from, from+width)
	w.pos = max(0, w.pos+step)

	level := w.level()
	x[0] *= level
	x[1] *= level
	return x
}

// level returns how much the head puts out: less the slower the tape moves,
// as when it winds down, and less again the faster it winds, where the
// music is moved up to where the ear is most sensitive.
func (w *winder) level() float64 {
	v := math.Abs(w.speed)
	if v <= 1 {
		return w.cue * v
	}
	return w.cue * math.Pow(v, -windMute)
}

// mean returns the mean of the track between the frame positions a and b.
// What is off the tape is silent.
func (w *winder) mean(a, b float64) [2]float64 {
	lo, hi := int(math.Floor(a)), int(math.Ceil(b))
	w.cover(lo, hi)
	var sum [2]float64
	for i := max(lo, w.start); i < min(hi, w.start+len(w.buf)); i++ {
		f := w.buf[i-w.start]
		part := min(b, float64(i+1)) - max(a, float64(i))
		sum[0] += f[0] * part
		sum[1] += f[1] * part
	}
	return [2]float64{sum[0] / (b - a), sum[1] / (b - a)}
}

// cover reads the frames lo to hi of the track into buf, as far as the track
// has them.
func (w *winder) cover(lo, hi int) {
	if w.err != nil {
		return
	}
	if lo < w.start && w.start > 0 {
		w.back()
	}
	for w.err == nil && hi > w.start+len(w.buf) && w.forth() {
	}
}

// forth reads the frames after those in buf, and reports whether there were
// any.
func (w *winder) forth() bool {
	end := w.start + len(w.buf)
	n := windRead
	if w.end >= 0 {
		n = min(n, w.end-end)
	}
	if n <= 0 {
		return false
	}
	if len(w.buf)+n > cap(w.buf) {
		// Make room: only the frames just behind the head are still needed.
		drop := min(len(w.buf), max(0, int(w.pos)-windKeep-w.start))
		drop = max(drop, len(w.buf)+n-cap(w.buf))
		w.buf = w.buf[:copy(w.buf, w.buf[drop:])]
		w.start += drop
	}
	began := time.Now()
	w.seek(end)
	got := w.read(w.buf[len(w.buf) : len(w.buf)+n])
	w.took(time.Since(began), got)
	w.buf = w.buf[:len(w.buf)+got]
	if got < n && w.err == nil {
		// The track is over, sooner than it said or without having said.
		w.end = end + got
		w.target = min(w.target, float64(w.end))
	}
	return got > 0
}

// back reads the chunk of frames before those in buf.
func (w *winder) back() {
	n := min(w.chunk, w.start)
	keep := min(len(w.buf), cap(w.buf)-n) // the frames furthest ahead of the head make room
	w.buf = w.buf[:n+keep]
	copy(w.buf[n:], w.buf[:keep])
	began := time.Now()
	w.seek(w.start - n)
	got := w.read(w.buf[:n])
	w.took(time.Since(began), got)
	clear(w.buf[got:n]) // what a damaged file does not give is silent
	w.start -= n
}

// prepare moves the decoder to where it is read next shortly before it is:
// a decoder in a process of its own, as ffmpeg is, takes a moment to get
// there, in which the output would have nothing to play.
func (w *winder) prepare() {
	if w.err != nil || w.done {
		return
	}
	lead := math.Abs(w.speed) * w.rate * windLead * float64(SampleRate)
	end := w.start + len(w.buf)
	switch {
	case w.speed < 0 && w.start > 0 && w.pos-float64(w.start) < lead:
		w.move(w.start - min(w.chunk, w.start))
	case w.speed > 0 && float64(end)-w.pos < lead && (w.end < 0 || end < w.end):
		w.move(end)
	}
}

// took takes note of how long the decoder took to read frames of the track.
func (w *winder) took(d time.Duration, frames int) {
	if frames >= windRead/2 { // a few frames at the end of the track tell little
		cost := d.Seconds() * w.rate * float64(SampleRate) / float64(frames)
		w.cost += (cost - w.cost) / 4
	}
}

// seek has the decoder read frame n of the track next.
func (w *winder) seek(n int) {
	w.move(n)
	var skipped [512][2]float64
	for w.err == nil && w.at < n {
		if w.read(skipped[:min(len(skipped), n-w.at)]) == 0 {
			break // the track is over before n
		}
	}
}

// move moves the decoder to where it gets to frame n of the track by
// reading on: windRoll frames before it, unless it is no further from it
// already.
func (w *winder) move(n int) {
	from := max(0, n-windRoll)
	if w.err != nil || (w.at >= from && w.at <= n) {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			w.fail(damaged(r))
		}
	}()
	if err := w.src.stream.Seek(from); err != nil {
		w.fail(err)
		return
	}
	w.at = from
}

// read fills dst from the decoder and returns how many frames it read: fewer
// than len(dst) at the end of the track. The decoders may panic on a damaged
// file, as in deck.read.
func (w *winder) read(dst [][2]float64) (n int) {
	if w.err != nil {
		return 0
	}
	defer func() {
		if r := recover(); r != nil {
			w.fail(damaged(r))
		}
		w.at += n
	}()
	for n < len(dst) {
		k, ok := w.src.stream.Stream(dst[n:])
		n += k
		if !ok || k == 0 {
			break
		}
	}
	return n
}

// fail ends the wind over an error of the decoder.
func (w *winder) fail(err error) {
	w.err, w.done = err, true
}

// release ends the wind: it leaves the source to be read from where the head
// is. An error means the source is not there.
func (w *winder) release() error {
	end := w.start + len(w.buf)
	head := max(w.start, min(end, int(math.Ceil(w.pos))))
	if w.exact && (w.end < 0 || end < w.end) {
		w.seek(end) // after a rewind: the frames up to there are read already
	}
	switch {
	case w.err != nil:
	case w.at == end:
		w.src.ahead = w.buf[head-w.start:]
		return nil
	default:
		// The decoder does not join on to the frames at hand, so it plays
		// on from the head itself.
		if w.seek(head); w.err == nil && w.at == head {
			return nil
		}
	}
	return w.src.seekFrame(int(w.target)) // the wind broke off: jump there
}
