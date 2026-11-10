package audio

import (
	"bytes"
	"encoding/binary"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The files below are headers written by hand: enough of each format for
// Duration, which reads nothing else, and no more.

func be32(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }
func le32(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }

// id3v2 is an ID3v2 tag with n bytes of content.
func id3v2(n int) []byte {
	tag := []byte{'I', 'D', '3', 4, 0, 0, byte(n >> 21 & 0x7f), byte(n >> 14 & 0x7f), byte(n >> 7 & 0x7f), byte(n & 0x7f)}
	return append(tag, make([]byte, n)...)
}

// flacHeader is the start of a FLAC file of total samples at 44.1 kHz.
func flacHeader(total uint64) []byte {
	info := make([]byte, 34)
	info[10], info[11], info[12] = 0x0a, 0xc4, 0x42 // 44100 Hz, 2 channels
	info[13] = 0xf0 | byte(total>>32&0x0f)          // 16 bits
	binary.BigEndian.PutUint32(info[14:], uint32(total))
	return append([]byte{'f', 'L', 'a', 'C', 0x80, 0, 0, 34}, info...)
}

// mp3Frame is a frame of MPEG 1 layer III at 128 kbit/s and 44.1 kHz, 417
// bytes of it, with body after the header.
func mp3Frame(body ...byte) []byte {
	frame := make([]byte, 417)
	copy(frame, []byte{0xff, 0xfb, 0x90, 0x64})
	copy(frame[4:], body)
	return frame
}

// mp3Frames is n such frames, the first one made by first.
func mp3Frames(n int, first []byte) []byte {
	out := append([]byte(nil), first...)
	for i := 1; i < n; i++ {
		out = append(out, mp3Frame()...)
	}
	return out
}

// oggPage is an Ogg page of the stream 0x01020304 that ends at granule.
func oggPage(kind byte, granule uint64, packet []byte) []byte {
	page := append([]byte{'O', 'g', 'g', 'S', 0, kind}, binary.LittleEndian.AppendUint64(nil, granule)...)
	page = append(page, 4, 3, 2, 1) // serial number
	page = append(page, make([]byte, 8)...)
	page = append(page, 1, byte(len(packet)))
	return append(page, packet...)
}

// atom is an MP4 atom.
func atom(kind string, content ...[]byte) []byte {
	body := bytes.Join(content, nil)
	return append(append(be32(uint32(8+len(body))), kind...), body...)
}

func TestDurationReadsTheHeaders(t *testing.T) {
	vorbis := append([]byte("\x01vorbis\x00\x00\x00\x00\x02"), le32(44100)...)
	vorbis = append(vorbis, make([]byte, 14)...)
	opus := append([]byte("OpusHead\x01\x02\x38\x01"), le32(48000)...) // 312 samples to skip
	opus = append(opus, 0, 0, 0)
	xing := append(make([]byte, 32), "Xing\x00\x00\x00\x01"...)
	vbri := append(make([]byte, 32), "VBRI"...)
	vbri = append(append(vbri, make([]byte, 10)...), be32(500)...)
	mvhd := bytes.Join([][]byte{{0, 0, 0, 0}, make([]byte, 8), be32(44100), be32(5 * 44100), make([]byte, 80)}, nil)
	mvhd64 := bytes.Join([][]byte{{1, 0, 0, 0}, make([]byte, 16), be32(48000), make([]byte, 4), be32(9 * 48000), make([]byte, 80)}, nil)
	ftyp := atom("ftyp", []byte("M4A \x00\x00\x00\x00"))
	second := time.Second

	withoutFFmpeg(t)
	for name, c := range map[string]struct {
		data []byte
		want time.Duration
	}{
		"plain.flac":      {flacHeader(3 * 44100), 3 * second},
		"long.flac":       {flacHeader(20 * 3600 * 44100), 20 * time.Hour}, // more samples than 32 bits hold
		"tagged.flac":     {append(id3v2(300), flacHeader(44100)...), second},
		"xing.mp3":        {mp3Frames(4, mp3Frame(append(xing, be32(1000)...)...)), 26122 * time.Millisecond},
		"vbri.mp3":        {mp3Frames(4, mp3Frame(vbri...)), 13061 * time.Millisecond},
		"cbr.mp3":         {mp3Frames(100, mp3Frame()), 2606 * time.Millisecond},
		"cbr-tagged.mp3":  {append(append(id3v2(2000), mp3Frames(100, mp3Frame())...), append([]byte("TAG"), make([]byte, 125)...)...), 2606 * time.Millisecond},
		"junk-before.mp3": {append([]byte{0, 0xff, 0x00, 0xff, 0xe0, 0x01}, mp3Frames(100, mp3Frame())...), 2606 * time.Millisecond},
		"vorbis.ogg":      {append(oggPage(2, 0, vorbis), oggPage(4, 7*44100, []byte{0})...), 7 * second},
		"opus.opus":       {append(oggPage(2, 0, opus), oggPage(4, 4*48000+312, []byte{0})...), 4 * second},
		"moov-last.m4a":   {bytes.Join([][]byte{ftyp, atom("mdat", make([]byte, 100)), atom("moov", atom("mvhd", mvhd))}, nil), 5 * second},
		"moov-first.m4a":  {bytes.Join([][]byte{ftyp, atom("moov", atom("udta"), atom("mvhd", mvhd64)), atom("mdat", make([]byte, 100))}, nil), 9 * second},
	} {
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, c.data, 0o644); err != nil {
			t.Fatal(err)
		}
		if got := Duration(path); got < c.want-time.Millisecond || got > c.want+time.Millisecond {
			t.Errorf("Duration(%s) = %v, want %v", name, got, c.want)
		}
	}
}

