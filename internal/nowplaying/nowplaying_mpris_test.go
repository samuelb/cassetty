//go:build linux || freebsd || netbsd || openbsd || dragonfly

package nowplaying

import (
	"bufio"
	"encoding/xml"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
)

const busConfig = `<!DOCTYPE busconfig PUBLIC "-//freedesktop//DTD D-Bus Bus Configuration 1.0//EN"
 "http://www.freedesktop.org/standards/dbus/1.0/busconfig.dtd">
<busconfig>
  <type>session</type>
  <listen>unix:path=%s</listen>
  <policy context="default">
    <allow send_destination="*" eavesdrop="true"/>
    <allow eavesdrop="true"/>
    <allow own="*"/>
  </policy>
</busconfig>`

// privateBus runs a session bus for the test alone, so that test players
// stay off the desktop, and points Start at it. It returns the bus address.
func privateBus(t *testing.T) string {
	t.Helper()
	daemon, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon not found")
	}
	dir := t.TempDir()
	conf := filepath.Join(dir, "bus.conf")
	if err := os.WriteFile(conf, fmt.Appendf(nil, busConfig, filepath.Join(dir, "bus")), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(daemon, "--config-file="+conf, "--nofork", "--nopidfile", "--print-address")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	addr, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatalf("dbus-daemon printed no address: %v", err)
	}
	addr = strings.TrimSpace(addr)
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", addr)
	return addr
}

// client is an MPRIS client on the test's bus.
type client struct {
	t    *testing.T
	conn *dbus.Conn
	obj  dbus.BusObject
	sigs chan *dbus.Signal
}

func newClient(t *testing.T, addr string) *client {
	t.Helper()
	conn, err := dbus.Connect(addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := conn.AddMatchSignal(dbus.WithMatchObjectPath(objectPath)); err != nil {
		t.Fatal(err)
	}
	c := &client{t: t, conn: conn, obj: conn.Object(busName, objectPath), sigs: make(chan *dbus.Signal, 64)}
	conn.Signal(c.sigs)
	return c
}

func (c *client) call(method string, args ...any) error {
	return c.obj.Call(method, 0, args...).Err
}

func (c *client) must(method string, args ...any) {
	c.t.Helper()
	if err := c.call(method, args...); err != nil {
		c.t.Fatalf("%s: %v", method, err)
	}
}

func (c *client) set(name string, value any) error {
	return c.call(ifaceProps+".Set", ifacePlayer, name, dbus.MakeVariant(value))
}

func (c *client) getAll(iface string) map[string]dbus.Variant {
	c.t.Helper()
	var props map[string]dbus.Variant
	if err := c.obj.Call(ifaceProps+".GetAll", 0, iface).Store(&props); err != nil {
		c.t.Fatalf("GetAll %s: %v", iface, err)
	}
	return props
}

func (c *client) get(name string) any {
	c.t.Helper()
	var v dbus.Variant
	if err := c.obj.Call(ifaceProps+".Get", 0, ifacePlayer, name).Store(&v); err != nil {
		c.t.Fatalf("Get %s: %v", name, err)
	}
	return v.Value()
}

// signal waits for the next signal called name.
func (c *client) signal(name string) *dbus.Signal {
	c.t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case sig := <-c.sigs:
			if sig.Name == name {
				return sig
			}
		case <-timeout:
			c.t.Fatalf("no %s signal", name)
		}
	}
}

