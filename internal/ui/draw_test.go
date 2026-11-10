package ui

import (
	"image/color"
	"slices"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestLineBufWritesRunsOfOneStyle(t *testing.T) {
	red := fgbg(lipgloss.Color("#ff0000"), lipgloss.Color("#101010"))
	blue := fg(lipgloss.Color("#0000ff")).Bold(true)

	var lb lineBuf
	lb.add(red, "ab").add(red, "c").add(blue, "d").add(blue, "").add(red, "e")
	lb.add(lipgloss.NewStyle(), "plain").add(red, "f")
	lb.raw("<raw>", 5).add(red, "g").padTo(red, 20)
	got := lb.String()

	want := red.Render("abc") + blue.Render("d") + red.Render("e") + "plain" + red.Render("f") +
		"<raw>" + red.Render("g"+strings.Repeat(" ", 3))
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	if lb.w != 20 || ansi.StringWidth(got) != 20 {
		t.Errorf("the line counts %d cells and is %d wide, want 20", lb.w, ansi.StringWidth(got))
	}
	if again := lb.String(); again != got {
		t.Errorf("asked again the line is %q", again)
	}

	// Underlined text is left to lipgloss, which styles it letter by letter.
	under := fg(lipgloss.Color("#00ff00")).Underline(true)
	var ul lineBuf
	ul.add(red, "a").add(under, "b c").add(red, "d")
	if want := red.Render("a") + under.Render("b c") + red.Render("d"); ul.String() != want {
		t.Errorf("got  %q\nwant %q", ul.String(), want)
	}
}

func TestCells(t *testing.T) {
	for text, want := range map[string]int{"": 0, "plain text": 10, "▮▮": 2, "日本": 4, "a\tb": ansi.StringWidth("a\tb")} {
		if got := cells(text); got != want {
			t.Errorf("cells(%q) = %d, want %d", text, got, want)
		}
	}
}

func TestBlendsAreTheCallersToChange(t *testing.T) {
	a, b := lipgloss.Color("#ff0000"), lipgloss.Color("#0000ff")
	for name, blend := range map[string]func() []lipgloss.Style{
		"gradient": func() []lipgloss.Style { return styles(gradient(5, a, b)) },
		"ramp":     func() []lipgloss.Style { return styles(ramp(5, a, b, a)) },
	} {
		first := blend()
		if len(first) != 5 {
			t.Fatalf("%s of %d colors", name, len(first))
		}
		if !slices.EqualFunc(first, blend(), func(x, y lipgloss.Style) bool { return x.Render("x") == y.Render("x") }) {
			t.Errorf("the %s differs when asked for again", name)
		}
	}
	// What one caller does to its colors does not reach the next.
	mine := gradient(5, a, b)
	mine[0] = b
	if again := gradient(5, a, b); fg(again[0]).Render("x") != fg(a).Render("x") {
		t.Error("a change to a gradient shows in the next one")
	}
}

func styles(colors []color.Color) []lipgloss.Style {
	out := make([]lipgloss.Style, len(colors))
	for i, c := range colors {
		out[i] = fg(c)
	}
	return out
}
