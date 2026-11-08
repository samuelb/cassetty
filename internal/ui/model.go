// Package ui implements the cassetty terminal interface.
package ui

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/samuelb/cassetty/internal/audio"
	"github.com/samuelb/cassetty/internal/config"
	"github.com/samuelb/cassetty/internal/library"
	"github.com/samuelb/cassetty/internal/nowplaying"
	"github.com/samuelb/cassetty/internal/viz"
	"github.com/samuelb/cassetty/internal/watch"
)

type playState int

const (
	stateStopped playState = iota
	statePlaying
	statePaused
)

type repeatMode int

const (
	repeatOff repeatMode = iota
	repeatAll
	repeatOne
)

var repeatNames = map[repeatMode]string{repeatOff: "off", repeatAll: "all", repeatOne: "one"}

type tab int

const (
	tabLibrary tab = iota
	tabPlaylist
)

// Track is a playlist entry.
type Track struct {
	Path   string
	Meta   *library.Meta // nil until read
	Length time.Duration
	Failed bool
}

// Options configure the UI.
type Options struct {
	Root            string
	StartDir        string
	Theme           string
	Volume          int
	ShowHidden      bool
	Shuffle         bool
	Repeat          string
	Tape            bool // start with the tape sound on
	PaintBackground bool
	SeekStep        time.Duration
	Formats         library.Formats
	Engine          *audio.Engine
	NowPlaying      *nowplaying.Session // media keys; nil where unsupported
	Notice          string              // shown once at startup
	Resume          config.Playback     // where the last run left off
}

// Model is the root bubbletea model.
type Model struct {
	opts     Options
	engine   *audio.Engine
	remote   *nowplaying.Session
	theme    Theme
	themeIdx int
	keys     keyMap
	help     help.Model

	width, height int
	lay           layout

	// playback
	playlist  []*Track
	cur       *Track // loaded track (may have been removed from the playlist)
	curHint   int    // last known playlist index of cur
	src       *audio.Source
	meta      library.Meta
	gen       uint64
	state     playState
	pos       time.Duration
	volume    int
	muted     bool
	shuffle   bool
	order     []int
	repeat    repeatMode
	loadSeq   int
	loading   bool
	failures  int
	remaining bool
	tape      bool // the tape sound is on

	// the track opened ahead to follow cur without a gap
	queue    queued
	queueSeq int
	// the shuffled order of the next round, drawn when asked for the track
	// after the last one, see nextIndexFrom
	nextOrder []int

	// the track the last run left off at, until it has been opened, and the
	// position in it
	resuming *Track
	resumeAt time.Duration

	// animation
	tickID         int
	lastTick       time.Time
	angleL, angleR float64
	spin           float64
	spectrum       *viz.Spectrum
	vu             viz.VU
	frames         [][2]float64
	marquee        float64
	blink          float64

	// library browser
	root      string
	dir       string
	entries   []library.Entry
	visible   []int
	cursor    int
	offset    int
	memory    map[string]string
	filter    string
	filtering bool
	dirErr    error

	// live updates of the shown folder
	watcher    *watch.Watcher
	polling    bool
	pollID     int
	refreshAt  time.Time
	refreshSeq int

	// playlist view
	plCursor, plOffset int

	tab        tab
	showHelp   bool
	helpOffset int

	// mouse
	hover        string
	hoverFrac    float64
	hoverRow     int
	drag         string
	dragFrac     float64
	lastClick    time.Time
	lastClickKey string

	flash     string
	flashErr  bool
	flashTill time.Time
}

// New creates the model.
func New(opts Options) *Model {
	m := &Model{
		opts:     opts,
		engine:   opts.Engine,
		remote:   opts.NowPlaying,
		keys:     newKeyMap(),
		help:     help.New(),
		curHint:  -1,
		volume:   opts.Volume,
		shuffle:  opts.Shuffle,
		tape:     opts.Tape,
		spectrum: viz.NewSpectrum(),
		frames:   make([][2]float64, viz.FrameSize),
		root:     filepath.Clean(opts.Root),
		memory:   map[string]string{},
		hoverRow: -1,
	}
	for k, v := range repeatNames {
		if v == opts.Repeat {
			m.repeat = k
		}
	}
	m.setTheme(themeIndex(opts.Theme))
	m.dir = m.root
	if opts.StartDir != "" && within(m.root, opts.StartDir) {
		if fi, err := os.Stat(opts.StartDir); err == nil && fi.IsDir() {
			m.dir = filepath.Clean(opts.StartDir)
		}
	}
	m.restore(opts.Resume)
	m.engine.SetVolume(float64(m.volume) / 100)
	m.engine.SetTape(m.tape)
	if opts.Notice != "" {
		m.setFlash(opts.Notice, true)
	}
	if w, err := watch.New(); err == nil {
		m.watcher = w
	}
	return m
}

