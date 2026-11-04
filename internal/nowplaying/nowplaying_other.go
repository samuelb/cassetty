//go:build !darwin && !linux && !freebsd && !netbsd && !openbsd && !dragonfly

package nowplaying

// Main runs f.
func Main(f func()) { f() }

// Session is unavailable on this platform.
type Session struct {
	C <-chan Command
}

// Start always fails with ErrUnsupported on this platform.
func Start() (*Session, error) { return nil, ErrUnsupported }

func (s *Session) Update(Info) {}

func (s *Session) Close() {}
