package audio

import (
	"encoding/binary"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ebitengine/oto/v3"
	"github.com/gopxl/beep/v2"
)

// Event is emitted when the currently loaded track has played to its end (or
// failed while streaming).
type Event struct {
	Gen uint64 // generation of the track that ended, see Engine.Start
	// Next is the generation of the queued track the deck rolled on to
	// without a gap, see Engine.Queue; 0 when the deck stopped.
	Next uint64
	Err  error
}

// idleSuspendDelay is how long the deck must have had nothing to play before
// the output is suspended. It outlasts the output latency, so by then only
// silence is queued in the device and resuming plays nothing stale.
const idleSuspendDelay = time.Second

// Engine owns the audio output and the deck that is permanently attached to
// it. All methods are safe for concurrent use.
//
// Position, Speed, Winding, Ended, SetVolume and Snapshot return at once,
// also while the output is held up reading a track from a disk that takes
// its time. The methods that change what plays wait for such a read to
// return.
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

// closeTimeout is how long Close waits for the output to let go of the deck.
const closeTimeout = 500 * time.Millisecond

// Close stops the output. An output that is held up reading from a disk is
// left behind after closeTimeout: Close is called when cassetty quits, which
// must not take as long as the disk does.
func (e *Engine) Close() {
	done := make(chan struct{})
	go func() {
		e.close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(closeTimeout):
	}
}

func (e *Engine) close() {
	if e.done != nil {
		close(e.done)
	}
	if e.null != nil {
		e.null.stop()
	} else if e.player != nil {
		e.player.Pause()
	}
	e.deck.mu.Lock()
	src, next := e.deck.src, e.deck.next
	e.deck.src, e.deck.stream = nil, nil
	e.deck.next, e.deck.nextStream = nil, nil
	e.deck.publish()
	e.deck.mu.Unlock()
	src.Close()
	next.Close()
}

// Events delivers end-of-track notifications.
func (e *Engine) Events() <-chan Event { return e.events }

// Start installs src as the current track, replacing (and closing) the
// previous one and clearing the queue. It returns the generation number of
// the new track, which is carried by the matching end-of-track Event.
func (e *Engine) Start(src *Source, play bool) uint64 {
	d := e.deck
	stream := wrapStream(src) // before locking: it panics on a source nothing plays from
	d.mu.Lock()
	old, queued := d.src, d.next
	d.seq++
	d.gen = d.seq
	gen := d.gen
	d.src = src
	d.stream = stream
	d.next, d.nextStream, d.nextGen = nil, nil, 0
	d.stopWind()
	d.setPlaying(play)
	if !play {
		d.tape.motor = 0 // a new tape does not wind down from where the old one was
	}
	d.ended = false
	d.tape.reset()
	if src != nil {
		d.tape.load(src.Tape())
	}
	d.publish()
	d.mu.Unlock()
	d.changed()
	if old != src {
		old.Close()
	}
	if queued != src {
		queued.Close()
	}
	return gen
}

// Queue sets src to follow the loaded track without a gap: when the loaded
// track plays to its end, the deck rolls straight on to src, and the Event
// for the track that ended carries the generation of src in Next. Queue
// replaces (and closes) any track queued before, and nil clears the queue.
// It returns the generation src will play as.
func (e *Engine) Queue(src *Source) uint64 {
	d := e.deck
	stream := wrapStream(src) // before locking, as in Start
	d.mu.Lock()
	old := d.next
	d.next, d.nextStream, d.nextGen = src, stream, 0
	if src != nil {
		d.seq++
		d.nextGen = d.seq
	}
	gen := d.nextGen
	d.mu.Unlock()
	if old != src {
		old.Close()
	}
	return gen
}

// Eject unloads the current track and clears the queue.
func (e *Engine) Eject() {
	d := e.deck
	d.mu.Lock()
	old, queued := d.src, d.next
	d.seq++
	d.gen = d.seq
	d.src, d.stream = nil, nil
	d.next, d.nextStream, d.nextGen = nil, nil, 0
	d.playing = false
	d.tape.wind = nil
	d.tape.motor = 0
	d.tape.reset()
	d.publish()
	d.mu.Unlock()
	d.changed()
	old.Close()
	queued.Close()
}

// SetPlaying starts or pauses playback of the loaded track. With the tape
// sound on, the tape takes a moment to get up to speed and winds down when
// it pauses.
func (e *Engine) SetPlaying(play bool) {
	e.deck.mu.Lock()
	e.deck.setPlaying(play)
	e.deck.publish()
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
	d.publish()
	d.mu.Unlock()
	d.changed()
}

// Speed returns how fast the tape moves: 0 when it stands still, 1 when it
// plays, and in between while it winds up or down. While it winds fast
// forward the speed is many times that of playing, and while it rewinds it
// is negative, see Wind.
func (e *Engine) Speed() float64 {
	return math.Float64frombits(e.deck.speed.Load())
}

