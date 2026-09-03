package codec

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type fileFormat int

const (
	formatUnknown fileFormat = iota
	formatWAV
	formatFLAC
	formatMP3
)

// sniff identifies a format from the bytes themselves rather than trusting a
// file extension, since a backing track can arrive renamed, extension-less,
// or (for MP3) without any container at all — just raw frames.
func sniff(data []byte) fileFormat {
	if len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WAVE" {
		return formatWAV
	}
	if len(data) >= 4 && string(data[0:4]) == "fLaC" {
		return formatFLAC
	}
	if len(data) >= 3 && string(data[0:3]) == "ID3" {
		return formatMP3
	}
	if len(data) >= 2 && data[0] == 0xFF && data[1]&0xE0 == 0xE0 {
		// 11-bit MPEG frame sync: the top byte is all ones, and the next
		// byte's top 3 bits are too.
		return formatMP3
	}
	return formatUnknown
}

// Decode decodes from memory. The format is detected from the data's own
// bytes; see sniff.
func Decode(data []byte) (*PCM, error) {
	switch sniff(data) {
	case formatWAV:
		return DecodeWAV(data)
	case formatFLAC:
		return DecodeFLAC(data)
	case formatMP3:
		return DecodeMP3(data)
	default:
		return nil, errors.New("codec: unrecognised audio format")
	}
}

// DecodeFile decodes a backing track from disk. The format is detected from
// the file's own bytes, with the extension as a fallback for a stream whose
// sniff is ambiguous.
func DecodeFile(path string) (*PCM, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("codec: %w", err)
	}

	pcm, decodeErr := Decode(data)
	if decodeErr == nil {
		return pcm, nil
	}

	switch strings.ToLower(filepath.Ext(path)) {
	case ".wav":
		return DecodeWAV(data)
	case ".mp3":
		return DecodeMP3(data)
	case ".flac":
		return DecodeFLAC(data)
	}
	return nil, decodeErr
}
