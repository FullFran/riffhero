// Package gp imports Guitar Pro 7/8 scores into the RiffHero practice model.
//
// A .gp file is a ZIP archive whose Content/score.gpif entry is a GPIF XML
// document. GPIF is not a tree of music: it is a set of flat, id-referenced
// tables — master bars, bars, voices, beats, notes, rhythms — joined by index.
// Following that indirection, and expanding the repeat structure into the
// linear timeline a practice session needs, is most of what this package does.
//
// This is the preferred score format for RiffHero because it is the only one
// that carries real tablature: a string and a fret per note, rather than a
// pitch that has to be guessed onto the neck.
//
// Guitar Pro 6 (.gpx, a BCFS container) and Guitar Pro 3-5 (.gp3/.gp4/.gp5,
// binary) are out of scope. They share nothing with GPIF beyond the vendor,
// and Guitar Pro exports either of them as .gp or MusicXML. Both are detected
// so the user is told what to do instead of being handed a parse error.
package gp

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/FullFran/riffhero/internal/practice"
)

// sourceName is what Song.Source carries when the score did not come from a
// named file, so the HUD can say where the music is from.
const sourceName = "Guitar Pro"

// gpifEntry is where Guitar Pro 7 and 8 put the score inside the archive.
const gpifEntry = "Content/score.gpif"

// maxGPIFSize bounds how much XML is decompressed out of the archive. A ZIP
// entry declares its own uncompressed size and a crafted one can declare a lot
// of it; a score is a few megabytes of text at the very worst.
const maxGPIFSize = 128 << 20

// errUnsupportedFormat marks a file RiffHero recognised and deliberately will
// not read, as opposed to one it failed to read. Callers can tell the two
// apart with errors.Is, which is the difference between "convert this" and
// "this file is broken".
var errUnsupportedFormat = errors.New("unsupported Guitar Pro format")

// Parse reads a Guitar Pro 7/8 archive held in memory. A bare GPIF document is
// accepted too, so callers do not have to know which of the two they have.
func Parse(data []byte, clock practice.Clock) (*practice.Song, error) {
	payload, err := extractGPIF(data)
	if err != nil {
		return nil, err
	}
	return ParseGPIF(payload, clock)
}

// ParseFile reads a Guitar Pro 7/8 file from disk. The resulting song's Source
// is the path it came from.
func ParseFile(path string, clock practice.Clock) (*practice.Song, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("gp: reading %s: %w", path, err)
	}
	song, err := Parse(data, clock)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	song.Source = path
	return song, nil
}

// ParseGPIF reads a bare GPIF XML document, which is what the archive holds.
func ParseGPIF(data []byte, clock practice.Clock) (*practice.Song, error) {
	if clock.SampleRate <= 0 {
		return nil, fmt.Errorf("gp: sample rate must be positive, got %d", clock.SampleRate)
	}

	var doc gpif
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("gp: parsing GPIF: %w", err)
	}

	tl, err := buildTimeline(&doc, clock)
	if err != nil {
		return nil, err
	}

	b := &builder{doc: &doc, tab: indexTables(&doc), tl: tl, clock: clock}
	song := b.build()
	song.Normalize()
	return song, nil
}

// extractGPIF pulls the GPIF document out of whatever container it arrived in,
// and refuses the Guitar Pro formats that are not GPIF at all.
//
// Detection is by content, not by file extension: a .gp5 renamed to .gp is a
// common way to end up here, and the magic bytes are the only honest answer.
func extractGPIF(data []byte) ([]byte, error) {
	switch {
	case len(data) == 0:
		return nil, fmt.Errorf("gp: the file is empty")

	case isZip(data):
		return readArchive(data)

	case hasPrefix(data, "BCFS"), hasPrefix(data, "BCFZ"):
		return nil, fmt.Errorf(
			"gp: this is a Guitar Pro 6 (.gpx) file, not a Guitar Pro 7/8 (.gp) one: %w; "+
				"open it in Guitar Pro and use File > Export as .gp or as MusicXML",
			errUnsupportedFormat)

	case isLegacyGuitarPro(data):
		return nil, fmt.Errorf(
			"gp: this is a %s file, not a Guitar Pro 7/8 (.gp) one: %w; "+
				"open it in Guitar Pro and use File > Export as .gp or as MusicXML",
			legacyVersion(data), errUnsupportedFormat)

	case looksLikeXML(data):
		return data, nil
	}
	return nil, fmt.Errorf("gp: unrecognised file: it is neither a .gp archive nor a GPIF document")
}

// isZip reports the ZIP local-file-header magic. The empty- and
// spanned-archive markers are accepted too so a degenerate archive produces a
// "no score inside" error rather than "unrecognised file".
func isZip(data []byte) bool {
	return hasPrefix(data, "PK\x03\x04") || hasPrefix(data, "PK\x05\x06") || hasPrefix(data, "PK\x07\x08")
}

// isLegacyGuitarPro detects GP3/4/5, whose files open with a Pascal-style
// version string: one length byte followed by "FICHIER GUITAR PRO v5.10".
func isLegacyGuitarPro(data []byte) bool {
	return len(data) > 1 && bytes.HasPrefix(data[1:], []byte("FICHIER GUITAR PRO"))
}

// legacyVersion reads the version string back so the error can name the exact
// format the user is holding.
func legacyVersion(data []byte) string {
	n := int(data[0])
	if n <= 0 || 1+n > len(data) {
		return "Guitar Pro 3-5"
	}
	v := strings.TrimSpace(string(data[1 : 1+n]))
	if v == "" {
		return "Guitar Pro 3-5"
	}
	return v
}

// looksLikeXML tolerates a byte-order mark and leading whitespace, which some
// exporters emit ahead of the declaration.
func looksLikeXML(data []byte) bool {
	body := bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	body = bytes.TrimLeft(body, " \t\r\n")
	return len(body) > 0 && body[0] == '<'
}

func hasPrefix(data []byte, prefix string) bool {
	return bytes.HasPrefix(data, []byte(prefix))
}

// readArchive finds the score inside a .gp archive.
func readArchive(data []byte) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("gp: reading the .gp archive: %w", err)
	}

	entry := findEntry(zr)
	if entry == nil {
		return nil, fmt.Errorf("gp: the archive has no %s entry, so it is not a Guitar Pro 7/8 score", gpifEntry)
	}

	rc, err := entry.Open()
	if err != nil {
		return nil, fmt.Errorf("gp: opening %s: %w", entry.Name, err)
	}
	defer rc.Close()

	payload, err := io.ReadAll(io.LimitReader(rc, maxGPIFSize+1))
	if err != nil {
		return nil, fmt.Errorf("gp: reading %s: %w", entry.Name, err)
	}
	if len(payload) > maxGPIFSize {
		return nil, fmt.Errorf("gp: %s is larger than %d bytes, which no real score is", entry.Name, maxGPIFSize)
	}
	return payload, nil
}

// findEntry prefers the documented path and falls back to any entry with the
// right base name, because a few tools rebuild the archive with a different
// prefix or with backslashes in the path.
func findEntry(zr *zip.Reader) *zip.File {
	for _, f := range zr.File {
		if f.Name == gpifEntry {
			return f
		}
	}
	for _, f := range zr.File {
		name := strings.ReplaceAll(f.Name, "\\", "/")
		if strings.EqualFold(path.Base(name), "score.gpif") {
			return f
		}
	}
	return nil
}