// Ended reports whether the loaded track has played to its end, with no
// queued track to roll on to: the deck stands still until it is told to
// seek or to start another track.
func (e *Engine) Ended() bool { return e.deck.over.Load() }

// Seek moves the play head of the loaded track to t at once, as no tape
// could: Wind is how a deck gets there.
func (e *Engine) Seek(t time.Duration) error {
	d := e.deck
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.src == nil {
		return errNoSource
	}
	d.stopWind()
	if err := d.src.seek(t); err != nil {
		return err
	}
	// Recreate the resampler so no stale samples from before the seek play.
	d.stream = wrapStream(d.src)
	d.tape.reset()
	d.ended = false
	if !d.playing {
		d.tape.motor = 0 // the tape winding down must not play the new position
	}
	d.publish()
	d.changed()
	return nil
}

// Wind moves the play head of the loaded track on by the given time, or
// back by a negative one, the way a deck does: the tape runs fast forward
// or back until the head is there, and while the deck plays the music is
// heard racing by. Winding again before the head is there adds to how far
// it goes; a deck that is asked for much more than it winds in a moment
// gets less far, so that a key held down winds the tape for as long as it
// is held. Position and Speed tell where the head is meanwhile and how fast
// the tape moves.
func (e *Engine) Wind(by time.Duration) error {
	d := e.deck
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.src == nil {
		return errNoSource
	}
	if d.tape.wind == nil {
		if d.src.format.SampleRate == SampleRate {
			// What the transport has read ahead are frames of the track,
			// but for those of the track before it that still play: the
			// tape winds on from the very frame the head is at.
			d.src.putBack(d.tape.unread())
		}
		d.tape.reset()
		d.tape.wind = newWinder(d.src, d.tape.motor, d.playing)
	}
	d.tape.wind.aim(by)
	d.ended = false
	d.publish()
	d.changed()
	return nil
}

// Winding reports whether the tape is being wound, see Wind: from when it is
// asked to until the head is where it was to go.
func (e *Engine) Winding() bool { return e.deck.winds.Load() }

// Position returns the play head position of the loaded track.
func (e *Engine) Position() time.Duration {
	return time.Duration(e.deck.pos.Load())
}

// SetVolume sets the output volume; v is a linear slider value in [0, 1]
// that is mapped onto a perceptual (squared) gain curve.
func (e *Engine) SetVolume(v float64) {
	v = max(0, min(1, v))
	e.deck.gain.Store(math.Float64bits(v * v))
}

// Snapshot copies the most recently played frames into dst, compensating
// for the output latency so the visualizers line up with what is audible.
func (e *Engine) Snapshot(dst [][2]float64) {
	e.deck.tap.read(dst, e.latency)
}

// seek moves the decoder to t.
func (s *Source) seek(t time.Duration) error {
	return s.seekFrame(s.format.SampleRate.N(t))
}

// seekFrame moves the decoder to frame n of the source. A decoder that
// panics, as some do on a damaged file, fails to seek.
func (s *Source) seekFrame(n int) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = damaged(r)
		}
	}()
	s.ahead = nil
	if n < 0 {
		n = 0
	}
	if l := s.stream.Len(); l > 0 && n >= l {
		n = l - 1
	}
	return s.stream.Seek(n)
}

// wrapStream returns the source stream converted to the output sample rate.
func wrapStream(src *Source) beep.Streamer {
	if src == nil {
		return nil
	}
	if src.format.SampleRate == SampleRate {
		return reader{src}
	}
	return beep.Resample(4, src.format.SampleRate, SampleRate, reader{src})
}

// deck is the single streamer attached to the output. It never drains: when
// nothing is playing it emits silence.
type deck struct {
	mu      sync.Mutex
	src     *Source
	stream  beep.Streamer
	gen     uint64
	seq     uint64 // the last generation handed out
	playing bool
	ended   bool
	events  chan<- Event
	tap     *tap
	wake    chan struct{} // signalled whenever idle may have changed
	tape    *transport
	pull    func([][2]float64) int // d.fill, bound once

	// the queued track, which follows the loaded one without a gap
	next       *Source
	nextStream beep.Streamer
	nextGen    uint64

	// What the player reads on every frame it draws is kept outside mu,
	// which the output holds while it reads from the decoder: on a disk
	// that spins up or a network share that hangs, such a read takes
	// seconds, and the player must not stand still for it. See publish.
	pos   atomic.Int64  // play head of the loaded track, a time.Duration
	speed atomic.Uint64 // tape speed, a float64, see Engine.Speed
	winds atomic.Bool   // see Engine.Winding
	over  atomic.Bool   // see Engine.Ended
	gain  atomic.Uint64 // output gain, a float64
}