func expect(t *testing.T, s *Session, want Command) {
	t.Helper()
	select {
	case got := <-s.C:
		if got != want {
			t.Errorf("command %+v, want %+v", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("no command, want %+v", want)
	}
}

func startSession(t *testing.T) *Session {
	t.Helper()
	s, err := Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

// song has a file name in Latin-1, as old rips do: invalid UTF-8, which
// D-Bus does not carry.
var song = Info{
	Path:     "/music/Band/Album/01 F\xfcr Elise.flac",
	Title:    "Für Elise",
	Artist:   "Band",
	Album:    "Album",
	Length:   3 * time.Minute,
	Position: 10 * time.Second,
	State:    Playing,
	Volume:   0.5,
	Shuffle:  true,
	Repeat:   RepeatAll,
}

func TestMPRIS(t *testing.T) {
	addr := privateBus(t)
	s := startSession(t)
	c := newClient(t, addr)
	if s.name != busName {
		t.Errorf("owns %s, want %s", s.name, busName)
	}

	// Without a track the player can do nothing, and ignores controls.
	if c.get("CanPlay") != false {
		t.Error("CanPlay without a track")
	}
	c.must(ifacePlayer + ".PlayPause")

	s.Update(song)
	changed := c.signal(ifaceProps + ".PropertiesChanged").Body
	if changed[0] != ifacePlayer {
		t.Errorf("PropertiesChanged for %v", changed[0])
	}
	diff := changed[1].(map[string]dbus.Variant)
	for name, want := range map[string]any{
		"PlaybackStatus": "Playing",
		"CanPlay":        true,
		"Volume":         0.5,
		"Shuffle":        true,
		"LoopStatus":     "Playlist",
	} {
		if got := diff[name].Value(); got != want {
			t.Errorf("changed %s = %v, want %v", name, got, want)
		}
	}
	for _, name := range []string{"Position", "Rate", "CanControl"} {
		if _, ok := diff[name]; ok {
			t.Errorf("%s signalled as changed", name)
		}
	}

	props := c.getAll(ifacePlayer)
	if pos := props["Position"].Value().(int64); pos < 10e6 || pos > 15e6 {
		t.Errorf("Position %d, want 10s on", pos)
	}
	md := props["Metadata"].Value().(map[string]dbus.Variant)
	for key, want := range map[string]any{
		"mpris:trackid": trackID(song.Path),
		"mpris:length":  int64(180e6),
		"xesam:title":   "Für Elise",
		"xesam:album":   "Album",
		"xesam:url":     "file:///music/Band/Album/01%20F%FCr%20Elise.flac",
	} {
		if got := md[key].Value(); got != want {
			t.Errorf("metadata %s = %v, want %v", key, got, want)
		}
	}
	if got := md["xesam:artist"].Value(); fmt.Sprint(got) != "[Band]" {
		t.Errorf("metadata xesam:artist = %v", got)
	}

	for method, want := range map[string]Kind{
		"PlayPause": TogglePlayPause,
		"Play":      Play,
		"Pause":     Pause,
		"Stop":      Stop,
		"Next":      Next,
		"Previous":  Previous,
	} {
		c.must(ifacePlayer + "." + method)
		expect(t, s, Command{Kind: want})
	}
	c.must(ifacePlayer+".Seek", int64(-5e6))
	expect(t, s, Command{Kind: SeekBy, Offset: -5 * time.Second})
	c.must(ifacePlayer+".SetPosition", trackID(song.Path), int64(60e6))
	expect(t, s, Command{Kind: Seek, Position: time.Minute})
	// Stale or out of range: ignored, so Stop comes next.
	c.must(ifacePlayer+".SetPosition", trackID("/other.flac"), int64(60e6))
	c.must(ifacePlayer+".SetPosition", trackID(song.Path), int64(-1))
	c.must(ifacePlayer+".SetPosition", trackID(song.Path), int64(181e6))
	c.must(ifacePlayer + ".Stop")
	expect(t, s, Command{Kind: Stop})
	if err := c.call(ifacePlayer+".OpenUri", "file:///music/x.flac"); err == nil {
		t.Error("OpenUri succeeded")
	}
	c.must(ifaceRoot + ".Raise")
	c.must(ifaceRoot + ".Quit")
	expect(t, s, Command{Kind: Quit})

	for _, set := range []struct {
		name  string
		value any
		want  Command
	}{
		{"Volume", 0.25, Command{Kind: SetVolume, Volume: 0.25}},
		{"Volume", -1.0, Command{Kind: SetVolume, Volume: 0}},
		{"Volume", 2.0, Command{Kind: SetVolume, Volume: 1}},
		{"Shuffle", false, Command{Kind: SetShuffle, Shuffle: false}},
		{"LoopStatus", "Track", Command{Kind: SetRepeat, Repeat: RepeatOne}},
		{"LoopStatus", "None", Command{Kind: SetRepeat, Repeat: RepeatOff}},
		{"Rate", 0.0, Command{Kind: Pause}},
	} {
		if err := c.set(set.name, set.value); err != nil {
			t.Errorf("set %s to %v: %v", set.name, set.value, err)
			continue
		}
		expect(t, s, set.want)
	}
	for _, bad := range []struct {
		name  string
		value any
		err   string
	}{
		{"LoopStatus", "Sometimes", "InvalidArgs"},
		{"Volume", "loud", "InvalidArgs"},
		{"PlaybackStatus", "Paused", "PropertyReadOnly"},
		{"Bogus", true, "UnknownProperty"},
	} {
		err := c.set(bad.name, bad.value)
		if e, ok := err.(dbus.Error); !ok || e.Name != "org.freedesktop.DBus.Error."+bad.err {
			t.Errorf("set %s to %v: %v, want %s", bad.name, bad.value, err, bad.err)
		}
	}
	if err := c.call(ifaceProps+".Set", ifaceRoot, "Identity", dbus.MakeVariant("x")); err == nil {
		t.Error("set Identity succeeded")
	}

	// A seek within the track is a Seeked signal, not a property change.
	seeked := song
	seeked.Position = 2 * time.Minute
	s.Update(seeked)
	if pos := c.signal(ifacePlayer + ".Seeked").Body[0]; pos != int64(120e6) {
		t.Errorf("Seeked to %v", pos)
	}
}

// TestIntrospection checks that the introspection data names the properties
// the player has, with their types.
func TestIntrospection(t *testing.T) {
	addr := privateBus(t)
	startSession(t)
	c := newClient(t, addr)
	var data string
	if err := c.obj.Call("org.freedesktop.DBus.Introspectable.Introspect", 0).Store(&data); err != nil {
		t.Fatal(err)
	}
	var node introspect.Node
	if err := xml.Unmarshal([]byte(data), &node); err != nil {
		t.Fatal(err)
	}
	writable := map[string]bool{"LoopStatus": true, "Rate": true, "Shuffle": true, "Volume": true}
	for _, iface := range []string{ifaceRoot, ifacePlayer} {
		props := c.getAll(iface)
		declared := 0
		for _, i := range node.Interfaces {
			if i.Name != iface {
				continue
			}
			for _, p := range i.Properties {
				declared++
				v, ok := props[p.Name]
				if !ok {
					t.Errorf("%s.%s is declared but missing", iface, p.Name)
					continue
				}
				if sig := v.Signature().String(); sig != p.Type {
					t.Errorf("%s.%s has type %s, declared %s", iface, p.Name, sig, p.Type)
				}
				want := "read"
				if iface == ifacePlayer && writable[p.Name] {
					want = "readwrite"
				}
				if p.Access != want {
					t.Errorf("%s.%s is declared %s, want %s", iface, p.Name, p.Access, want)
				}
			}
		}
		if declared != len(props) {
			t.Errorf("%s declares %d properties, has %d", iface, declared, len(props))
		}
	}
}

func TestSecondInstance(t *testing.T) {
	addr := privateBus(t)
	startSession(t)
	s := startSession(t)
	if want := fmt.Sprintf("%s.instance%d", busName, os.Getpid()); s.name != want {
		t.Errorf("second instance owns %s, want %s", s.name, want)
	}
	c := newClient(t, addr)
	s.Close()
	var has bool
	if err := c.conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, s.name).Store(&has); err != nil {
		t.Fatal(err)
	}
	if has {
		t.Error("Close kept the bus name")
	}
}

func TestNoBus(t *testing.T) {
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+filepath.Join(t.TempDir(), "none"))
	if _, err := Start(); err == nil {
		t.Error("Start succeeded without a bus")
	}
}

func TestMicros(t *testing.T) {
	if got := micros(-5); got != -5*time.Microsecond {
		t.Errorf("micros(-5) = %v", got)
	}
	// The player adds offsets to positions: twice the largest offset must
	// not overflow, yet cross any track.
	for _, us := range []int64{math.MaxInt64, math.MinInt64} {
		if d := micros(us); (d > 0) != (us > 0) || (d+d)/2 != d || d.Abs() < 10*365*24*time.Hour {
			t.Errorf("micros(%d) = %v", us, d)
		}
	}
}

func TestTrackID(t *testing.T) {
	for _, path := range []string{"", "/music/a.flac", "/music/F\xfcr Elise.flac"} {
		if id := trackID(path); !id.IsValid() || strings.HasPrefix(string(id), "/org/mpris") != (path == "") {
			t.Errorf("trackID(%q) = %s", path, id)
		}
	}
	if trackID("/a.flac") == trackID("/b.flac") {
		t.Error("two tracks share an ID")
	}
}
