// Package library browses the music folder on disk: listing directories,
// collecting tracks recursively and reading track metadata.
package library

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/dhowden/tag"
)

// Entry is a folder or a playable file inside a library folder.
type Entry struct {
	Name  string
	Path  string
	IsDir bool
	Size  int64
}

// Ext returns the lowercase file extension without the dot.
func (e Entry) Ext() string { return Ext(e.Name) }

// Ext returns the lowercase extension of name without the dot.
func Ext(name string) string {
	return strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
}

// Formats is the set of playable extensions (lowercase, without dot).
type Formats map[string]bool

// NewFormats builds a Formats set from a list of extensions.
func NewFormats(exts []string) Formats {
	f := Formats{}
	for _, e := range exts {
		f[strings.ToLower(e)] = true
	}
	return f
}

// Playable reports whether a file name has a playable extension.
func (f Formats) Playable(name string) bool { return f[Ext(name)] }

// ReadDir lists the sub folders and playable files of dir. Folders come
// first; both groups are sorted in the order of NaturalCompare.
func ReadDir(dir string, formats Formats, showHidden bool) ([]Entry, error) {
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var dirs, files []Entry
	for _, de := range des {
		name := de.Name()
		if !showHidden && strings.HasPrefix(name, ".") {
			continue
		}
		p := filepath.Join(dir, name)
		isDir := de.IsDir()
		var size int64
		if de.Type()&fs.ModeSymlink != 0 {
			fi, err := os.Stat(p)
			if err != nil {
				continue // dangling link
			}
			isDir, size = fi.IsDir(), fi.Size()
		} else if !isDir {
			if fi, err := de.Info(); err == nil {
				size = fi.Size()
			}
		}
		switch {
		case isDir:
			dirs = append(dirs, Entry{Name: name, Path: p, IsDir: true})
		case formats.Playable(name):
			files = append(files, Entry{Name: name, Path: p, Size: size})
		}
	}
	sortByName(dirs)
	sortByName(files)
	return append(dirs, files...), nil
}

// sortByName sorts entries in the order of NaturalCompare.
func sortByName(entries []Entry) {
	s := byName{entries: entries, keys: make([]string, len(entries))}
	for i, e := range entries {
		s.keys[i] = Fold(e.Name)
	}
	sort.Sort(s)
}

// byName sorts entries by their names, of which keys holds the folded form.
type byName struct {
	entries []Entry
	keys    []string
}

func (s byName) Len() int { return len(s.entries) }

func (s byName) Less(i, j int) bool {
	if c := compareFolded(s.keys[i], s.keys[j]); c != 0 {
		return c < 0
	}
	return s.entries[i].Name < s.entries[j].Name
}

func (s byName) Swap(i, j int) {
	s.entries[i], s.entries[j] = s.entries[j], s.entries[i]
	s.keys[i], s.keys[j] = s.keys[j], s.keys[i]
}

// Collect returns all playable files below dir, in the order of Walk.
func Collect(dir string, formats Formats, showHidden bool) ([]string, error) {
	entries, err := Walk(dir, formats, showHidden)
	var out []string
	for _, e := range entries {
		if !e.IsDir {
			out = append(out, e.Path)
		}
	}
	return out, err
}

// Walk returns the sub folders and playable files below dir, depth first
// and sorted like the browser: each folder's files come before its sub
// folders, and each sub folder right before what is in it.
func Walk(dir string, formats Formats, showHidden bool) ([]Entry, error) {
	var out []Entry
	seen := map[string]bool{}
	var walk func(string, int) error
	walk = func(d string, depth int) error {
		if real, err := filepath.EvalSymlinks(d); err == nil {
			if seen[real] {
				return nil // symlink loop
			}
			seen[real] = true
		}
		entries, err := ReadDir(d, formats, showHidden)
		if err != nil {
			if depth == 0 {
				return err
			}
			return nil // skip unreadable sub folders
		}
		// A folder's own tracks come before those of its sub folders, so
		// "Bonus" or "Extras" folders play after the album.
		for _, e := range entries {
			if !e.IsDir {
				out = append(out, e)
			}
		}
		for _, e := range entries {
			if e.IsDir && depth < 32 {
				out = append(out, e)
				if err := walk(e.Path, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	err := walk(dir, 0)
	return out, err
}

// NaturalCompare compares strings without regard to case and accents, see
// Fold, and treats runs of digits as numbers: "2" comes before "10", and
// an accented letter among the plain ones, not after "z".
func NaturalCompare(a, b string) int {
	if c := compareFolded(Fold(a), Fold(b)); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}

// compareFolded is NaturalCompare for folded strings. It takes names that
// differ in case, accents or leading zeros only for equal.
func compareFolded(a, b string) int {
	for a != "" && b != "" {
		if isDigit(a[0]) && isDigit(b[0]) {
			na, nb := digitRun(a), digitRun(b)
			ta, tb := strings.TrimLeft(na, "0"), strings.TrimLeft(nb, "0")
			if len(ta) != len(tb) {
				return cmpInt(len(ta), len(tb))
			}
			if c := strings.Compare(ta, tb); c != 0 {
				return c
			}
			a, b = a[len(na):], b[len(nb):]
			continue
		}
		ra, sa := utf8.DecodeRuneInString(a)
		rb, sb := utf8.DecodeRuneInString(b)
		if ra != rb {
			return cmpInt(int(ra), int(rb))
		}
		a, b = a[sa:], b[sb:]
	}
	return cmpInt(len(a), len(b))
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func digitRun(s string) string {
	i := 0
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	return s[:i]
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// Meta is the subset of tag metadata cassetty displays.
type Meta struct {
	Title  string
	Artist string
	Album  string
	Track  int
}

// ReadMeta reads the tags of the file at path. Missing or broken tags yield
// an empty Meta.
func ReadMeta(path string) (m Meta) {
	defer func() {
		if recover() != nil { // the tag parser is not hardened against every file
			m = Meta{}
		}
	}()
	f, err := os.Open(path)
	if err != nil {
		return Meta{}
	}
	defer f.Close()
	md, err := tag.ReadFrom(f)
	if err != nil {
		return Meta{}
	}
	m.Title = strings.TrimSpace(md.Title())
	m.Artist = strings.TrimSpace(md.Artist())
	if m.Artist == "" {
		m.Artist = strings.TrimSpace(md.AlbumArtist())
	}
	m.Album = strings.TrimSpace(md.Album())
	m.Track, _ = md.Track()
	return m
}
