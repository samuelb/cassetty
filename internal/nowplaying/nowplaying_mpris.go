//go:build linux || freebsd || netbsd || openbsd || dragonfly

package nowplaying

// MPRIS, the D-Bus interface of media players:
// https://specifications.freedesktop.org/mpris-spec/latest/

import (
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"net/url"
	"os"
	"reflect"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
)

// Main runs f. MPRIS needs nothing of the main thread.
func Main(f func()) { f() }

const (
	busName     = "org.mpris.MediaPlayer2.cassetty"
	objectPath  = dbus.ObjectPath("/org/mpris/MediaPlayer2")
	ifaceRoot   = "org.mpris.MediaPlayer2"
	ifacePlayer = "org.mpris.MediaPlayer2.Player"
	ifaceProps  = "org.freedesktop.DBus.Properties"

	noTrack = dbus.ObjectPath("/org/mpris/MediaPlayer2/TrackList/NoTrack")
)

// Session is an MPRIS player on the session bus: it receives the commands of
// the desktop's media controls and publishes the loaded track. Update and
// Close must not be called concurrently.
type Session struct {
	// C receives the commands.
	C <-chan Command

	c       chan Command
	conn    *dbus.Conn
	name    string  // the bus name owned
	tracker tracker // used by Update only

	mu   sync.Mutex
	info Info      // as of the last Update
	at   time.Time // when info came
}

// Start connects to the session bus and registers the player. It fails
// without a session bus; it does not start one.
func Start() (*Session, error) {
	conn, err := dbus.SessionBusPrivateNoAutoStartup()
	if err != nil {
		return nil, err
	}
	s, err := start(conn)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return s, nil
}

func start(conn *dbus.Conn) (*Session, error) {
	if err := conn.Auth(nil); err != nil {
		return nil, err
	}
	if err := conn.Hello(); err != nil {
		return nil, err
	}
	c := make(chan Command, 8)
	s := &Session{C: c, c: c, conn: conn, at: time.Now()}

	// Methods run on goroutines of their own, possibly several at once.
	do := func(k Kind) func() *dbus.Error {
		return func() *dbus.Error {
			s.control(Command{Kind: k})
			return nil
		}
	}
	root := map[string]any{
		"Raise": func() *dbus.Error { return nil }, // a terminal cannot be raised
		"Quit": func() *dbus.Error {
			s.send(Command{Kind: Quit})
			return nil
		},
	}
	player := map[string]any{
		"Next":      do(Next),
		"Previous":  do(Previous),
		"Pause":     do(Pause),
		"PlayPause": do(TogglePlayPause),
		"Stop":      do(Stop),
		"Play":      do(Play),
		"Seek": func(offset int64) *dbus.Error {
			s.control(Command{Kind: SeekBy, Offset: micros(offset)})
			return nil
		},
		"SetPosition": func(track dbus.ObjectPath, position int64) *dbus.Error {
			info, _ := s.now()
			pos := micros(position)
			// The spec has calls ignored that name another track or a
			// position outside this one.
			if track == trackID(info.Path) && pos >= 0 && (info.Length == 0 || pos <= info.Length) {
				s.control(Command{Kind: Seek, Position: pos})
			}
			return nil
		},
		"OpenUri": func(string) *dbus.Error {
			return dbus.MakeFailedError(errors.New("cassetty opens no URIs"))
		},
	}
	for _, err := range []error{
		conn.Export(introspect.Introspectable(introspection), objectPath, "org.freedesktop.DBus.Introspectable"),
		conn.Export(properties{s}, objectPath, ifaceProps),
		conn.ExportMethodTable(root, objectPath, ifaceRoot),
		conn.ExportMethodTable(player, objectPath, ifacePlayer),
	} {
		if err != nil {
			return nil, err
		}
	}

	// Another cassetty may own the name already. The spec has each further
	// instance append ".instance<pid>".
	for _, name := range []string{busName, fmt.Sprintf("%s.instance%d", busName, os.Getpid())} {
		reply, err := conn.RequestName(name, dbus.NameFlagDoNotQueue)
		if err != nil {
			return nil, err
		}
		if reply == dbus.RequestNameReplyPrimaryOwner {
			s.name = name
			return s, nil
		}
	}
	return nil, fmt.Errorf("the bus name %s is taken", busName)
}

