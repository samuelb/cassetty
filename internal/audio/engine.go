package audio

import (
	"encoding/binary"
	"math"
	"sync"
	"time"

	"github.com/ebitengine/oto/v3"
	"github.com/gopxl/beep/v2"
)

// Event is emitted when the currently loaded track has played to its end (or
// failed while streaming).
type Event struct {
	Gen uint64 // generation of the track that ended, see Engine.Start
	Err error
}

// idleSuspendDelay is how long the deck must have had nothing to play before
// the output is suspended. It outlasts the output latency, so by then only
// silence is queued in the device and resuming plays nothing stale.
const idleSuspendDelay = time.Second

// Engine owns the audio output and the deck that is permanently attached to
// it. All methods are safe for concurrent use.
type Engine struct {
	deck    *deck
	events  chan Event
	silent  bool
	latency int // output latency in frames
	player  *oto.Player
	null    *nullOutput
	done    chan struct{} // stops the idle watcher
}

// output is the part of the oto context that the idle watcher drives.
type output interface {
	Suspend() error
	Resume() error
}

// NewEngine initialises the audio output. When silent is true, or no audio
// device can be opened, the engine runs against a null output that consumes
// samples in real time; InitErr then reports why the device was not used.
func NewEngine(silent bool) (e *Engine, initErr error) {
	e = &Engine{events: make(chan Event, 8)}
	e.deck = newDeck(e.events)

	if !silent {
		const buffer = 100 * time.Millisecond
		ctx, ready, err := oto.NewContext(&oto.NewContextOptions{
			SampleRate:      int(SampleRate),
			ChannelCount:    2,
			Format:          oto.FormatFloat32LE,
			BufferSize:      buffer / 2,
			ApplicationName: "cassetty",
		})
		if err == nil {
			<-ready
			err = ctx.Err()
		}
		if err == nil {
			e.player = ctx.NewPlayer(&pcmReader{s: e.deck})
			e.player.SetBufferSize(SampleRate.N(buffer/2) * 8)
			e.player.Play()
			e.latency = SampleRate.N(buffer) * 3 / 4
			e.done = make(chan struct{})
			go e.deck.suspendWhenIdle(ctx, idleSuspendDelay, e.done)
			return e, nil
		}
		initErr = err
	}
	e.silent = true
	e.null = startNullOutput(e.deck)
	return e, initErr
}

// Silent reports whether the engine is running without a sound device.
func (e *Engine) Silent() bool { return e.silent }

// Close stops the output.
func (e *Engine) Close() {
	if e.done != nil {
		close(e.done)
	}
	if e.null != nil {
		e.null.stop()
	} else if e.player != nil {
		e.player.Close()
	}
	e.deck.mu.Lock()
	src := e.deck.src
	e.deck.src, e.deck.stream = nil, nil
	e.deck.mu.Unlock()
	src.Close()
}

// Events delivers end-of-track notifications.
func (e *Engine) Events() <-chan Event { return e.events }

// Start installs src as the current track, replacing (and closing) the
// previous one. It returns the generation number of the new track, which is
// carried by the matching end-of-track Event.
func (e *Engine) Start(src *Source, play bool) uint64 {
	d := e.deck
	d.mu.Lock()
	old := d.src
	d.gen++
	gen := d.gen
	d.src = src
	d.stream = wrapStream(src)
	d.setPlaying(play)
	if !play {
		d.tape.motor = 0 // a new tape does not wind down from where the old one was
	}
	d.ended = false
	d.tape.reset()
	if src != nil {
		d.tape.load(src.Tape())
	}
	d.mu.Unlock()
	d.changed()
	if old != nil && old != src {
		old.Close()
	}
	return gen
}

// Eject unloads the current track.
func (e *Engine) Eject() {
	d := e.deck
	d.mu.Lock()
	old := d.src
	d.gen++
	d.src, d.stream = nil, nil
	d.playing = false
	d.tape.motor = 0
	d.tape.reset()
	d.mu.Unlock()
	d.changed()
	old.Close()
}

// SetPlaying starts or pauses playback of the loaded track. With the tape
// sound on, the tape takes a moment to get up to speed and winds down when
// it pauses.
func (e *Engine) SetPlaying(play bool) {
	e.deck.mu.Lock()
	e.deck.setPlaying(play)
	e.deck.mu.Unlock()
	e.deck.changed()
}