// Close releases the folder watcher.
func (m *Model) Close() {
	if m.watcher != nil {
		m.watcher.Close()
	}
}

// State returns what should be remembered for the next run.
func (m *Model) State() config.State {
	vol := m.volume
	return config.State{
		Volume:  &vol,
		Theme:   m.theme.Name,
		Shuffle: m.shuffle,
		Repeat:  repeatNames[m.repeat],
		Tape:    m.tape,
		LastDir: m.dir,

		Playback: m.playback(),
	}
}

// playback returns where playback is, for the next run to pick up from.
func (m *Model) playback() config.Playback {
	var pb config.Playback
	for _, t := range m.playlist {
		pb.Playlist = append(pb.Playlist, t.Path)
	}
	if m.shuffle && len(m.order) == len(m.playlist) {
		pb.Order = slices.Clone(m.order)
	}
	cur, pos := m.cur, m.pos
	switch {
	case m.src != nil:
		pos = m.engine.Position() // m.pos is as of the last frame
	case cur == nil && m.resuming != nil:
		cur, pos = m.resuming, m.resumeAt // quit before it opened
	}
	if cur != nil {
		m.currentIndex() // brings curHint up to date
		pb.Track, pb.Index = cur.Path, m.curHint
		pb.Position = math.Round(pos.Seconds()*1000) / 1000
	}
	return pb
}

// restore brings back the playlist of the last run, with its shuffled order,
// and remembers the track it left off at for Init to load. Tracks outside
// the library are left out.
func (m *Model) restore(pb config.Playback) {
	// kept[i] is the number of tracks kept before pb.Playlist[i].
	kept := make([]int, len(pb.Playlist)+1)
	for i, p := range pb.Playlist {
		kept[i+1] = kept[i]
		if filepath.IsAbs(p) && within(m.root, p) {
			m.playlist = append(m.playlist, &Track{Path: p})
			kept[i+1]++
		}
	}
	if n := len(m.playlist); n == len(pb.Playlist) && len(pb.Order) == n {
		seen := make([]bool, n)
		ok := true
		for _, i := range pb.Order {
			if i < 0 || i >= n || seen[i] {
				ok = false
				break
			}
			seen[i] = true
		}
		if ok {
			m.order = slices.Clone(pb.Order)
		}
	}
	if pb.Track == "" || !filepath.IsAbs(pb.Track) || !within(m.root, pb.Track) {
		return
	}
	at := time.Duration(max(0, pb.Position) * float64(time.Second))
	m.resuming, m.resumeAt = &Track{Path: pb.Track}, at
	if i := pb.Index; i >= 0 && i <= len(pb.Playlist) {
		m.curHint = kept[i]
		if i < len(pb.Playlist) && kept[i+1] > kept[i] && pb.Playlist[i] == pb.Track {
			m.resuming = m.playlist[m.curHint]
			m.plCursor = m.curHint
		}
	}
}

// resume loads the track the last run left off at, paused where it was,
// and reads the tags of the restored playlist.
func (m *Model) resume() tea.Cmd {
	cmds := []tea.Cmd{m.readMeta(m.playlist)}
	if m.resuming != nil {
		cmds = append(cmds, m.loadAt(m.resuming, false, m.resumeAt))
	}
	return tea.Batch(cmds...)
}

func (m *Model) setTheme(i int) {
	m.themeIdx = (i + len(themes)) % len(themes)
	m.theme = themes[m.themeIdx]
	t := m.theme
	st := help.DefaultDarkStyles()
	st.ShortKey = fg(t.Accent)
	st.ShortDesc = fg(t.Muted)
	st.ShortSeparator = fg(t.Faint)
	st.FullKey = fg(t.Accent)
	st.FullDesc = fg(t.Muted)
	st.FullSeparator = fg(t.Faint)
	st.Ellipsis = fg(t.Faint)
	m.help.Styles = st
}

// messages

type tickMsg struct {
	id int
	t  time.Time
}

type dirMsg struct {
	dir     string
	entries []library.Entry
	err     error
	focus   string
	refresh bool // re-read of the shown folder after a change
}

type collectMode int

const (
	collectAppend collectMode = iota
	collectPlay
)

type collectMsg struct {
	dir   string
	paths []string
	err   error
	mode  collectMode
}

type openedMsg struct {
	seq      int
	track    *Track
	src      *audio.Source
	meta     library.Meta
	err      error
	autoplay bool
	at       time.Duration // where to put the play head
}

