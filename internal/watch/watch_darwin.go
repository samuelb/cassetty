package watch

import (
	"sync"

	"golang.org/x/sys/unix"
)

// Watcher watches a single directory with kqueue.
type Watcher struct {
	// C receives a value after the watched directory changed. Bursts of
	// changes are coalesced. C is closed when the watcher is closed.
	C <-chan struct{}

	c      chan struct{}
	kq     int
	mu     sync.Mutex
	fd     int // event-only handle of the watched directory, -1 if none
	dev    int32
	ino    uint64
	closed bool
	once   sync.Once
}

const closeIdent = 1 // EVFILT_USER event that wakes the loop on Close

// New starts a watcher that is not watching anything yet.
func New() (*Watcher, error) {
	kq, err := unix.Kqueue()
	if err != nil {
		return nil, err
	}
	unix.CloseOnExec(kq)
	var ev unix.Kevent_t
	unix.SetKevent(&ev, closeIdent, unix.EVFILT_USER, unix.EV_ADD|unix.EV_CLEAR)
	if _, err := unix.Kevent(kq, []unix.Kevent_t{ev}, nil, nil); err != nil {
		unix.Close(kq)
		return nil, err
	}
	c := make(chan struct{}, 1)
	w := &Watcher{C: c, c: c, kq: kq, fd: -1}
	go w.loop()
	return w, nil
}

// Watch makes dir the watched directory, replacing the previous one.
// Watching the directory that is already watched is cheap and re-arms the
// watch if the path now refers to a different directory.
func (w *Watcher) Watch(dir string) error {
	var st unix.Stat_t
	if err := unix.Stat(dir, &st); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return unix.EBADF
	}
	if w.fd >= 0 && w.dev == st.Dev && w.ino == st.Ino {
		return nil
	}

	// O_EVTONLY: the handle only receives events and does not keep the
	// volume busy, so external drives can still be ejected.
	fd, err := unix.Open(dir, unix.O_EVTONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	var ev unix.Kevent_t
	unix.SetKevent(&ev, fd, unix.EVFILT_VNODE, unix.EV_ADD|unix.EV_CLEAR|unix.EV_ENABLE)
	ev.Fflags = unix.NOTE_WRITE | unix.NOTE_EXTEND | unix.NOTE_ATTRIB | unix.NOTE_LINK |
		unix.NOTE_DELETE | unix.NOTE_RENAME | unix.NOTE_REVOKE
	if _, err := unix.Kevent(w.kq, []unix.Kevent_t{ev}, nil, nil); err != nil {
		unix.Close(fd)
		return err
	}
	if w.fd >= 0 {
		unix.Close(w.fd) // also removes its kevent
	}
	w.fd, w.dev, w.ino = fd, st.Dev, st.Ino
	return nil
}

// Close stops the watcher and releases its handles.
func (w *Watcher) Close() (err error) {
	w.once.Do(func() {
		w.mu.Lock()
		w.closed = true
		w.mu.Unlock()
		var ev unix.Kevent_t
		unix.SetKevent(&ev, closeIdent, unix.EVFILT_USER, 0)
		ev.Fflags = unix.NOTE_TRIGGER
		_, err = unix.Kevent(w.kq, []unix.Kevent_t{ev}, nil, nil)
	})
	return err
}

func (w *Watcher) loop() {
	defer func() {
		w.mu.Lock()
		if w.fd >= 0 {
			unix.Close(w.fd)
			w.fd = -1
		}
		w.mu.Unlock()
		unix.Close(w.kq)
		close(w.c)
	}()
	events := make([]unix.Kevent_t, 8)
	for {
		n, err := unix.Kevent(w.kq, nil, events, nil)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return
		}
		changed := false
		for _, ev := range events[:n] {
			if ev.Filter == unix.EVFILT_USER {
				return
			}
			changed = true
		}
		if changed {
			notify(w.c)
		}
	}
}