// SetTape turns the tape sound on or off: wow and flutter, hiss, saturation
// and a softer treble, after the tape type of the track, and a tape that
// winds up when it starts and down when it pauses.
func (e *Engine) SetTape(on bool) {
	d := e.deck
	d.mu.Lock()
	d.tape.setOn(on)
	d.setPlaying(d.playing) // without the tape sound the motor no longer eases
	d.mu.Unlock()
	d.changed()
}

// Speed returns how fast the tape moves: 0 when it stands still, 1 when it
// plays, and in between while it winds up or down.
func (e *Engine) Speed() float64 {
	d := e.deck
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.idle() {
		return 0
	}
	return d.tape.motor
}

// Seek moves the play head of the loaded track to t.
func (e *Engine) Seek(t time.Duration) error {
	d := e.deck
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.src == nil {
		return errNoSource
	}
	n := d.src.format.SampleRate.N(t)
	if n < 0 {
		n = 0
	}
	if l := d.src.stream.Len(); l > 0 && n >= l {
		n = l - 1
	}
	if err := d.src.stream.Seek(n); err != nil {
		return err
	}
	// Recreate the resampler so no stale samples from before the seek play.
	d.stream = wrapStream(d.src)
	d.tape.reset()
	d.ended = false
	if !d.playing {
		d.tape.motor = 0 // the tape winding down must not play the new position
	}
	d.changed()
	return nil
}

// Position returns the play head position of the loaded track.
func (e *Engine) Position() time.Duration {
	d := e.deck
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.src == nil {
		return 0
	}
	return d.src.format.SampleRate.D(d.src.stream.Position())
}

// SetVolume sets the output volume; v is a linear slider value in [0, 1]
// that is mapped onto a perceptual (squared) gain curve.
func (e *Engine) SetVolume(v float64) {
	v = max(0, min(1, v))
	e.deck.mu.Lock()
	e.deck.gain = v * v
	e.deck.mu.Unlock()
}

// Snapshot copies the most recently played frames into dst, compensating
// for the output latency so the visualizers line up with what is audible.
func (e *Engine) Snapshot(dst [][2]float64) {
	e.deck.tap.read(dst, e.latency)
}

// wrapStream returns the source stream converted to the output sample rate.
func wrapStream(src *Source) beep.Streamer {
	if src == nil {
		return nil
	}
	if src.format.SampleRate == SampleRate {
		return src.stream
	}
	return beep.Resample(4, src.format.SampleRate, SampleRate, src.stream)
}

// deck is the single streamer attached to the output. It never drains: when
// nothing is playing it emits silence.
type deck struct {
	mu      sync.Mutex
	src     *Source
	stream  beep.Streamer
	gen     uint64
	playing bool
	ended   bool
	gain    float64
	events  chan<- Event
	tap     *tap
	wake    chan struct{} // signalled whenever idle may have changed
	tape    *transport
	pull    func([][2]float64) int // d.fill, bound once
}

func newDeck(events chan<- Event) *deck {
	d := &deck{gain: 1, events: events, tap: newTap(1 << 15), wake: make(chan struct{}, 1), tape: newTransport()}
	d.pull = d.fill
	return d
}

// idle reports whether the deck has nothing to play and emits silence.
// The caller must hold d.mu.
func (d *deck) idle() bool {
	return d.stream == nil || d.ended || (!d.playing && d.tape.motor == 0)
}

// setPlaying starts or stops the tape. With the tape sound on the motor
// eases up to speed and the tape winds down (see transport); otherwise both
// happen at once. The caller must hold d.mu.
func (d *deck) setPlaying(play bool) {
	d.playing = play
	if !d.tape.on {
		d.tape.motor = 0
		if play {
			d.tape.motor = 1
		}
	}
}

// changed tells the idle watcher that idle may have changed. It never
// blocks, so Stream can call it on the output's goroutine.
func (d *deck) changed() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