// queued is the track opened ahead of time so that the deck can roll on to
// it without a gap when the loaded one ends.
type queued struct {
	track *Track
	seq   int           // of the open request
	src   *audio.Source // nil while opening or when it failed; the engine owns it
	meta  library.Meta
	gen   uint64
}

type queuedMsg struct {
	seq   int
	track *Track
	src   *audio.Source
	meta  library.Meta
	err   error
}

type audioEventMsg audio.Event

type metaMsg map[string]library.Meta

func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		m.loadDir(m.dir, ""),
		m.waitAudio(),
		m.waitWatch(),
		m.waitRemote(),
		m.kickTick(),
		m.resume(),
	)
}

func (m *Model) waitAudio() tea.Cmd {
	ch := m.engine.Events()
	return func() tea.Msg { return audioEventMsg(<-ch) }
}

// kickTick (re)starts the animation loop; any older loop dies out.
func (m *Model) kickTick() tea.Cmd {
	m.tickID++
	id := m.tickID
	return func() tea.Msg { return tickMsg{id: id, t: time.Now()} }
}

func (m *Model) animating() bool {
	if m.state == statePlaying || m.spin != 0 || m.drag != "" || m.engine.Speed() > 0 {
		return true
	}
	if m.vu.L > 0.002 || m.vu.R > 0.002 || m.vu.PeakL > 0.002 || m.vu.PeakR > 0.002 {
		return true
	}
	for i := range m.spectrum.Levels {
		if m.spectrum.Levels[i] > 0.002 || m.spectrum.Peaks[i] > 0.002 {
			return true
		}
	}
	return false
}

func (m *Model) nextTick() tea.Cmd {
	d := 500 * time.Millisecond
	switch {
	case m.animating():
		d = 40 * time.Millisecond
	case m.state == statePaused:
		d = 250 * time.Millisecond
	}
	id := m.tickID
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg{id: id, t: t} })
}

// animate advances reels, visualizers and the marquee by dt seconds.
func (m *Model) animate(dt float64) {
	if m.src != nil {
		m.pos = m.engine.Position()
	}
	playing := m.state == statePlaying
	// The tape keeps moving for a moment after a pause while it winds down.
	motor := m.engine.Speed()
	running := motor > 0

	if m.src != nil {
		speed := m.spin + motor
		// Constant tape speed: angular velocity is inversely proportional
		// to the radius of the tape pack on each reel.
		const rMin, rMax, base = 1.0, 2.3, 4.0
		p := m.progress()
		rL := math.Sqrt(rMin*rMin + (1-p)*(rMax*rMax-rMin*rMin))
		rR := math.Sqrt(rMin*rMin + p*(rMax*rMax-rMin*rMin))
		step := func(r float64) float64 {
			d := dt * speed * base * rMin / r
			return math.Max(-0.45, math.Min(0.45, d)) // stay below the strobe limit
		}
		m.angleL += step(rL)
		m.angleR += step(rR)
	}
	m.spin *= math.Exp(-dt * 3)
	if math.Abs(m.spin) < 0.05 {
		m.spin = 0
	}

	if running {
		m.engine.Snapshot(m.frames)
	}
	m.spectrum.Update(m.frames, m.lay.specBars, float64(audio.SampleRate), dt, running)
	m.vu.Update(m.frames[len(m.frames)-2048:], dt, running)

	if playing {
		m.marquee += dt * 5
	}
	m.blink += dt
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := m.update(msg)
	m.syncNowPlaying()
	return m, tea.Batch(cmd, m.queueNext())
}

func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.relayout()
		return m, nil

	case tickMsg:
		if msg.id != m.tickID {
			return m, nil
		}
		dt := 0.0
		if !m.lastTick.IsZero() {
			dt = math.Min(0.25, msg.t.Sub(m.lastTick).Seconds())
		}
		m.lastTick = msg.t
		m.animate(dt)
		return m, m.nextTick()

	case dirMsg:
		return m, m.applyDir(msg)

	case watchMsg, refreshMsg, pollMsg:
		return m, m.handleLive(msg)

	case collectMsg:
		return m, m.applyCollect(msg)

	case openedMsg:
		return m, m.applyOpened(msg)

	case queuedMsg:
		return m, m.applyQueued(msg)

	case metaMsg:
		for _, t := range m.playlist {
			if md, ok := msg[t.Path]; ok && t.Meta == nil {
				t.Meta = &md
			}
		}
		return m, nil

	case audioEventMsg:
		cmds := []tea.Cmd{m.waitAudio()}
		if msg.Gen == m.gen && !m.loading {
			if msg.Err != nil {
				m.setFlash("playback error: "+msg.Err.Error(), true)
			}
			if msg.Next != 0 && msg.Next == m.queue.gen {
				cmds = append(cmds, m.advance())
			} else {
				cmds = append(cmds, m.trackEnded())
			}
		}
		return m, tea.Batch(cmds...)

	case remoteMsg:
		return m, tea.Batch(m.waitRemote(), m.handleRemote(nowplaying.Command(msg)))

	case tea.KeyPressMsg:
		return m, m.handleKey(msg)

	case tea.MouseMsg:
		return m, m.handleMouse(msg)
	}
	return m, nil
}

