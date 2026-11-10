package watch

import (
	"os"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Watcher watches a single directory with inotify.
type Watcher struct {
	// C receives a value after the watched directory changed. Bursts of
	// changes are coalesced. C is closed when the watcher is closed.
	C <-chan struct{}

	c  chan struct{}
	f  *os.File // the inotify instance; closing it ends the loop
	fd int
	mu sync.Mutex
	wd int // current watch descriptor, -1 if none
}

const mask = unix.IN_CREATE | unix.IN_DELETE | unix.IN_MOVED_FROM | unix.IN_MOVED_TO |
	unix.IN_CLOSE_WRITE | unix.IN_ATTRIB | unix.IN_DELETE_SELF | unix.IN_MOVE_SELF | unix.IN_ONLYDIR

// New starts a watcher that is not watching anything yet.
func New() (*Watcher, error) {
	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		return nil, err
	}
	c := make(chan struct{}, 1)
	// A non-blocking fd wrapped in an os.File uses the runtime poller, so
	// Close interrupts the pending Read.
	w := &Watcher{C: c, c: c, f: os.NewFile(uintptr(fd), "inotify"), fd: fd, wd: -1}
	go w.loop()
	return w, nil
}

// Watch makes dir the watched directory, replacing the previous one.
func (w *Watcher) Watch(dir string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	// Adding a watch for an inode that is already watched returns the same
	// descriptor, so re-watching the current directory is cheap.
	wd, err := unix.InotifyAddWatch(w.fd, dir, mask)
	if err != nil {
		return err
	}
	if w.wd >= 0 && w.wd != wd {
		// The folder watched before may be gone, and its watch with it.
		_, _ = unix.InotifyRmWatch(w.fd, uint32(w.wd))
	}
	w.wd = wd
	return nil
}

// Close stops the watcher and releases its handles.
func (w *Watcher) Close() error { return w.f.Close() }

func (w *Watcher) loop() {
	defer close(w.c)
	buf := make([]byte, 64*1024)
	for {
		n, err := w.f.Read(buf)
		if err != nil {
			return
		}
		w.mu.Lock()
		wd := w.wd
		w.mu.Unlock()
		changed := false
		for off := 0; off+unix.SizeofInotifyEvent <= n; {
			ev := (*unix.InotifyEvent)(unsafe.Pointer(&buf[off]))
			switch {
			case ev.Mask&unix.IN_Q_OVERFLOW != 0:
				changed = true
			case int(ev.Wd) == wd && ev.Mask&unix.IN_IGNORED == 0:
				changed = true
			}
			off += unix.SizeofInotifyEvent + int(ev.Len)
		}
		if changed {
			notify(w.c)
		}
	}
}