// suspendWhenIdle suspends out once the deck has had nothing to play for
// delay, so that while stopped, paused or ejected neither cassetty nor the
// system's audio server keeps rendering silence, and resumes it as soon as
// the deck has something to play again. It runs until done is closed.
//
// Stream must not suspend the output itself: the ALSA backend holds its
// lock while it pulls samples, and Suspend takes that lock.
func (d *deck) suspendWhenIdle(out output, delay time.Duration, done <-chan struct{}) {
	suspended := false
	timer := time.NewTimer(delay) // nothing is loaded yet
	defer timer.Stop()
	for {
		select {
		case <-done:
			return
		case <-d.wake:
			d.mu.Lock()
			idle := d.idle()
			d.mu.Unlock()
			if !idle {
				timer.Stop()
				if suspended {
					out.Resume()
					suspended = false
				}
			} else if !suspended {
				timer.Reset(delay)
			}
		case <-timer.C:
			d.mu.Lock()
			idle := d.idle()
			d.mu.Unlock()
			if idle && !suspended {
				out.Suspend()
				suspended = true
			}
		}
	}
}

func (d *deck) Stream(samples [][2]float64) (int, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.idle() {
		clear(samples)
		return len(samples), true
	}

	n := d.tape.process(samples, d.playing, d.pull)
	if n > 0 {
		d.tap.write(samples[:n])
		for i := range samples[:n] {
			samples[i][0] *= d.gain
			samples[i][1] *= d.gain
		}
	}
	clear(samples[n:])
	if !d.playing && d.tape.motor == 0 {
		d.changed() // the tape has wound down, so the output can be suspended
	}
	return len(samples), true
}

// fill streams the loaded track into samples and returns how many frames it
// filled; fewer than len(samples) means the track has ended. The caller must
// hold d.mu.
func (d *deck) fill(samples [][2]float64) int {
	n := 0
	for n < len(samples) && !d.ended {
		k, ok := d.stream.Stream(samples[n:])
		n += k
		if ok && k > 0 {
			continue // a short read is followed by (0, false) at the end
		}
		d.ended = true
		d.changed()
		err := d.stream.Err()
		if err == nil && d.src != nil {
			err = d.src.stream.Err()
		}
		select {
		case d.events <- Event{Gen: d.gen, Err: err}:
		default:
		}
	}
	return n
}

func (d *deck) Err() error { return nil }

// pcmReader adapts a streamer to the float32 little-endian stereo byte
// stream consumed by oto.
type pcmReader struct {
	s   beep.Streamer
	buf [][2]float64
}

func (r *pcmReader) Read(p []byte) (int, error) {
	n := len(p) / 8
	if n == 0 {
		return 0, nil
	}
	if len(r.buf) < n {
		r.buf = make([][2]float64, n)
	}
	r.s.Stream(r.buf[:n])
	for i, f := range r.buf[:n] {
		binary.LittleEndian.PutUint32(p[i*8:], math.Float32bits(float32(max(-1, min(1, f[0])))))
		binary.LittleEndian.PutUint32(p[i*8+4:], math.Float32bits(float32(max(-1, min(1, f[1])))))
	}
	return n * 8, nil
}

// nullOutput pulls samples from a streamer in real time without playing
// them, for machines without a sound device (and for testing).
type nullOutput struct {
	done chan struct{}
}

func startNullOutput(s beep.Streamer) *nullOutput {
	o := &nullOutput{done: make(chan struct{})}
	go func() {
		t := time.NewTicker(10 * time.Millisecond)
		defer t.Stop()
		buf := make([][2]float64, 4096)
		last := time.Now()
		for {
			select {
			case <-o.done:
				return
			case now := <-t.C:
				n := SampleRate.N(now.Sub(last))
				last = now
				for n > 0 {
					k := min(n, len(buf))
					s.Stream(buf[:k])
					n -= k
				}
			}
		}
	}()
	return o
}

func (o *nullOutput) stop() { close(o.done) }

// tap is a ring buffer of the most recently played (pre-volume) frames.
type tap struct {
	mu  sync.Mutex
	buf [][2]float64
	w   int // total frames written
}

func newTap(size int) *tap { return &tap{buf: make([][2]float64, size)} }

func (t *tap) write(s [][2]float64) {
	t.mu.Lock()
	for _, f := range s {
		t.buf[t.w%len(t.buf)] = f
		t.w++
	}
	t.mu.Unlock()
}

func (t *tap) read(dst [][2]float64, delay int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	end := t.w - delay
	for i := range dst {
		idx := end - len(dst) + i
		if idx < 0 || idx < t.w-len(t.buf) {
			dst[i] = [2]float64{}
			continue
		}
		dst[i] = t.buf[idx%len(t.buf)]
	}
}