// send passes c on to the player, unless it is falling behind.
func (s *Session) send(c Command) {
	select {
	case s.c <- c:
	default:
	}
}

// control passes on c, which needs a loaded track: without one the player
// tells that it can neither play nor pause, seek or skip, and the spec has
// such calls ignored.
func (s *Session) control(c Command) {
	if info, _ := s.now(); info.Path != "" {
		s.send(c)
	}
}

// now returns the last Update and the position it implies by now.
func (s *Session) now() (Info, time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pos := s.info.Position
	if s.info.State == Playing {
		pos += time.Since(s.at)
		if s.info.Length > 0 {
			pos = min(pos, s.info.Length)
		}
	}
	return s.info, pos
}

// Update publishes info. It is cheap to call after every change of the
// player: only changes clients cannot work out by themselves are signalled.
func (s *Session) Update(info Info) {
	now := time.Now()
	s.mu.Lock()
	old := s.info
	s.info, s.at = info, now
	s.mu.Unlock()

	changed, jumped := s.tracker.update(info, now)
	if !changed {
		return
	}
	// Every change but the position's was published, so old, the last
	// Update, holds what clients know.
	if diff := changes(playerProps(old, 0), playerProps(info, 0)); len(diff) > 0 {
		_ = s.conn.Emit(objectPath, ifaceProps+".PropertiesChanged", ifacePlayer, diff, []string{})
	}
	// Clients advance the position of a playing track by themselves; they
	// learn of jumps within the track from Seeked.
	if jumped && info.Path != "" && info.Path == old.Path {
		_ = s.conn.Emit(objectPath, ifacePlayer+".Seeked", info.Position.Microseconds())
	}
}

// Close leaves the session bus, which withdraws the player.
func (s *Session) Close() {
	_, _ = s.conn.ReleaseName(s.name)
	_ = s.conn.Close()
}

// changes returns the properties of now that differ from those of was.
func changes(was, now map[string]dbus.Variant) map[string]dbus.Variant {
	diff := map[string]dbus.Variant{}
	for k, v := range now {
		if !reflect.DeepEqual(was[k].Value(), v.Value()) {
			diff[k] = v
		}
	}
	return diff
}

func rootProps() map[string]dbus.Variant {
	return map[string]dbus.Variant{
		"CanQuit":             dbus.MakeVariant(true),
		"CanRaise":            dbus.MakeVariant(false),
		"HasTrackList":        dbus.MakeVariant(false),
		"Identity":            dbus.MakeVariant("cassetty"),
		"SupportedUriSchemes": dbus.MakeVariant([]string{}),
		"SupportedMimeTypes":  dbus.MakeVariant([]string{}),
	}
}

// playerProps returns the properties of the player interface for info at
// position pos.
func playerProps(info Info, pos time.Duration) map[string]dbus.Variant {
	status := "Stopped"
	switch info.State {
	case Playing:
		status = "Playing"
	case Paused:
		status = "Paused"
	}
	loaded := dbus.MakeVariant(info.Path != "")
	return map[string]dbus.Variant{
		"PlaybackStatus": dbus.MakeVariant(status),
		"LoopStatus":     dbus.MakeVariant(loopStatus[info.Repeat]),
		"Rate":           dbus.MakeVariant(1.0),
		"Shuffle":        dbus.MakeVariant(info.Shuffle),
		"Metadata":       dbus.MakeVariant(metadata(info)),
		"Volume":         dbus.MakeVariant(info.Volume),
		"Position":       dbus.MakeVariant(pos.Microseconds()),
		"MinimumRate":    dbus.MakeVariant(1.0),
		"MaximumRate":    dbus.MakeVariant(1.0),
		"CanGoNext":      loaded,
		"CanGoPrevious":  loaded,
		"CanPlay":        loaded,
		"CanPause":       loaded,
		"CanSeek":        loaded,
		"CanControl":     dbus.MakeVariant(true),
	}
}