// --- playback --------------------------------------------------------------

// progress is the play head position as a fraction of the track length.
func (m *Model) progress() float64 {
	if m.src == nil || m.src.Length <= 0 {
		return 0
	}
	return math.Max(0, math.Min(1, float64(m.displayPos())/float64(m.src.Length)))
}

// displayPos is the position shown in the UI (follows the seek bar drag).
func (m *Model) displayPos() time.Duration {
	if m.drag == "seek" && m.src != nil {
		return time.Duration(m.dragFrac * float64(m.src.Length))
	}
	return m.pos
}

func (m *Model) currentIndex() int {
	if m.cur == nil {
		return -1
	}
	if m.curHint >= 0 && m.curHint < len(m.playlist) && m.playlist[m.curHint] == m.cur {
		return m.curHint
	}
	i := slices.Index(m.playlist, m.cur)
	if i >= 0 {
		m.curHint = i
	}
	return i
}

// load opens the track asynchronously and installs it when ready.
func (m *Model) load(t *Track, autoplay bool) tea.Cmd { return m.loadAt(t, autoplay, 0) }

// loadAt is load, with the play head put at position at. A track loaded
// there without playing waits paused.
func (m *Model) loadAt(t *Track, autoplay bool, at time.Duration) tea.Cmd {
	m.loadSeq++
	m.loading = true
	seq := m.loadSeq
	return func() tea.Msg {
		src, err := audio.Open(t.Path)
		var meta library.Meta
		if err == nil {
			meta = library.ReadMeta(t.Path)
		}
		return openedMsg{seq: seq, track: t, src: src, meta: meta, err: err, autoplay: autoplay, at: at}
	}
}

func (m *Model) applyOpened(msg openedMsg) tea.Cmd {
	if msg.seq != m.loadSeq {
		msg.src.Close() // superseded by a newer request
		return nil
	}
	m.loading = false
	m.resuming = nil
	if msg.err != nil {
		msg.track.Failed = true
		m.setFlash(msg.err.Error(), true)
		m.failures++
		if msg.autoplay && m.failures < len(m.playlist) {
			if i := slices.Index(m.playlist, msg.track); i >= 0 {
				m.turnOrder(i)
				if n, ok := m.nextIndexFrom(i); ok {
					return m.load(m.playlist[n], true)
				}
			}
		}
		return nil
	}
	m.queue = queued{} // Start clears the engine's queue
	m.install(msg.track, msg.src, msg.meta, m.engine.Start(msg.src, msg.autoplay))
	if msg.at > 0 {
		m.seekTo(msg.at)
	}
	switch {
	case msg.autoplay:
		m.state = statePlaying
	case m.pos > 0:
		m.state = statePaused
	default:
		m.state = stateStopped
	}
	return m.kickTick()
}

// install makes t, opened as src, the loaded track; gen is its generation
// in the engine.
func (m *Model) install(t *Track, src *audio.Source, meta library.Meta, gen uint64) {
	t.Meta, t.Length, t.Failed = &meta, src.Length, false
	m.failures = 0
	m.cur, m.src, m.meta, m.gen = t, src, meta, gen
	m.turnOrder(m.currentIndex())
	m.pos, m.marquee = 0, 0
}

// advance makes the queued track, which the deck has rolled on to, the
// loaded one. It keeps playing, or winding down after a pause.
func (m *Model) advance() tea.Cmd {
	q := m.queue
	m.queue = queued{}
	m.install(q.track, q.src, q.meta, q.gen)
	return m.kickTick()
}

// playTrack loads t, straight from the queue when it is opened there.
func (m *Model) playTrack(t *Track, autoplay bool) tea.Cmd {
	if q := m.queue; q.track == t && q.src != nil {
		m.queue = queued{}
		m.loadSeq++ // supersedes a track still being opened
		return m.applyOpened(openedMsg{seq: m.loadSeq, track: t, src: q.src, meta: q.meta, autoplay: autoplay})
	}
	return m.load(t, autoplay)
}