func TestDurationAgreesWithTheDecoder(t *testing.T) {
	dir := t.TempDir()
	for rate, length := range map[int]time.Duration{44100: 1300 * time.Millisecond, 48000: 250 * time.Millisecond, 8000: 2 * time.Second} {
		path := filepath.Join(dir, "tone.wav")
		writeWAV(t, path, rate, length)
		src, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		src.Close()
		if got := Duration(path); got != src.Length {
			t.Errorf("Duration = %v, the decoder says %v", got, src.Length)
		}
		// A file cut short plays as long as it has data for.
		data, _ := os.ReadFile(path)
		os.WriteFile(path, data[:44+(len(data)-44)/2], 0o644)
		if got, want := Duration(path), src.Length/2; got < want-time.Millisecond || got > want+time.Millisecond {
			t.Errorf("Duration of the file cut in half = %v, want %v", got, want)
		}
	}
}

func TestDurationOfWhatIsNoAudio(t *testing.T) {
	withoutFFmpeg(t)
	dir := t.TempDir()
	if got := Duration(filepath.Join(dir, "missing.mp3")); got != 0 {
		t.Errorf("a missing file plays for %v", got)
	}
	// Whatever is in a file, it is read without a panic and without end.
	rng := rand.New(rand.NewPCG(7, 11))
	starts := [][]byte{nil, []byte("RIFF\x00\x00\x00\x00WAVEfmt "), []byte("fLaC\x80\x00\x00\x22"), {0xff, 0xfb, 0x90, 0x64}, []byte("ID3\x04\x00\x00\x00\x00\x00\x10"),
		[]byte("OggS\x00\x02"), []byte("\x00\x00\x00\x10ftypM4A \x00\x00\x00\x00\x00\x00\x00\x00moov\x00\x00\x00\x00mvhd")}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 3000 {
			data := append([]byte(nil), starts[i%len(starts)]...)
			for range rng.IntN(600) {
				data = append(data, byte(rng.IntN(256)))
			}
			for _, ext := range []string{"wav", "flac", "mp3", "ogg", "opus", "m4a", "aac"} {
				path := filepath.Join(dir, "noise."+ext)
				if err := os.WriteFile(path, data, 0o644); err != nil {
					t.Error(err)
					return
				}
				if d := Duration(path); d < 0 || d > 1000*time.Hour {
					t.Errorf("%s of %x plays for %v", ext, data, d)
				}
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("reading a header takes forever")
	}
}
