package ui

import (
	"math"
	"testing"
)

func TestOctants(t *testing.T) {
	seen := map[string]int{}
	for m, g := range octants {
		if prev, ok := seen[g]; ok {
			t.Errorf("masks %08b and %08b are both drawn as %q", prev, m, g)
		}
		seen[g] = m
	}
	// Octants are numbered row by row: 1 2 / 3 4 / 5 6 / 7 8.
	for mask, want := range map[int]string{
		0b00000000: " ",
		0b11111111: "█",
		0b01010101: "▌",
		0b00001111: "▀",
		0b11000000: "▂",
		0b00000100: "\U0001CD00", // BLOCK OCTANT-3, the first octant character
		0b11111110: "\U0001CDE5", // BLOCK OCTANT-2345678, the last
	} {
		if got := octants[mask]; got != want {
			t.Errorf("octants[%08b] = %q, want %q", mask, got, want)
		}
	}
}

// The windows of all cassette sizes the layout makes.
func eachReelWindow(t *testing.T, f func(cols, rows int, g reels)) {
	for _, h := range []int{9, 11, 13} {
		for w := 34; w <= 50; w++ {
			lw := w - 4
			cols, rows := lw-2*max(3, lw/7), h-6
			f(cols, rows, reelGeometry(cols, rows))
		}
	}
}

func TestReelsAreSymmetric(t *testing.T) {
	eachReelWindow(t, func(cols, rows int, g reels) {
		dw := float64(cols * 2)
		half := func(v float64) bool { return v-math.Floor(v) == 0.5 }
		if !half(g.cxL) || !half(g.cy) {
			t.Errorf("%dx%d: the reels turn about %v,%v, not the middle of four dots", cols, rows, g.cxL, g.cy)
		}
		if g.cxR != dw-1-g.cxL {
			t.Errorf("%dx%d: the right reel at %v does not mirror the left at %v", cols, rows, g.cxR, g.cxL)
		}
		if q := (dw - 1) * 0.25; math.Abs(g.cxL-q) > 1 {
			t.Errorf("%dx%d: the left reel at %v is more than a dot from %v", cols, rows, g.cxL, q)
		}
	})
}

func TestHubLiesOnTheEmptyPack(t *testing.T) {
	eachReelWindow(t, func(cols, rows int, g reels) {
		if reach := ringReach(g.cxL, g.cy, g.hubR); g.rMin < reach {
			t.Errorf("%dx%d: the cells of the hub reach %.2f, beyond the empty pack of %.2f", cols, rows, reach, g.rMin)
		}
		if g.rMax <= g.rMin {
			t.Errorf("%dx%d: a full pack of %.2f is no larger than an empty one of %.2f", cols, rows, g.rMax, g.rMin)
		}
		for _, r := range []float64{g.rMin, g.rMax} {
			if unNubbed(r) != r {
				t.Errorf("%dx%d: a pack of %.2f ends in a nub", cols, rows, r)
			}
		}
	})
}