// queueNext keeps the track that follows the loaded one opened in the
// engine's queue, so the deck rolls on to it without a gap. It runs after
// every message and only acts when the next track changes.
func (m *Model) queueNext() tea.Cmd {
	if m.loading {
		return nil // the track being opened clears the queue anyway
	}
	var want *Track
	if m.cur != nil {
		if m.repeat == repeatOne {
			want = m.cur
		} else if i, ok := m.nextIndex(); ok {
			want = m.playlist[i]
		}
	}
	if want == m.queue.track {
		return nil
	}
	if m.queue.src != nil {
		m.engine.Queue(nil)
	}
	m.queue = queued{}
	if want == nil {
		return nil
	}
	m.queueSeq++
	seq := m.queueSeq
	m.queue = queued{track: want, seq: seq}
	return func() tea.Msg {
		src, err := audio.Open(want.Path)
		var meta library.Meta
		if err == nil {
			meta = library.ReadMeta(want.Path)
		}
		return queuedMsg{seq: seq, track: want, src: src, meta: meta, err: err}
	}
}

func (m *Model) applyQueued(msg queuedMsg) tea.Cmd {
	if msg.seq != m.queue.seq {
		msg.src.Close() // the next track changed meanwhile
		return nil
	}
	if msg.err != nil {
		return nil // it is loaded the usual way when its turn comes, which reports the error
	}
	m.queue.src, m.queue.meta = msg.src, msg.meta
	m.queue.gen = m.engine.Queue(msg.src)
	return nil
}

func (m *Model) setState(s playState) tea.Cmd {
	if m.src == nil {
		return nil
	}
	m.state = s
	m.engine.SetPlaying(s == statePlaying)
	m.blink = 0
	return m.kickTick()
}

// play implements the PLAY key (Winamp semantics: restarts when playing).
func (m *Model) play() tea.Cmd {
	if m.src == nil {
		return m.playSomething()
	}
	if m.state == statePlaying {
		m.seekTo(0)
		return nil
	}
	return m.setState(statePlaying)
}

func (m *Model) pause() tea.Cmd {
	switch m.state {
	case statePlaying:
		return m.setState(statePaused)
	case statePaused:
		return m.setState(statePlaying)
	}
	return nil
}

func (m *Model) togglePlay() tea.Cmd {
	if m.src == nil {
		return m.playSomething()
	}
	if m.state == statePlaying {
		return m.setState(statePaused)
	}
	return m.setState(statePlaying)
}

// playSomething starts playback when no tape is loaded.
func (m *Model) playSomething() tea.Cmd {
	if len(m.playlist) > 0 {
		i := m.plCursor
		if m.tab == tabLibrary || i >= len(m.playlist) {
			i = max(0, m.currentIndex())
		}
		return m.load(m.playlist[i], true)
	}
	if e, ok := m.selectedEntry(); ok && !e.IsDir {
		return m.playFromDir(e)
	}
	m.setFlash("nothing to play — pick a track in the library", false)
	return nil
}

func (m *Model) stop() tea.Cmd {
	if m.src == nil {
		return nil
	}
	cmd := m.setState(stateStopped)
	m.seekTo(0) // after stopping, so the tape does not wind down from the start
	return cmd
}

func (m *Model) eject() tea.Cmd {
	if m.src == nil {
		return nil
	}
	m.engine.Eject()
	m.src, m.cur = nil, nil
	m.queue = queued{} // Eject clears the engine's queue
	m.state = stateStopped
	m.pos = 0
	m.setFlash("tape ejected", false)
	return m.kickTick()
}

func (m *Model) seekTo(t time.Duration) {
	if m.src == nil {
		return
	}
	if m.src.Length > 0 {
		t = min(t, m.src.Length)
	}
	t = max(0, t)
	if err := m.engine.Seek(t); err != nil {
		m.setFlash("seek: "+err.Error(), true)
		return
	}
	m.pos = t
}

func (m *Model) seekBy(d time.Duration) tea.Cmd {
	if m.src == nil {
		return nil
	}
	m.seekTo(m.engine.Position() + d)
	if d > 0 {
		m.spin = 6
	} else {
		m.spin = -6
	}
	return m.kickTick()
}

func (m *Model) trackEnded() tea.Cmd {
	// A track can also end while paused, as the tape winds down.
	play := m.state == statePlaying
	if m.repeat == repeatOne && m.src != nil {
		m.seekTo(0)
		if !play {
			return nil
		}
		return m.setState(statePlaying)
	}
	if i, ok := m.nextIndex(); ok {
		return m.playTrack(m.playlist[i], play)
	}
	cmd := m.setState(stateStopped)
	m.seekTo(0)
	m.setFlash("end of tape", false)
	return cmd
}

func (m *Model) next() tea.Cmd {
	i, ok := m.nextIndex()
	if !ok {
		m.setFlash("end of playlist", false)
		return nil
	}
	return m.playTrack(m.playlist[i], m.state == statePlaying || m.src == nil)
}