var loopStatus = map[Repeat]string{RepeatOff: "None", RepeatAll: "Playlist", RepeatOne: "Track"}

func metadata(info Info) map[string]dbus.Variant {
	md := map[string]dbus.Variant{"mpris:trackid": dbus.MakeVariant(trackID(info.Path))}
	if info.Path == "" {
		return md
	}
	md["xesam:url"] = dbus.MakeVariant((&url.URL{Scheme: "file", Path: info.Path}).String())
	if info.Title != "" {
		md["xesam:title"] = dbus.MakeVariant(text(info.Title))
	}
	if info.Artist != "" {
		md["xesam:artist"] = dbus.MakeVariant([]string{text(info.Artist)})
	}
	if info.Album != "" {
		md["xesam:album"] = dbus.MakeVariant(text(info.Album))
	}
	if info.Length > 0 {
		md["mpris:length"] = dbus.MakeVariant(info.Length.Microseconds())
	}
	return md
}

// trackID names the track at path for SetPosition. The spec reserves paths
// below /org/mpris.
func trackID(path string) dbus.ObjectPath {
	if path == "" {
		return noTrack
	}
	h := fnv.New64a()
	h.Write([]byte(path))
	return dbus.ObjectPath(fmt.Sprintf("/io/cassetty/track/%016x", h.Sum64()))
}

// micros converts MPRIS microseconds to a Duration. It saturates instead of
// overflowing, at a quarter of the range, which is still decades: the player
// adds offsets to positions.
func micros(us int64) time.Duration {
	const lim = math.MaxInt64 / 4 / int64(time.Microsecond)
	return time.Duration(min(lim, max(-lim, us))) * time.Microsecond
}

// properties implements org.freedesktop.DBus.Properties.
type properties struct{ s *Session }

func (p properties) GetAll(iface string) (map[string]dbus.Variant, *dbus.Error) {
	switch iface {
	case ifaceRoot:
		return rootProps(), nil
	case ifacePlayer:
		return playerProps(p.s.now()), nil
	}
	return nil, dbusError("UnknownInterface", "no interface %s", iface)
}

func (p properties) Get(iface, name string) (dbus.Variant, *dbus.Error) {
	props, err := p.GetAll(iface)
	if err != nil {
		return dbus.Variant{}, err
	}
	v, ok := props[name]
	if !ok {
		return dbus.Variant{}, dbusError("UnknownProperty", "no property %s.%s", iface, name)
	}
	return v, nil
}

func (p properties) Set(iface, name string, value dbus.Variant) *dbus.Error {
	if _, err := p.Get(iface, name); err != nil {
		return err
	}
	if iface != ifacePlayer {
		return dbusError("PropertyReadOnly", "%s is read-only", name)
	}
	invalid := dbusError("InvalidArgs", "invalid %s: %s", name, value)
	switch name {
	case "Volume":
		v, ok := value.Value().(float64)
		if !ok || math.IsNaN(v) {
			return invalid
		}
		// Below 0 means 0, says the spec; cassetty does not amplify.
		p.s.send(Command{Kind: SetVolume, Volume: max(0, min(1, v))})
	case "Shuffle":
		on, ok := value.Value().(bool)
		if !ok {
			return invalid
		}
		p.s.send(Command{Kind: SetShuffle, Shuffle: on})
	case "LoopStatus":
		status, _ := value.Value().(string)
		for r, st := range loopStatus {
			if st == status {
				p.s.send(Command{Kind: SetRepeat, Repeat: r})
				return nil
			}
		}
		return invalid
	case "Rate":
		// Only 1 is supported. The spec has 0 act as Pause and leaves other
		// rates to be ignored.
		rate, ok := value.Value().(float64)
		if !ok {
			return invalid
		}
		if rate == 0 {
			p.s.control(Command{Kind: Pause})
		}
	default:
		return dbusError("PropertyReadOnly", "%s is read-only", name)
	}
	return nil
}

