package audio

import (
	"bytes"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Duration returns the playing time of the file at path, or 0 where it
// cannot tell. It reads what the header of the file says and decodes
// nothing, so that asking for every track of a long playlist takes a few
// kilobytes of each: opening a track to ask its length reads all of an MP3
// and may decode all of a FLAC. What it returns can be off by a frame or two
// from the length of the opened track.
func Duration(path string) (d time.Duration) {
	defer func() {
		// The headers are read with care, but a damaged file is no reason
		// to end the player.
		if recover() != nil || d < 0 || d > 1000*time.Hour {
			d = 0
		}
	}()
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	if f, err := os.Open(path); err == nil {
		switch ext {
		case "wav":
			d = wavDuration(f)
		case "flac":
			d = flacDuration(f)
		case "mp3":
			d = mp3Duration(f)
		case "ogg", "oga", "opus":
			d = oggDuration(f)
		case "m4a", "m4b", "mp4", "alac", "aac":
			d = mp4Duration(f)
		}
		f.Close()
	}
	if d > 0 || ext == "wav" || ext == "flac" || ext == "mp3" || ext == "ogg" || ext == "oga" {
		return d
	}
	// The formats that ffmpeg plays, and those of the above it may know
	// better: an .aac file is rarely an MP4, for one.
	return ffprobeDuration(path)
}

// ffprobeDuration asks ffprobe, if it is installed.
func ffprobeDuration(path string) time.Duration {
	lookupFFmpeg()
	if ffprobePath == "" {
		return 0
	}
	out, err := exec.Command(ffprobePath, "-v", "error", "-show_entries", "format=duration",
		"-of", "default=nw=1:nk=1", path).Output()
	if err != nil {
		return 0
	}
	secs, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil || secs <= 0 {
		return 0
	}
	return time.Duration(secs * float64(time.Second))
}

// frames returns the playing time of n frames at rate frames a second.
func frames(n, rate uint64) time.Duration {
	if rate == 0 {
		return 0
	}
	return time.Duration(float64(n) / float64(rate) * float64(time.Second))
}

// readAt reads n bytes at off; fewer if the file ends before.
func readAt(f *os.File, off int64, n int) []byte {
	b := make([]byte, n)
	k, _ := f.ReadAt(b, off)
	return b[:k]
}

func fileSize(f *os.File) int64 {
	fi, err := f.Stat()
	if err != nil {
		return 0
	}
	return fi.Size()
}

// id3v2Size returns the length of the ID3v2 tag at off, 0 if there is none.
func id3v2Size(f *os.File, off int64) int64 {
	h := readAt(f, off, 10)
	if len(h) < 10 || string(h[:3]) != "ID3" || h[6]|h[7]|h[8]|h[9] >= 0x80 {
		return 0
	}
	n := int64(h[6])<<21 | int64(h[7])<<14 | int64(h[8])<<7 | int64(h[9])
	if h[5]&0x10 != 0 {
		n += 10 // a footer
	}
	return 10 + n
}

// wavDuration reads the byte rate of the "fmt " chunk and the size of the
// "data" chunk.
func wavDuration(f *os.File) time.Duration {
	h := readAt(f, 0, 12)
	if len(h) < 12 || string(h[:4]) != "RIFF" || string(h[8:]) != "WAVE" {
		return 0
	}
	size := fileSize(f)
	var byteRate uint64
	for off, n := int64(12), 0; n < 64; n++ {
		c := readAt(f, off, 8)
		if len(c) < 8 {
			return 0
		}
		length := int64(binary.LittleEndian.Uint32(c[4:]))
		switch string(c[:4]) {
		case "fmt ":
			if b := readAt(f, off+8, 16); len(b) == 16 {
				byteRate = uint64(binary.LittleEndian.Uint32(b[8:]))
			}
		case "data":
			// A file cut short, or written by a program that did not know
			// the size in advance, has less than its header claims.
			length = min(length, size-off-8)
			if length <= 0 {
				return 0
			}
			return frames(uint64(length), byteRate)
		}
		off += 8 + length + length&1
	}
	return 0
}

// flacDuration reads the STREAMINFO block, which comes first.
func flacDuration(f *os.File) time.Duration {
	off := id3v2Size(f, 0)
	h := readAt(f, off, 4+4+34)
	if len(h) < 42 || string(h[:4]) != "fLaC" || h[4]&0x7f != 0 {
		return 0
	}
	return streamInfoDuration(h[8:])
}

// streamInfoDuration reads the sample rate and the number of samples from
// a FLAC STREAMINFO block.
func streamInfoDuration(info []byte) time.Duration {
	if len(info) < 18 {
		return 0
	}
	rate := uint64(info[10])<<12 | uint64(info[11])<<4 | uint64(info[12])>>4
	total := uint64(info[13]&0x0f)<<32 | uint64(binary.BigEndian.Uint32(info[14:]))
	return frames(total, rate)
}

// MPEG audio: kbit/s by bitrate index, for version 1 layers I to III and
// for versions 2 and 2.5 layer I and layers II and III.
var mpegBitrates = [5][16]int{
	{0, 32, 64, 96, 128, 160, 192, 224, 256, 288, 320, 352, 384, 416, 448},
	{0, 32, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 384},
	{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320},
	{0, 32, 48, 56, 64, 80, 96, 112, 128, 144, 160, 176, 192, 224, 256},
	{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160},
}

// mpegFrame is what the four bytes of an MPEG audio frame header say.
type mpegFrame struct {
	v1      bool // MPEG 1, else 2 or 2.5
	layer   int  // 1 to 3
	bitrate int  // bit/s
	rate    int  // samples a second
	mono    bool
	size    int // bytes of the frame
	samples int // samples of the frame
}

// parseMPEG reads a frame header, and reports false for what is none.
func parseMPEG(h []byte) (fr mpegFrame, ok bool) {
	if len(h) < 4 || h[0] != 0xff || h[1]&0xe0 != 0xe0 {
		return fr, false
	}
	version := h[1] >> 3 & 3 // 0: 2.5, 2: 2, 3: 1
	layerBits := h[1] >> 1 & 3
	bitrateIdx, rateIdx := h[2]>>4, h[2]>>2&3
	if version == 1 || layerBits == 0 || bitrateIdx == 0 || bitrateIdx == 15 || rateIdx == 3 {
		return fr, false
	}
	fr.v1, fr.layer, fr.mono = version == 3, 4-int(layerBits), h[3]>>6 == 3
	fr.rate = [3]int{44100, 48000, 32000}[rateIdx]
	switch version {
	case 2:
		fr.rate /= 2
	case 0:
		fr.rate /= 4
	}
	table := fr.layer - 1
	if !fr.v1 {
		table = 3
		if fr.layer > 1 {
			table = 4
		}
	}
	fr.bitrate = mpegBitrates[table][bitrateIdx] * 1000
	pad := int(h[2] >> 1 & 1)
	switch {
	case fr.layer == 1:
		fr.samples, fr.size = 384, (12*fr.bitrate/fr.rate+pad)*4
	case fr.layer == 3 && !fr.v1:
		fr.samples, fr.size = 576, 72*fr.bitrate/fr.rate+pad
	default:
		fr.samples, fr.size = 1152, 144*fr.bitrate/fr.rate+pad
	}
	return fr, fr.size > 4
}

// mp3Duration reads the number of frames from the Xing, Info or VBRI header
// that encoders put in the first frame. Without one the file is taken to be
// of one bitrate throughout, which a file without such a header mostly is.
func mp3Duration(f *os.File) time.Duration {
	start := id3v2Size(f, 0)
	buf := readAt(f, start, 1<<16)
	for i := 0; i+4 <= len(buf); i++ {
		fr, ok := parseMPEG(buf[i:])
		if !ok {
			continue
		}
		// What looks like a header by chance is rarely followed by another.
		if next := i + fr.size; next+4 <= len(buf) {
			if nx, ok := parseMPEG(buf[next:]); !ok || nx.rate != fr.rate || nx.layer != fr.layer {
				continue
			}
		}
		frame := buf[i:min(len(buf), i+fr.size)]
		side := 32 // the side information of layer III, before the header
		switch {
		case fr.v1 && fr.mono, !fr.v1 && !fr.mono:
			side = 17
		case !fr.v1:
			side = 9
		}
		if at := 4 + side; len(frame) >= at+12 {
			if tag := string(frame[at : at+4]); (tag == "Xing" || tag == "Info") && frame[at+7]&1 != 0 {
				n := binary.BigEndian.Uint32(frame[at+8:])
				return frames(uint64(n)*uint64(fr.samples), uint64(fr.rate))
			}
		}
		if len(frame) >= 36+18 && string(frame[36:40]) == "VBRI" {
			n := binary.BigEndian.Uint32(frame[36+14:])
			return frames(uint64(n)*uint64(fr.samples), uint64(fr.rate))
		}
		audio := fileSize(f) - start - int64(i)
		if tail := readAt(f, fileSize(f)-128, 3); string(tail) == "TAG" {
			audio -= 128 // an ID3v1 tag
		}
		if audio <= 0 {
			return 0
		}
		return frames(uint64(audio)*8, uint64(fr.bitrate))
	}
	return 0
}

// oggDuration reads the sample rate from the first page and the position
// of the last sample from the last one. It knows Vorbis, Opus and FLAC.
func oggDuration(f *os.File) time.Duration {
	first := readAt(f, 0, 27+255+64)
	if len(first) < 28 || string(first[:4]) != "OggS" {
		return 0
	}
	serial := first[14:18]
	packet := first[min(len(first), 27+int(first[26])):]
	var rate, skip uint64
	switch {
	case len(packet) >= 16 && string(packet[:7]) == "\x01vorbis":
		rate = uint64(binary.LittleEndian.Uint32(packet[12:]))
	case len(packet) >= 12 && string(packet[:8]) == "OpusHead":
		rate, skip = 48000, uint64(binary.LittleEndian.Uint16(packet[10:]))
	case len(packet) >= 13+8+18 && string(packet[:5]) == "\x7fFLAC" && string(packet[9:13]) == "fLaC":
		if d := streamInfoDuration(packet[17:]); d > 0 {
			return d
		}
		info := packet[17:]
		rate = uint64(info[10])<<12 | uint64(info[11])<<4 | uint64(info[12])>>4
	default:
		return 0
	}

	// The last page of the stream: a page is 65307 bytes at most.
	size := fileSize(f)
	off := max(0, size-(1<<16+1<<10))
	tail := readAt(f, off, int(size-off))
	for end := len(tail); ; {
		i := bytes.LastIndex(tail[:end], []byte("OggS"))
		if i < 0 {
			return 0
		}
		end = i
		if i+27 > len(tail) || !bytes.Equal(tail[i+14:i+18], serial) {
			continue
		}
		granule := binary.LittleEndian.Uint64(tail[i+6:])
		if granule == 1<<64-1 || granule < skip {
			continue // no packet ends on this page
		}
		return frames(granule-skip, rate)
	}
}

// mp4Duration reads the movie header: moov/mvhd.
func mp4Duration(f *os.File) time.Duration {
	size := fileSize(f)
	moov, end := findAtom(f, 0, size, "moov")
	if moov < 0 {
		return 0
	}
	mvhd, _ := findAtom(f, moov, end, "mvhd")
	if mvhd < 0 {
		return 0
	}
	b := readAt(f, mvhd, 32)
	switch {
	case len(b) >= 20 && b[0] == 0:
		return frames(uint64(binary.BigEndian.Uint32(b[16:])), uint64(binary.BigEndian.Uint32(b[12:])))
	case len(b) >= 32 && b[0] == 1:
		return frames(binary.BigEndian.Uint64(b[24:]), uint64(binary.BigEndian.Uint32(b[20:])))
	}
	return 0
}

// findAtom looks for the atom of the given type among those between off
// and end, and returns where its content begins and ends; -1 if it is not
// there.
func findAtom(f *os.File, off, end int64, kind string) (int64, int64) {
	for n := 0; n < 1<<12 && off+8 <= end; n++ {
		h := readAt(f, off, 16)
		if len(h) < 8 {
			break
		}
		length, header := int64(binary.BigEndian.Uint32(h)), int64(8)
		switch {
		case length == 1 && len(h) == 16:
			length, header = int64(binary.BigEndian.Uint64(h[8:])), 16
		case length == 0:
			length = end - off // to the end of the file
		}
		if length < header || off+length > end {
			break
		}
		if string(h[4:8]) == kind {
			return off + header, off + length
		}
		off += length
	}
	return -1, -1
}