func (m *Model) prev() tea.Cmd {
	if m.src != nil && m.engine.Position() > 3*time.Second {
		m.seekTo(0)
		return nil
	}
	i, ok := m.prevIndex()
	if !ok {
		m.seekTo(0)
		return nil
	}
	return m.load(m.playlist[i], m.state == statePlaying || m.src == nil)
}

// resetOrder drops the shuffled order, which is drawn anew when needed.
func (m *Model) resetOrder() { m.order, m.nextOrder = nil, nil }

func (m *Model) reshuffle() {
	m.nextOrder = nil
	m.order = rand.Perm(len(m.playlist))
	if c := m.currentIndex(); c >= 0 {
		i := slices.Index(m.order, c)
		m.order[0], m.order[i] = m.order[i], m.order[0]
	}
}

func (m *Model) nextIndex() (int, bool) { return m.nextIndexFrom(m.currentIndex()) }

// nextIndexFrom returns the playlist index that follows index c, honouring
// shuffle and repeat. c < 0 means no track of the playlist is loaded. Asking
// again gives the same answer until the playlist, the modes or the loaded
// track change, so the next track can be queued ahead of time.
func (m *Model) nextIndexFrom(c int) (int, bool) {
	n := len(m.playlist)
	if n == 0 {
		return -1, false
	}
	if c < 0 {
		// The loaded track was removed: continue where it used to be.
		if m.cur != nil && m.curHint >= 0 && m.curHint < n {
			return m.curHint, true
		}
		return 0, true
	}
	if m.shuffle {
		if len(m.order) != n {
			m.reshuffle()
		}
		if p := slices.Index(m.order, c); p+1 < n {
			return m.order[p+1], true
		}
		if m.repeat == repeatAll {
			// The next round, in a new order that takes over when its
			// first track loads (see turnOrder).
			if len(m.nextOrder) != n {
				m.nextOrder = rand.Perm(n)
				if n > 1 && m.nextOrder[0] == c {
					m.nextOrder[0], m.nextOrder[n-1] = m.nextOrder[n-1], m.nextOrder[0]
				}
			}
			return m.nextOrder[0], true
		}
		return -1, false
	}
	if c+1 < n {
		return c + 1, true
	}
	if m.repeat == repeatAll {
		return 0, true
	}
	return -1, false
}

// turnOrder starts the next shuffled round when track i opens it.
func (m *Model) turnOrder(i int) {
	if i >= 0 && len(m.nextOrder) == len(m.playlist) && m.nextOrder[0] == i {
		m.order, m.nextOrder = m.nextOrder, nil
	}
}

func (m *Model) prevIndex() (int, bool) {
	n := len(m.playlist)
	c := m.currentIndex()
	if n == 0 || c < 0 {
		return -1, false
	}
	if m.shuffle && len(m.order) == n {
		if p := slices.Index(m.order, c); p > 0 {
			return m.order[p-1], true
		}
		return -1, false
	}
	if c > 0 {
		return c - 1, true
	}
	if m.repeat == repeatAll {
		return n - 1, true
	}
	return -1, false
}

func (m *Model) setVolume(v int) {
	m.volume = max(0, min(100, v))
	m.muted = false
	m.engine.SetVolume(float64(m.volume) / 100)
}

func (m *Model) setShuffle(on bool) {
	m.shuffle = on
	m.resetOrder()
	m.setFlash("shuffle "+onOff(on), false)
}

func (m *Model) setRepeat(r repeatMode) {
	m.repeat = r
	m.setFlash("repeat "+repeatNames[r], false)
}

func (m *Model) toggleTape() {
	m.tape = !m.tape
	m.engine.SetTape(m.tape)
	m.setFlash("tape sound "+onOff(m.tape), false)
}

func (m *Model) toggleMute() {
	m.muted = !m.muted
	if m.muted {
		m.engine.SetVolume(0)
	} else {
		m.engine.SetVolume(float64(m.volume) / 100)
	}
}

// --- playlist --------------------------------------------------------------

func tracksFor(paths []string) []*Track {
	out := make([]*Track, len(paths))
	for i, p := range paths {
		out[i] = &Track{Path: p}
	}
	return out
}

func (m *Model) readMeta(tracks []*Track) tea.Cmd {
	var paths []string
	for _, t := range tracks {
		if t.Meta == nil {
			paths = append(paths, t.Path)
		}
	}
	if len(paths) == 0 {
		return nil
	}
	return func() tea.Msg {
		out := make(metaMsg, len(paths))
		for _, p := range paths {
			out[p] = library.ReadMeta(p)
		}
		return out
	}
}