func newDeck(events chan<- Event) *deck {
	d := &deck{events: events, tap: newTap(1 << 15), wake: make(chan struct{}, 1), tape: newTransport()}
	d.gain.Store(math.Float64bits(1))
	d.pull = d.fill
	return d
}

// publish makes the play head, the tape speed and the end of the track
// known to the methods of Engine that do not wait for d.mu. Whatever
// changes one of them calls it; the caller must hold d.mu.
func (d *deck) publish() {
	var pos time.Duration
	switch {
	case d.src == nil:
	case d.tape.wind != nil:
		pos = d.src.format.SampleRate.D(int(d.tape.wind.pos))
	default:
		pos = d.src.format.SampleRate.D(d.src.position())
	}
	d.pos.Store(int64(pos))
	speed := 0.0
	if !d.idle() {
		speed = d.tape.motor
	}
	d.speed.Store(math.Float64bits(speed))
	d.winds.Store(d.tape.wind != nil)
	d.over.Store(d.stream != nil && d.ended)
}

// idle reports whether the deck has nothing to play and emits silence.
// The caller must hold d.mu.
func (d *deck) idle() bool {
	return d.stream == nil || d.ended || (!d.playing && d.tape.motor == 0 && d.tape.wind == nil)
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
				// An output that fails to resume is asked again the next
				// time something is to play.
				if suspended && out.Resume() == nil {
					suspended = false
				}
			} else if !suspended {
				timer.Reset(delay)
			}
		case <-timer.C:
			d.mu.Lock()
			idle := d.idle()
			d.mu.Unlock()
			if idle && !suspended && out.Suspend() == nil {
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
	if w := d.tape.wind; w != nil && w.done {
		d.land()
		if !d.idle() {
			n += d.tape.process(samples[n:], d.playing, d.pull)
		}
	}
	if n > 0 {
		d.tap.write(samples[:n])
		gain := math.Float64frombits(d.gain.Load())
		for i := range samples[:n] {
			samples[i][0] *= gain
			samples[i][1] *= gain
		}
	}
	clear(samples[n:])
	d.publish()
	if !d.playing && d.tape.motor == 0 {
		d.changed() // the tape has wound down, so the output can be suspended
	}
	return len(samples), true
}

// stopWind breaks off a wind, for the tape to be put somewhere else: the
// motor is back at a speed of playing. The caller must hold d.mu.
func (d *deck) stopWind() {
	if d.tape.wind == nil {
		return
	}
	d.tape.wind = nil
	d.tape.motor = 0
	if d.playing {
		d.tape.motor = 1
	}
}

// land ends a wind that got to where it was to go: the tape plays on from
// where the head is, or stands there. A decoder that failed while winding is
// told to seek there instead, and a track that cannot do that either is
// over, as one that breaks off while it plays. The caller must hold d.mu.
func (d *deck) land() {
	w := d.tape.wind
	d.stopWind()
	d.tape.reset()
	if err := w.release(); err != nil {
		d.ended = true
		d.changed()
		select {
		case d.events <- Event{Gen: d.gen, Err: err}:
		default:
		}
		return
	}
	d.stream = wrapStream(d.src) // no stale frames from before the wind
}

// fill streams the loaded track into samples and returns how many frames it
// filled. When the track ends the deck rolls on to the queued track in the
// same buffer, so the music carries on without a gap; fewer than
// len(samples) frames means there was none and the deck has stopped. The
// caller must hold d.mu.
func (d *deck) fill(samples [][2]float64) int {
	n := 0
	for n < len(samples) && !d.ended {
		k, ok, err := d.read(samples[n:])
		n += k
		if ok && k > 0 {
			continue // a short read is followed by (0, false) at the end
		}
		if err == nil {
			err = d.stream.Err()
		}
		if err == nil && d.src != nil {
			err = d.src.stream.Err()
		}
		ev := Event{Gen: d.gen, Err: err}
		if d.next != nil {
			old := d.src
			d.src, d.stream, d.gen = d.next, d.nextStream, d.nextGen
			d.next, d.nextStream, d.nextGen = nil, nil, 0
			d.tape.load(d.src.Tape())
			ev.Next = d.gen
			go old.Close() // not on the audio thread
		} else {
			d.ended = true
			d.changed()
		}
		select {
		case d.events <- ev:
		default:
		}
	}
	return n
}

// read streams the loaded track into samples, like its Stream. The decoders
// are not hardened against every damaged file, and a panic of theirs here,
// on the goroutine of the output, would end cassetty on the spot and leave
// the terminal as it is: it ends the track with an error instead. The caller
// must hold d.mu.
func (d *deck) read(samples [][2]float64) (n int, ok bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			n, ok, err = 0, false, damaged(r)
		}
	}()
	n, ok = d.stream.Stream(samples)
	return n, ok, nil
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
