package nowplaying

import (
	"errors"
	"math"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// MediaPlayer calls the command handlers on the main dispatch queue, which
// only runs while the main thread serves its run loop. Keeping the main
// goroutine on the main thread lets Main do that.
func init() { runtime.LockOSThread() }

var (
	loadOnce sync.Once
	loadErr  error

	cfRunLoopRun     func()
	cfRunLoopGetMain func() uintptr
	cfRunLoopStop    func(uintptr)
	dispatchAsyncF   func(queue, ctx, fn uintptr)
	pthreadMainNP    func() int32
	poolPush         func() uintptr
	poolPop          func(uintptr)

	mainQueue uintptr // dispatch_get_main_queue()
	drainFn   uintptr // drain as a C function

	// keys of the Now Playing info dictionary
	keyTitle, keyArtist, keyAlbum, keyDuration, keyElapsed, keyRate, keyMediaType objc.ID
)

// load opens the system frameworks once.
func load() error {
	loadOnce.Do(func() { loadErr = loadFrameworks() })
	return loadErr
}

func loadFrameworks() error {
	open := func(path string) (uintptr, error) {
		return purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	}
	sys, err := open("/usr/lib/libSystem.B.dylib")
	if err != nil {
		return err
	}
	libobjc, err := open("/usr/lib/libobjc.A.dylib")
	if err != nil {
		return err
	}
	cf, err := open("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation")
	if err != nil {
		return err
	}
	if _, err := open("/System/Library/Frameworks/Foundation.framework/Foundation"); err != nil {
		return err
	}
	mp, err := open("/System/Library/Frameworks/MediaPlayer.framework/MediaPlayer")
	if err != nil {
		return err
	}
	if objc.GetClass("MPRemoteCommandCenter") == 0 || objc.GetClass("MPNowPlayingInfoCenter") == 0 {
		return errors.New("MediaPlayer lacks the remote command classes")
	}

	purego.RegisterLibFunc(&cfRunLoopRun, cf, "CFRunLoopRun")
	purego.RegisterLibFunc(&cfRunLoopGetMain, cf, "CFRunLoopGetMain")
	purego.RegisterLibFunc(&cfRunLoopStop, cf, "CFRunLoopStop")
	purego.RegisterLibFunc(&dispatchAsyncF, sys, "dispatch_async_f")
	purego.RegisterLibFunc(&pthreadMainNP, sys, "pthread_main_np")
	purego.RegisterLibFunc(&poolPush, libobjc, "objc_autoreleasePoolPush")
	purego.RegisterLibFunc(&poolPop, libobjc, "objc_autoreleasePoolPop")
	if mainQueue, err = purego.Dlsym(sys, "_dispatch_main_q"); err != nil {
		return err
	}

	for name, key := range map[string]*objc.ID{
		"MPMediaItemPropertyTitle":                    &keyTitle,
		"MPMediaItemPropertyArtist":                   &keyArtist,
		"MPMediaItemPropertyAlbumTitle":               &keyAlbum,
		"MPMediaItemPropertyPlaybackDuration":         &keyDuration,
		"MPNowPlayingInfoPropertyElapsedPlaybackTime": &keyElapsed,
		"MPNowPlayingInfoPropertyPlaybackRate":        &keyRate,
		"MPNowPlayingInfoPropertyMediaType":           &keyMediaType,
	} {
		// The symbol is an NSString *const variable.
		p, err := purego.Dlsym(mp, name)
		if err != nil {
			return err
		}
		*key = **(**objc.ID)(unsafe.Pointer(&p))
	}

	drainFn = purego.NewCallback(drain)
	return nil
}

// Main runs f while the main thread serves the run loop that delivers media
// commands, and returns once f has returned. It must be called from the main
// goroutine; elsewhere, or without the system frameworks, it only runs f.
func Main(f func()) {
	if load() != nil || pthreadMainNP() == 0 {
		f()
		return
	}
	go func() {
		// Queued behind any work f left for the main thread.
		defer onMain(func() { cfRunLoopStop(cfRunLoopGetMain()) })
		f()
	}()
	cfRunLoopRun()
}

var (
	queueMu sync.Mutex
	queue   []func()
)

// onMain runs fn on the main thread, after the funcs queued before it.
func onMain(fn func()) {
	queueMu.Lock()
	queue = append(queue, fn)
	first := len(queue) == 1
	queueMu.Unlock()
	if first {
		dispatchAsyncF(mainQueue, 0, drainFn)
	}
}

// drain runs the queued funcs. libdispatch calls it on the main thread.
func drain(uintptr) {
	queueMu.Lock()
	fns := queue
	queue = nil
	queueMu.Unlock()
	pool := poolPush()
	for _, fn := range fns {
		fn()
	}
	poolPop(pool)
}

// Session receives the system's media commands and publishes the loaded
// track. Its methods must not be called concurrently.
type Session struct {
	// C receives the commands. They only arrive while Main runs.
	C <-chan Command

	c       chan Command
	tracker tracker
}

// commands are the MPRemoteCommandCenter commands cassetty handles.
var commands = []struct {
	name string
	kind Kind
}{
	{"togglePlayPauseCommand", TogglePlayPause},
	{"playCommand", Play},
	{"pauseCommand", Pause},
	{"stopCommand", Stop},
	{"nextTrackCommand", Next},
	{"previousTrackCommand", Previous},
	{"changePlaybackPositionCommand", Seek},
}

// Start registers for the media commands.
func Start() (*Session, error) {
	if err := load(); err != nil {
		return nil, err
	}
	c := make(chan Command, 8)
	s := &Session{C: c, c: c}
	onMain(s.register)
	return s, nil
}

func (s *Session) register() {
	center := send(class("MPRemoteCommandCenter"), "sharedCommandCenter")
	for _, cmd := range commands {
		kind := cmd.kind
		handler := objc.NewBlock(func(_ objc.Block, event objc.ID) int {
			c := Command{Kind: kind}
			if kind == Seek {
				secs := objc.Send[float64](event, objc.RegisterName("positionTime"))
				if math.IsNaN(secs) || secs < 0 {
					return 200 // MPRemoteCommandHandlerStatusCommandFailed
				}
				c.Position = time.Duration(secs * float64(time.Second))
			}
			select {
			case s.c <- c:
			default: // the player is falling behind; drop it
			}
			return 0 // MPRemoteCommandHandlerStatusSuccess
		})
		send(send(center, cmd.name), "addTargetWithHandler:", handler)
		handler.Release() // the command keeps a copy
	}
}

// Update publishes info as the Now Playing track. It is cheap to call after
// every change of the player: only changes the system cannot work out by
// itself are passed on.
func (s *Session) Update(info Info) {
	if changed, _ := s.tracker.update(info, time.Now()); changed {
		onMain(func() { publish(info) })
	}
}

// Close withdraws the published track and stops the commands.
func (s *Session) Close() {
	onMain(func() {
		center := send(class("MPRemoteCommandCenter"), "sharedCommandCenter")
		for _, cmd := range commands {
			send(send(center, cmd.name), "removeTarget:", objc.ID(0))
		}
		publish(Info{})
	})
}

// MPNowPlayingPlaybackState
const (
	mpPlaying uint = 1
	mpPaused  uint = 2
	mpStopped uint = 3
)

const mpMediaTypeAudio uint = 1 // MPNowPlayingInfoMediaTypeAudio

func publish(info Info) {
	center := send(class("MPNowPlayingInfoCenter"), "defaultCenter")
	if info.Path == "" {
		send(center, "setNowPlayingInfo:", objc.ID(0))
		send(center, "setPlaybackState:", mpStopped)
		return
	}
	d := send(class("NSMutableDictionary"), "dictionary")
	put := func(key, value objc.ID) {
		if value != 0 {
			send(d, "setObject:forKey:", value, key)
		}
	}
	put(keyTitle, nsString(info.Title))
	put(keyArtist, nsString(info.Artist))
	put(keyAlbum, nsString(info.Album))
	if info.Length > 0 {
		put(keyDuration, send(class("NSNumber"), "numberWithDouble:", info.Length.Seconds()))
	}
	put(keyElapsed, send(class("NSNumber"), "numberWithDouble:", info.Position.Seconds()))
	rate, state := 0.0, mpStopped
	switch info.State {
	case Playing:
		rate, state = 1, mpPlaying
	case Paused:
		state = mpPaused
	}
	put(keyRate, send(class("NSNumber"), "numberWithDouble:", rate))
	put(keyMediaType, send(class("NSNumber"), "numberWithUnsignedInteger:", mpMediaTypeAudio))
	send(center, "setNowPlayingInfo:", d)
	send(center, "setPlaybackState:", state)
}

func class(name string) objc.ID { return objc.ID(objc.GetClass(name)) }

func send(id objc.ID, sel string, args ...any) objc.ID {
	return id.Send(objc.RegisterName(sel), args...)
}

// nsString returns an autoreleased NSString, or 0 for "".
func nsString(s string) objc.ID {
	if s == "" {
		return 0
	}
	// stringWithUTF8String: rejects invalid UTF-8 and stops at the first NUL.
	return send(class("NSString"), "stringWithUTF8String:", text(s))
}
