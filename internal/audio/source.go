// Package audio implements the playback engine: decoding audio files into a
// single always-running "deck" streamer that feeds the sound card, plus a
// sample tap used by the visualizers.
package audio

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/flac"
	"github.com/gopxl/beep/v2/mp3"
	"github.com/gopxl/beep/v2/vorbis"
	"github.com/gopxl/beep/v2/wav"
)

// SampleRate is the output sample rate of the deck. Sources with a different
// rate are resampled on the fly.
const SampleRate beep.SampleRate = 44100

// nativeFormats are decoded in-process by pure Go decoders.
var nativeFormats = []string{"mp3", "flac", "wav", "ogg", "oga"}

// ffmpegFormats are decoded by piping through ffmpeg, if it is installed.
var ffmpegFormats = []string{"m4a", "m4b", "aac", "alac", "opus", "aif", "aiff", "aifc", "wma", "ape", "wv", "mka", "ac3", "caf"}

// SupportedFormats returns the playable file extensions (lowercase, without
// dot). The ffmpeg backed formats are only included when ffmpeg is available.
func SupportedFormats() []string {
	out := append([]string(nil), nativeFormats...)
	if FFmpegAvailable() {
		out = append(out, ffmpegFormats...)
	}
	return out
}

// Source is an opened, decodable audio file.
type Source struct {
	Path       string
	Codec      string        // short codec name for display, e.g. "MP3"
	SampleRate int           // native sample rate of the file
	Channels   int           // native channel count of the file
	Length     time.Duration // 0 when unknown
	Bitrate    int           // average kbit/s, 0 when unknown

	stream beep.StreamSeekCloser
	format beep.Format // format of stream (may differ from native for ffmpeg)
	// ahead holds frames read from stream that have not played yet: they
	// are the next ones of the source, see reader.
	ahead [][2]float64
}

// position returns the frame of the source that is read next.
func (s *Source) position() int {
	return s.stream.Position() - len(s.ahead)
}

// putBack returns frames that were read from the source to it, to be read
// again: the last of frames, as far as the source has been read.
func (s *Source) putBack(frames [][2]float64) {
	frames = frames[len(frames)-max(0, min(len(frames), s.position())):]
	if len(frames) > 0 {
		s.ahead = append(slices.Clone(frames), s.ahead...)
	}
}

// reader streams a source: the frames it has read ahead, then its stream.
type reader struct{ src *Source }

func (r reader) Stream(samples [][2]float64) (int, bool) {
	s := r.src
	n := copy(samples, s.ahead)
	if s.ahead = s.ahead[n:]; len(s.ahead) == 0 {
		s.ahead = nil
	}
	if n == len(samples) {
		return n, true
	}
	k, ok := s.stream.Stream(samples[n:])
	return n + k, ok || n > 0
}

func (r reader) Err() error { return r.src.stream.Err() }

// Close releases the underlying decoder and file.
func (s *Source) Close() error {
	if s == nil || s.stream == nil {
		return nil
	}
	return s.stream.Close()
}

// Open opens and decodes the audio file at path.
func Open(path string) (*Source, error) {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	if !slices.Contains(nativeFormats, ext) {
		if !FFmpegAvailable() {
			return nil, fmt.Errorf("unsupported format %q (install ffmpeg for more formats)", ext)
		}
		return openFFmpeg(path)
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	src, err := openNative(f, path, ext)
	if err != nil {
		f.Close()
		// Containers like .ogg may hold codecs the native decoders don't
		// handle (Opus, FLAC); let ffmpeg have a go at it.
		if FFmpegAvailable() {
			if src, ferr := openFFmpeg(path); ferr == nil {
				return src, nil
			}
		}
		return nil, fmt.Errorf("cannot play %s: %w", filepath.Base(path), err)
	}
	return src, nil
}

// openNative decodes f, the file at path, with the pure Go decoder for ext.
// The decoders are not hardened against every damaged file: a panic of
// theirs is an error like any other, and so is a format nothing can be
// played from.
func openNative(f *os.File, path, ext string) (src *Source, err error) {
	defer func() {
		if r := recover(); r != nil {
			src, err = nil, damaged(r)
		}
	}()
	var (
		stream beep.StreamSeekCloser
		format beep.Format
		codec  = strings.ToUpper(ext)
	)
	switch ext {
	case "mp3":
		stream, format, err = mp3.Decode(f)
	case "flac":
		stream, format, err = flac.Decode(f)
	case "wav":
		stream, format, err = wav.Decode(f)
	default:
		stream, format, err = vorbis.Decode(f)
		codec = "VORBIS"
	}
	if err != nil {
		return nil, err
	}
	if err := checkFormat(format); err != nil {
		return nil, err
	}
	src = &Source{
		Path:       path,
		Codec:      codec,
		SampleRate: int(format.SampleRate),
		Channels:   format.NumChannels,
		stream:     stream,
		format:     format,
	}
	if n := stream.Len(); n > 0 {
		src.Length = format.SampleRate.D(n)
	}
	if fi, err := f.Stat(); err == nil && src.Length > 0 {
		src.Bitrate = int(float64(fi.Size()) * 8 / src.Length.Seconds() / 1000)
	}
	return src, nil
}

// checkFormat refuses what a damaged header may claim and no deck can play:
// the resampler, for one, panics on a sample rate of zero or less.
func checkFormat(f beep.Format) error {
	if f.SampleRate < 1000 || f.SampleRate > 1_000_000 {
		return fmt.Errorf("damaged file (a sample rate of %d Hz)", int(f.SampleRate))
	}
	if f.NumChannels < 1 {
		return fmt.Errorf("damaged file (%d channels)", f.NumChannels)
	}
	return nil
}

// damaged is the error for a decoder that panicked with r.
func damaged(r any) error {
	return fmt.Errorf("damaged file (%v)", r)
}

// errNoSource is returned by engine operations that need a loaded track.
var errNoSource = errors.New("no track loaded")
