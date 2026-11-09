package library

import (
	"strings"
	"testing"
)

func TestFold(t *testing.T) {
	for in, want := range map[string]string{
		"Pink Floyd":                     "pink floyd",
		"Die A\u0308rzte":                "die arzte", // decomposed, as programs on macOS store it
		"Die \u00c4rzte":                 "die arzte", // composed, as a keyboard types it
		"Bj\u00f6rk":                     "bjork",
		"MOT\u00d6RHEAD":                 "motorhead",
		"Sigur R\u00f3s":                 "sigur ros",
		"\u0391\u03b8\u03ae\u03bd\u03b1": "\u03b1\u03b8\u03b7\u03bd\u03b1", // Athina, in Greek
		"M\u00d8":                        "m\u00f8",                        // a letter of its own, with no accent to take off
		"":                               "",
	} {
		if got := Fold(in); got != want {
			t.Errorf("Fold(%+q) = %+q, want %+q", in, got, want)
		}
	}
	// Both forms of a name fold alike in scripts whose marks stay, too.
	for composed, decomposed := range map[string]string{
		"\ud55c\uad6d":       "\u1112\u1161\u11ab\u1100\u116e\u11a8", // Hangul
		"\u30ac\u30fc\u30eb": "\u30ab\u3099\u30fc\u30eb",             // Katakana
	} {
		if Fold(composed) != Fold(decomposed) {
			t.Errorf("%+q and %+q fold to %+q and %+q", composed, decomposed, Fold(composed), Fold(decomposed))
		}
	}
}

func TestParts(t *testing.T) {
	for _, name := range []string{
		"01 So What.mp3", "Die A\u0308rzte", "Die \u00c4rzte", "\ud55c\uad6d",
		"\u30ab\u3099\u30fc\u30eb", "e\u0301\u0302x", "",
	} {
		var text, fold strings.Builder
		for _, p := range Parts(name) {
			if p.Text == "" {
				t.Errorf("Parts(%+q) has an empty part", name)
			}
			text.WriteString(p.Text)
			fold.WriteString(p.Fold)
		}
		if text.String() != name {
			t.Errorf("the parts of %+q make up %+q", name, text.String())
		}
		if fold.String() != Fold(name) {
			t.Errorf("the parts of %+q fold to %+q, Fold to %+q", name, fold.String(), Fold(name))
		}
	}
	// An accent stays with its letter.
	if parts := Parts("A\u0308r"); len(parts) != 2 || parts[0].Text != "A\u0308" || parts[0].Fold != "a" {
		t.Errorf("Parts = %+q", parts)
	}
}

func TestCompose(t *testing.T) {
	for in, want := range map[string]string{
		"Die A\u0308rzte": "Die \u00c4rzte",
		"Die \u00c4rzte":  "Die \u00c4rzte",
		"plain":           "plain",
	} {
		if got := Compose(in); got != want {
			t.Errorf("Compose(%+q) = %+q, want %+q", in, got, want)
		}
	}
}