// replacePlaylist swaps in a new playlist and starts playing track start.
func (m *Model) replacePlaylist(tracks []*Track, start int) tea.Cmd {
	m.playlist = tracks
	m.resetOrder()
	m.curHint = -1
	m.plCursor, m.plOffset = start, 0
	m.failures = 0
	return tea.Batch(m.load(tracks[start], true), m.readMeta(tracks))
}

func (m *Model) appendTracks(tracks []*Track) tea.Cmd {
	if len(tracks) == 0 {
		return nil
	}
	m.playlist = append(m.playlist, tracks...)
	m.resetOrder()
	cmds := []tea.Cmd{m.readMeta(tracks)}
	if m.src == nil && !m.loading {
		// An empty deck: insert the tape, ready to play.
		cmds = append(cmds, m.load(tracks[0], false))
	}
	return tea.Batch(cmds...)
}

func (m *Model) applyCollect(msg collectMsg) tea.Cmd {
	if msg.err != nil {
		m.setFlash(msg.err.Error(), true)
		return nil
	}
	if len(msg.paths) == 0 {
		m.setFlash("no audio files in "+filepath.Base(msg.dir), true)
		return nil
	}
	tracks := tracksFor(msg.paths)
	if msg.mode == collectPlay {
		m.setFlash(fmt.Sprintf("playing %s · %s", filepath.Base(msg.dir), plural(len(tracks), "track")), false)
		start := 0
		if m.shuffle {
			start = rand.IntN(len(tracks))
		}
		return m.replacePlaylist(tracks, start)
	}
	m.setFlash(fmt.Sprintf("added %s from %s", plural(len(tracks), "track"), filepath.Base(msg.dir)), false)
	return m.appendTracks(tracks)
}

func (m *Model) collect(dir string, mode collectMode) tea.Cmd {
	formats, hidden := m.opts.Formats, m.opts.ShowHidden
	return func() tea.Msg {
		paths, err := library.Collect(dir, formats, hidden)
		return collectMsg{dir: dir, paths: paths, err: err, mode: mode}
	}
}

// playFromDir plays the folder of entry e, starting at e.
func (m *Model) playFromDir(e library.Entry) tea.Cmd {
	var tracks []*Track
	start := 0
	for _, en := range m.entries {
		if en.IsDir {
			continue
		}
		if en.Path == e.Path {
			start = len(tracks)
		}
		tracks = append(tracks, &Track{Path: en.Path})
	}
	if len(tracks) == 0 {
		return nil
	}
	return m.replacePlaylist(tracks, start)
}

func (m *Model) removeFromPlaylist(i int) {
	if i < 0 || i >= len(m.playlist) {
		return
	}
	if c := m.currentIndex(); c == i {
		m.curHint = i // keep the place so "next" continues here
	} else if c > i {
		m.curHint = c - 1
	}
	m.playlist = slices.Delete(m.playlist, i, i+1)
	m.resetOrder()
	m.plCursor = min(m.plCursor, max(0, len(m.playlist)-1))
}

func (m *Model) movePlaylist(i, d int) {
	j := i + d
	if i < 0 || j < 0 || i >= len(m.playlist) || j >= len(m.playlist) {
		return
	}
	m.playlist[i], m.playlist[j] = m.playlist[j], m.playlist[i]
	m.resetOrder()
	m.plCursor = j
}

// --- helpers ---------------------------------------------------------------

func (m *Model) setFlash(s string, isErr bool) {
	m.flash, m.flashErr = s, isErr
	m.flashTill = time.Now().Add(4 * time.Second)
	if isErr {
		m.flashTill = time.Now().Add(7 * time.Second)
	}
}

func trackName(path string) string {
	b := filepath.Base(path)
	return strings.TrimSuffix(b, filepath.Ext(b))
}

// trackDisplay is "Artist - Title" when tags are available.
func (m *Model) trackDisplay() string {
	if m.meta.Title != "" {
		if m.meta.Artist != "" {
			return m.meta.Artist + " - " + m.meta.Title
		}
		return m.meta.Title
	}
	if m.cur != nil {
		return trackName(m.cur.Path)
	}
	return ""
}

func (m *Model) trackTitle() string {
	if m.meta.Title != "" {
		return m.meta.Title
	}
	if m.cur != nil {
		return trackName(m.cur.Path)
	}
	return ""
}

// trackArtistAlbum returns the artist and album of the loaded track, falling
// back to the names of its folders.
func (m *Model) trackArtistAlbum() (artist, album string) {
	artist, album = m.meta.Artist, m.meta.Album
	if m.cur != nil {
		dir := filepath.Dir(m.cur.Path)
		if album == "" && dir != m.root && within(m.root, dir) {
			album = filepath.Base(dir)
		}
		if parent := filepath.Dir(dir); artist == "" && dir != m.root && parent != m.root && within(m.root, parent) {
			artist = filepath.Base(parent)
		}
	}
	return artist, album
}

