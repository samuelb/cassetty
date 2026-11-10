package audio

import (
	"testing"
	"time"
)

// How long the motor takes to get up to speed and to wind down to a stop.
var (
	startTime = time.Duration(tapeStart * float64(time.Second))
	stopTime  = time.Duration(tapeStop * float64(time.Second))
)

func TestTheTapeGetsUpToSpeedAndWindsDown(t *testing.T) {
	// The tape sound is off: the motor eases all the same.
	e, _ := reelEngine(60*time.Second, false)
	e.SetPlaying(true)
	up := e.pull(startTime)
	if e.Speed() != 1 {
		t.Fatalf("speed %v after getting up to speed", e.Speed())
	}
	// The music speeds up from a stop, getting louder, and as much of it
	// plays as would in half the time.
	if got := e.Position(); !near(got, startTime/2, readAhead) {
		t.Errorf("the head is at %v after getting up to speed, want %v", got, startTime/2)
	}
	if early, late := rms(up[:len(up)/5]), rms(up[len(up)*4/5:]); early > late/3 {
		t.Errorf("getting up to speed should fade in: early %.3f, late %.3f", early, late)
	}

	full := rms(e.pull(100 * time.Millisecond))
	from := e.Position()
	e.SetPlaying(false)
	down := e.pull(stopTime + 10*time.Millisecond)
	if e.Speed() != 0 || !e.deck.idle() {
		t.Fatalf("speed %v after winding down, idle %v", e.Speed(), e.deck.idle())
	}
	if got := e.Position() - from; !near(got, stopTime/2, readAhead) {
		t.Errorf("the head moved on by %v while winding down, want %v", got, stopTime/2)
	}
	if early, late := rms(down[:len(down)/5]), rms(down[len(down)*4/5:]); early > full || late > early/3 {
		t.Errorf("winding down should fade out: full %.3f, early %.3f, late %.3f", full, early, late)
	}
}

func TestStoppingWindsTheTapeDownWhereItWas(t *testing.T) {
	e, r := reelEngine(60*time.Second, true)
	full := rms(e.pull(time.Second))

	// STOP takes the tape back to the start: the head is there at once,
	// and the tape is heard winding down where it was.
	e.SetPlaying(false)
	if err := e.Seek(0); err != nil {
		t.Fatal(err)
	}
	if e.Position() != 0 || e.Speed() < 0.99 || e.deck.idle() {
		t.Fatalf("the head is at %v after stopping, the tape at a speed of %v", e.Position(), e.Speed())
	}
	down := e.pull(stopTime + 10*time.Millisecond)
	if got := SampleRate.D(int(down[0][0] * float64(r.frames))); !near(got, time.Second, readAhead) {
		t.Errorf("the tape winds down from %v, want 1s", got)
	}
	if early, late := rms(down[:len(down)/5]), rms(down[len(down)*4/5:]); early < full/2 || late > early/3 {
		t.Errorf("winding down should fade out: full %.3f, early %.3f, late %.3f", full, early, late)
	}
	if e.Speed() != 0 || !e.deck.idle() || e.Position() != 0 {
		t.Fatalf("speed %v at %v after winding down, idle %v", e.Speed(), e.Position(), e.deck.idle())
	}

	e.SetPlaying(true)
	e.upToSpeed()
	if f := e.pull(10 * time.Millisecond)[0]; f != r.frame(0) {
		t.Errorf("the tape plays %v after stopping, not the start", f)
	}
}

func TestPlayingWhileTheTapeWindsDownStartsOver(t *testing.T) {
	e, _ := reelEngine(60*time.Second, true)
	e.pull(time.Second)
	e.SetPlaying(false)
	e.Seek(0)
	e.pull(50 * time.Millisecond)

	// The tape is heard winding down to the end where it was, while the
	// deck gets up to speed from the start.
	e.SetPlaying(true)
	e.pull(startTime)
	if e.Speed() != 1 || len(e.deck.tail) != 0 {
		t.Fatalf("speed %v after getting up to speed, with %d frames of winding down to come", e.Speed(), len(e.deck.tail))
	}
	if got := e.Position(); !near(got, startTime/2, readAhead) {
		t.Errorf("the head is at %v after getting up to speed, want %v", got, startTime/2)
	}
}

func TestEjectingWindsTheTapeDown(t *testing.T) {
	e, _ := reelEngine(60*time.Second, true)
	full := rms(e.pull(time.Second))
	e.Eject()
	if e.Speed() < 0.99 || e.deck.idle() {
		t.Fatalf("the tape stops at once when it is ejected, at a speed of %v", e.Speed())
	}
	down := e.pull(stopTime + 10*time.Millisecond)
	if early, late := rms(down[:len(down)/5]), rms(down[len(down)*4/5:]); early < full/2 || late > early/3 {
		t.Errorf("winding down should fade out: full %.3f, early %.3f, late %.3f", full, early, late)
	}
	if e.Speed() != 0 || !e.deck.idle() {
		t.Errorf("speed %v after the tape came out, idle %v", e.Speed(), e.deck.idle())
	}
}

func TestTheDeckGoesOnToAnotherTrackAtSpeedOnlyWhilePlaying(t *testing.T) {
	e, _ := reelEngine(60*time.Second, true)
	e.pull(time.Second)

	// While it plays, the next track plays at once, as on the same tape.
	src, r := reelSource(60 * time.Second)
	e.Start(src, true)
	if e.Speed() != 1 || len(e.deck.tail) != 0 {
		t.Fatalf("the deck goes on to the next track at a speed of %v", e.Speed())
	}
	if f := e.pull(10 * time.Millisecond)[0]; f != r.frame(0) {
		t.Errorf("the next track starts with %v", f)
	}

	// While it winds down, the tape in the deck winds down to the end, and
	// the next one gets up to speed.
	e.SetPlaying(false)
	e.pull(50 * time.Millisecond)
	src, _ = reelSource(60 * time.Second)
	e.Start(src, true)
	if e.Speed() != 0 || len(e.deck.tail) == 0 {
		t.Fatalf("the deck goes on to the next track at a speed of %v, with %d frames of winding down", e.Speed(), len(e.deck.tail))
	}
	e.pull(startTime)
	if got := e.Position(); e.Speed() != 1 || !near(got, startTime/2, readAhead) {
		t.Errorf("the head is at %v at a speed of %v after getting up to speed", got, e.Speed())
	}

	// And a track that is loaded to stand in the deck does not play, but
	// the one that played is heard winding down.
	src, _ = reelSource(60 * time.Second)
	e.Start(src, false)
	if len(e.deck.tail) == 0 {
		t.Fatal("the track that played stops at once")
	}
	e.pull(stopTime + 10*time.Millisecond)
	if e.Speed() != 0 || !e.deck.idle() || e.Position() != 0 {
		t.Errorf("speed %v at %v after loading a track to stand, idle %v", e.Speed(), e.Position(), e.deck.idle())
	}
}
