package library

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Fold returns s the way names are matched: in lower case, and without the
// accents of Latin, Greek and Cyrillic letters, whichever Unicode form s is
// in. Programs on macOS store "ä" as an "a" followed by a combining
// diaeresis while a keyboard types it as one letter; both fold to "a", so
// that either finds the other, and so does a plain "a".
func Fold(s string) string {
	if isASCII(s) {
		return strings.ToLower(s)
	}
	return foldDecomposed(norm.NFD.String(s))
}

// Part is one letter of a name, with the accents that belong to it, and
// what it folds to.
type Part struct {
	Text string // as it stands in the name
	Fold string // as Fold returns it
}

// Parts splits s into its letters. Their texts make up s and their folds
// Fold(s), which tells where in a name the match of a folded query lies.
func Parts(s string) []Part {
	parts := make([]Part, 0, len(s))
	var it norm.Iter
	it.InitString(norm.NFD, s)
	for !it.Done() {
		start := it.Pos()
		decomposed := it.Next()
		parts = append(parts, Part{Text: s[start:it.Pos()], Fold: foldDecomposed(string(decomposed))})
	}
	return parts
}

// Compose returns s with every accent joined to its letter where Unicode has
// one character for the two, the form NFC.
func Compose(s string) string {
	if isASCII(s) {
		return s
	}
	return norm.NFC.String(s)
}

// foldDecomposed folds s, which is in the decomposed form NFD: there the
// accents stand by themselves, after the letter they belong to.
func foldDecomposed(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if !isAccent(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// isAccent reports whether r is one of the combining diacritical marks, the
// accents of the Latin, Greek and Cyrillic scripts. The marks of other
// scripts stay, as many of those tell one word from another.
func isAccent(r rune) bool { return r >= 0x0300 && r <= 0x036F }

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}
