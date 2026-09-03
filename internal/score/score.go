// Package score is the front door to every importer. It decides which one a
// file belongs to and hands back the one normalized model the rest of the app
// knows about.
//
// Detection is by content first and by extension second. Extensions lie —
// a MusicXML file exported by half the notation programs in existence is
// called `.xml`, and a `.gp` and an `.mxl` are both ZIP archives — so the
// bytes get the first word.
package score

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/FullFran/riffhero/internal/practice"
	"github.com/FullFran/riffhero/internal/score/gp"
	"github.com/FullFran/riffhero/internal/score/midi"
	"github.com/FullFran/riffhero/internal/score/musicxml"
)

// Format is one supported score format.
type Format string

const (
	FormatGuitarPro Format = "guitarpro"
	FormatMusicXML  Format = "musicxml"
	FormatMIDI      Format = "midi"
	FormatUnknown   Format = ""
)

// Extensions lists what Load will accept, for help text and file dialogs.
var Extensions = []string{".gp", ".gpif", ".musicxml", ".mxl", ".xml", ".mid", ".midi"}

// Load reads a score from disk.
func Load(path string, clock practice.Clock) (*practice.Song, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading the score: %w", err)
	}

	song, err := Parse(data, path, clock)
	if err != nil {
		return nil, err
	}
	song.Source = filepath.Base(path)
	if song.Title == "" {
		song.Title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	return song, nil
}

// Parse reads a score from memory. hint is a file name used only to break ties
// the bytes cannot; it may be empty.
func Parse(data []byte, hint string, clock practice.Clock) (*practice.Song, error) {
	switch Detect(data, hint) {
	case FormatGuitarPro:
		return gp.Parse(data, clock)
	case FormatMusicXML:
		return musicxml.Parse(data, clock)
	case FormatMIDI:
		return midi.Parse(data, clock)
	default:
		return nil, fmt.Errorf("unrecognized score format%s; supported: %s",
			hintSuffix(hint), strings.Join(Extensions, " "))
	}
}

// Detect identifies a score format from its bytes, using the file name only
// where the bytes genuinely cannot decide.
func Detect(data []byte, hint string) Format {
	switch {
	case len(data) >= 4 && string(data[:4]) == "MThd":
		return FormatMIDI

	case bytes.HasPrefix(data, []byte("PK\x03\x04")):
		// Both Guitar Pro 7/8 and compressed MusicXML are ZIP archives, and
		// the only honest way to tell them apart is to look inside.
		return zipFormat(data, hint)

	case len(data) >= 4 && (string(data[:4]) == "BCFS" || string(data[:4]) == "BCFZ"):
		// Guitar Pro 6. The gp importer refuses it with an error that says so,
		// which is more useful than "unrecognized format".
		return FormatGuitarPro

	case bytes.Contains(head(data, 64), []byte("FICHIER GUITAR PRO")):
		return FormatGuitarPro

	case looksLikeXML(data):
		// Both formats are XML, so "starts with a <" is not an answer. The
		// root element is.
		head := head(data, 4096)
		if bytes.Contains(head, []byte("<GPIF")) {
			return FormatGuitarPro
		}
		return FormatMusicXML
	}

	switch strings.ToLower(filepath.Ext(hint)) {
	case ".gp", ".gpif", ".gpx", ".gp3", ".gp4", ".gp5":
		return FormatGuitarPro
	case ".musicxml", ".mxl", ".xml":
		return FormatMusicXML
	case ".mid", ".midi":
		return FormatMIDI
	}
	return FormatUnknown
}

// zipFormat looks inside an archive for the entry that identifies it.
func zipFormat(data []byte, hint string) Format {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		// A ZIP we cannot open is still more likely to be whatever the name
		// says than nothing at all; let the importer produce the real error.
		if strings.EqualFold(filepath.Ext(hint), ".gp") {
			return FormatGuitarPro
		}
		return FormatMusicXML
	}
	for _, f := range r.File {
		switch {
		case strings.EqualFold(filepath.Base(f.Name), "score.gpif"):
			return FormatGuitarPro
		case strings.EqualFold(f.Name, "META-INF/container.xml"):
			return FormatMusicXML
		}
	}
	if strings.EqualFold(filepath.Ext(hint), ".gp") {
		return FormatGuitarPro
	}
	return FormatMusicXML
}

// looksLikeXML skips a byte-order mark and leading whitespace before deciding.
func looksLikeXML(data []byte) bool {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	data = bytes.TrimLeft(head(data, 256), " \t\r\n")
	return len(data) > 0 && data[0] == '<'
}

func head(data []byte, n int) []byte {
	if len(data) < n {
		return data
	}
	return data[:n]
}

func hintSuffix(hint string) string {
	if hint == "" {
		return ""
	}
	return " for " + filepath.Base(hint)
}