// trackSubtitle is "Artist · Album", falling back to the folder names.
func (m *Model) trackSubtitle() string {
	artist, album := m.trackArtistAlbum()
	switch {
	case artist != "" && album != "":
		return artist + " · " + album
	case artist != "":
		return artist
	}
	return album
}

func (m *Model) labelRight() string {
	n := len(m.playlist)
	if i := m.currentIndex(); i >= 0 && n > 1 {
		return fmt.Sprintf("%d/%d", i+1, n)
	}
	return ""
}

func (m *Model) tapeType() string {
	if m.src == nil {
		return ""
	}
	return m.src.Tape().String()
}

// tapeLength names the cassette after the track length, e.g. "C-46".
func (m *Model) tapeLength() string {
	if m.src == nil || m.src.Length <= 0 {
		return "C-60"
	}
	mins := int(math.Ceil(m.src.Length.Minutes())) * 2
	for _, c := range []int{10, 46, 60, 90, 120} {
		if mins <= c {
			return fmt.Sprintf("C-%d", c)
		}
	}
	return "C-120"
}

// within reports whether path is root or below it.
func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	k := m.keys

	if m.filtering {
		return m.handleFilterKey(msg)
	}
	if m.showHelp {
		switch {
		case msg.String() == "ctrl+c":
			return tea.Quit
		case key.Matches(msg, k.Help, k.Quit), msg.String() == "esc":
			m.showHelp = false
		case key.Matches(msg, k.Up):
			m.helpOffset--
		case key.Matches(msg, k.Down):
			m.helpOffset++
		case key.Matches(msg, k.PageUp):
			m.helpOffset -= 10
		case key.Matches(msg, k.PageDown):
			m.helpOffset += 10
		}
		m.helpOffset = max(0, m.helpOffset)
		return nil
	}

	switch {
	case key.Matches(msg, k.Quit):
		return tea.Quit
	case key.Matches(msg, k.Help):
		m.showHelp, m.helpOffset = true, 0
	case key.Matches(msg, k.PlayPause):
		return m.togglePlay()
	case key.Matches(msg, k.Stop):
		return m.stop()
	case key.Matches(msg, k.Next):
		return m.next()
	case key.Matches(msg, k.Prev):
		return m.prev()
	case key.Matches(msg, k.Eject):
		return m.eject()
	case key.Matches(msg, k.SeekBack):
		return m.seekBy(-m.opts.SeekStep)
	case key.Matches(msg, k.SeekFwd):
		return m.seekBy(m.opts.SeekStep)
	case key.Matches(msg, k.SeekBackFar):
		return m.seekBy(-6 * m.opts.SeekStep)
	case key.Matches(msg, k.SeekFwdFar):
		return m.seekBy(6 * m.opts.SeekStep)
	case key.Matches(msg, k.VolUp):
		m.setVolume(m.volume + 5)
	case key.Matches(msg, k.VolDown):
		m.setVolume(m.volume - 5)
	case key.Matches(msg, k.Mute):
		m.toggleMute()
	case key.Matches(msg, k.Shuffle):
		m.setShuffle(!m.shuffle)
	case key.Matches(msg, k.Repeat):
		m.setRepeat((m.repeat + 1) % 3)
	case key.Matches(msg, k.Theme):
		m.setTheme(m.themeIdx + 1)
		m.setFlash("theme: "+m.theme.Name, false)
	case key.Matches(msg, k.Remaining):
		m.remaining = !m.remaining
	case key.Matches(msg, k.Tape):
		m.toggleTape()
	case key.Matches(msg, k.Tab):
		m.switchTab()
	case msg.String() == "1":
		m.tab = tabLibrary
	case msg.String() == "2":
		m.tab = tabPlaylist
	default:
		if m.tab == tabPlaylist {
			return m.handlePlaylistKey(msg)
		}
		return m.handleLibraryKey(msg)
	}
	return nil
}

func (m *Model) switchTab() {
	if m.tab == tabLibrary {
		m.tab = tabPlaylist
		if i := m.currentIndex(); i >= 0 {
			m.plCursor = i
			m.ensurePlaylistVisible()
		}
	} else {
		m.tab = tabLibrary
	}
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// ErrNoMusicDir is returned when the configured library root is missing.
var ErrNoMusicDir = errors.New("music folder not found")

// CheckRoot verifies the library root exists and is a folder.
func CheckRoot(root string) error {
	fi, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrNoMusicDir, root)
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s is not a folder", root)
	}
	return nil
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
