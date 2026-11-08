package ui

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/samuelb/cassetty/internal/audio"
)

// toneLibrary returns a folder of n 440 Hz WAV files of length d.
func toneLibrary(t *testing.T, n int, d time.Duration) string {
	t.Helper()
	const rate = 44100
	frames := int(d.Seconds() * rate)
	data := make([]byte, 44+frames*4)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(36+frames*4))
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1)
	binary.LittleEndian.PutUint16(data[22:], 2)
	binary.LittleEndian.PutUint32(data[24:], rate)
	binary.LittleEndian.PutUint32(data[28:], rate*4)
	binary.LittleEndian.PutUint16(data[32:], 4)
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], uint32(frames*4))
	for i := 0; i < frames; i++ {
		v := uint16(int16(8000 * math.Sin(2*math.Pi*440*float64(i)/rate)))
		binary.LittleEndian.PutUint16(data[44+i*4:], v)
		binary.LittleEndian.PutUint16(data[46+i*4:], v)
	}
	root := t.TempDir()
	for i := range n {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("%02d.wav", i+1)), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestPlaysOnWithoutAGap(t *testing.T) {
	m := newTestModel(t, toneLibrary(t, 3, time.Second))
	press(m, "enter") // plays the first track, with its folder as the playlist
	if len(m.playlist) != 3 || m.cur != m.playlist[0] || m.state != statePlaying {
		t.Fatalf("first track not playing: state %v, flash %q", m.state, m.flash)
	}
	if m.queue.track != m.playlist[1] || m.queue.src == nil {
		t.Fatal("second track not queued")
	}

	var ev audio.Event
	select {
	case ev = <-m.engine.Events():
	case <-time.After(3 * time.Second):
		t.Fatal("first track did not end")
	}
	if ev.Next == 0 {
		t.Fatalf("the deck stopped after the first track: %+v", ev)
	}
	_, cmd := m.Update(audioEventMsg(ev))
	if m.cur != m.playlist[1] || m.state != statePlaying || m.loading {
		t.Fatalf("second track not playing: cur %v, state %v, loading %v", m.cur, m.state, m.loading)
	}
	run(m, cmd)
	if m.queue.track != m.playlist[2] || m.queue.src == nil {
		t.Fatal("third track not queued")
	}
}

func TestNextPlaysTheQueuedTrackAtOnce(t *testing.T) {
	m := newTestModel(t, toneLibrary(t, 3, 5*time.Second))
	press(m, "enter")
	queued := m.queue.src
	if queued == nil {
		t.Fatal("second track not queued")
	}
	press(m, "b")
	if m.cur != m.playlist[1] || m.src != queued || m.loading || m.state != statePlaying {
		t.Fatalf("next did not play the queued track: cur %v, loading %v, state %v", m.cur, m.loading, m.state)
	}
	if m.queue.track != m.playlist[2] {
		t.Fatal("third track not queued after skipping")
	}

	// Turning on repeat one queues the loaded track again.
	press(m, "r")
	press(m, "r")
	if m.repeat != repeatOne || m.queue.track != m.cur {
		t.Fatalf("repeat %v queues %v, want the loaded track", m.repeat, m.queue.track)
	}
	press(m, "e")
	if m.queue.track != nil {
		t.Fatal("ejecting should clear the queue")
	}
}

func TestShuffleNamesTheSameNextTrackUntilItPlays(t *testing.T) {
	m := newTestModel(t, t.TempDir())
	m.playlist = tracksFor([]string{"a", "b", "c", "d", "e"})
	n := len(m.playlist)
	m.shuffle, m.repeat = true, repeatAll
	m.cur = m.playlist[0]
	var played []int
	for range 3 * n {
		i, ok := m.nextIndex()
		if j, _ := m.nextIndex(); !ok || j != i {
			t.Fatalf("next track changed from %d to %d on asking again", i, j)
		}
		m.cur = m.playlist[i]
		m.turnOrder(m.currentIndex())
		played = append(played, i)
	}
	// The first round plays the n-1 tracks after the loaded one; each
	// round after that plays every track once, and never starts with the
	// track that ended the round before.
	rounds := [][]int{played[:n-1]}
	for r := n - 1; r+n <= len(played); r += n {
		rounds = append(rounds, played[r:r+n])
	}
	for k, round := range rounds {
		seen := map[int]bool{}
		if k == 0 {
			seen[0] = true
		}
		for _, i := range round {
			if seen[i] {
				t.Fatalf("round %d plays track %d twice: %v", k, i, played)
			}
			seen[i] = true
		}
		if k > 0 && round[0] == rounds[k-1][len(rounds[k-1])-1] {
			t.Fatalf("round %d starts with the track that ended the round before: %v", k, played)
		}
	}
}