func dbusError(name, format string, args ...any) *dbus.Error {
	return dbus.NewError("org.freedesktop.DBus.Error."+name, []any{fmt.Sprintf(format, args...)})
}

const introspection = `<!DOCTYPE node PUBLIC "-//freedesktop//DTD D-BUS Object Introspection 1.0//EN"
 "http://www.freedesktop.org/standards/dbus/1.0/introspect.dtd">
<node>
  <interface name="org.freedesktop.DBus.Introspectable">
    <method name="Introspect">
      <arg name="xml_data" type="s" direction="out"/>
    </method>
  </interface>
  <interface name="org.freedesktop.DBus.Peer">
    <method name="Ping"/>
    <method name="GetMachineId">
      <arg name="machine_uuid" type="s" direction="out"/>
    </method>
  </interface>
  <interface name="org.freedesktop.DBus.Properties">
    <method name="Get">
      <arg name="interface_name" type="s" direction="in"/>
      <arg name="property_name" type="s" direction="in"/>
      <arg name="value" type="v" direction="out"/>
    </method>
    <method name="GetAll">
      <arg name="interface_name" type="s" direction="in"/>
      <arg name="properties" type="a{sv}" direction="out"/>
    </method>
    <method name="Set">
      <arg name="interface_name" type="s" direction="in"/>
      <arg name="property_name" type="s" direction="in"/>
      <arg name="value" type="v" direction="in"/>
    </method>
    <signal name="PropertiesChanged">
      <arg name="interface_name" type="s"/>
      <arg name="changed_properties" type="a{sv}"/>
      <arg name="invalidated_properties" type="as"/>
    </signal>
  </interface>
  <interface name="org.mpris.MediaPlayer2">
    <method name="Raise"/>
    <method name="Quit"/>
    <property name="CanQuit" type="b" access="read"/>
    <property name="CanRaise" type="b" access="read"/>
    <property name="HasTrackList" type="b" access="read"/>
    <property name="Identity" type="s" access="read"/>
    <property name="SupportedUriSchemes" type="as" access="read"/>
    <property name="SupportedMimeTypes" type="as" access="read"/>
  </interface>
  <interface name="org.mpris.MediaPlayer2.Player">
    <method name="Next"/>
    <method name="Previous"/>
    <method name="Pause"/>
    <method name="PlayPause"/>
    <method name="Stop"/>
    <method name="Play"/>
    <method name="Seek">
      <arg name="Offset" type="x" direction="in"/>
    </method>
    <method name="SetPosition">
      <arg name="TrackId" type="o" direction="in"/>
      <arg name="Position" type="x" direction="in"/>
    </method>
    <method name="OpenUri">
      <arg name="Uri" type="s" direction="in"/>
    </method>
    <signal name="Seeked">
      <arg name="Position" type="x"/>
    </signal>
    <property name="PlaybackStatus" type="s" access="read"/>
    <property name="LoopStatus" type="s" access="readwrite"/>
    <property name="Rate" type="d" access="readwrite"/>
    <property name="Shuffle" type="b" access="readwrite"/>
    <property name="Metadata" type="a{sv}" access="read"/>
    <property name="Volume" type="d" access="readwrite"/>
    <property name="Position" type="x" access="read">
      <annotation name="org.freedesktop.DBus.Property.EmitsChangedSignal" value="false"/>
    </property>
    <property name="MinimumRate" type="d" access="read"/>
    <property name="MaximumRate" type="d" access="read"/>
    <property name="CanGoNext" type="b" access="read"/>
    <property name="CanGoPrevious" type="b" access="read"/>
    <property name="CanPlay" type="b" access="read"/>
    <property name="CanPause" type="b" access="read"/>
    <property name="CanSeek" type="b" access="read"/>
    <property name="CanControl" type="b" access="read"/>
  </interface>
</node>`
