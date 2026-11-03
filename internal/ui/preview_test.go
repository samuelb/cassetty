package ui

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/samuelb/cassetty/internal/audio"
	"github.com/samuelb/cassetty/internal/library"
)

// TestCassettePreview writes cassette renderings at several sizes and
// positions to $CASSETTY_PREVIEW, for eyeballing the artwork in a terminal.
func TestCassettePreview(t *testing.T) {
	out := os.Getenv("CASSETTY_PREVIEW")
	if out == "" {
		t.Skip("CASSETTY_PREVIEW not set")
	}
	m := newTestModel(t, t.TempDir())
	m.src = &audio.Source{Codec: "MP3", Length: 3 * time.Minute, SampleRate: 44100, Channels: 2, Bitrate: 320}
	m.cur = &Track{Path: "/music/Analog Dreams/Night Drive/01 Neon Overture.mp3"}
	m.meta = library.Meta{Title: "Neon Overture", Artist: "Analog Dreams", Album: "Night Drive"}
	m.state = statePlaying

	var sb strings.Builder
	for _, size := range [][2]int{{50, 13}, {44, 11}, {40, 9}} {
		var cols [][]string
		for i, p := range []float64{0, 0.5, 1} {
			m.pos = time.Duration(p * float64(m.src.Length))
			m.angleL, m.angleR = float64(i)*0.35, float64(i)*0.5
			cols = append(cols, m.renderCassette(size[0], size[1]))
		}
		for row := range cols[0] {
			for _, c := range cols {
				sb.WriteString(c[row] + "  ")
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}
	if err := os.WriteFile(out, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}
