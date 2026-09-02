// Package musicxml imports MusicXML scores — raw score-partwise XML or a
// zipped .mxl container — into RiffHero's normalized practice.Song model.
package musicxml

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/FullFran/riffhero/internal/practice"
)

// zipMagic is the four-byte signature every ZIP local-file header starts
// with. .mxl is just a ZIP container, so this is what tells it apart from a
// raw XML document without trusting a file extension that may not even
// exist (Parse takes bytes, not a path).
var zipMagic = []byte("PK\x03\x04")

// Parse reads a MusicXML score held in memory. Both raw `score-partwise` XML
// and a zipped `.mxl` container are accepted; the format is detected from the
// bytes, not from a file name.
func Parse(data []byte, clock practice.Clock) (*practice.Song, error) {
	raw := data
	if bytes.HasPrefix(data, zipMagic) {
		extracted, err := extractContainer(data)
		if err != nil {
			return nil, err
		}
		raw = extracted
	}

	if err := checkRoot(raw); err != nil {
		return nil, err
	}

	var doc scorePartwiseXML
	if err := xml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("musicxml: %w", err)
	}

	return build(doc, clock)
}

// ParseFile reads a MusicXML score from disk.
func ParseFile(path string, clock practice.Clock) (*practice.Song, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("musicxml: read %s: %w", path, err)
	}
	song, err := Parse(data, clock)
	if err != nil {
		return nil, err
	}
	song.Source = path
	return song, nil
}

// checkRoot validates the document is score-partwise before the real parse
// runs, so score-timewise gets a message that says exactly what is
// unsupported instead of failing deep inside conversion with a confusing
// field-mismatch error — or worse, silently decoding nothing because none of
// score-timewise's differently-shaped elements matched our struct tags.
func checkRoot(raw []byte) error {
	var probe struct {
		XMLName xml.Name
	}
	if err := xml.Unmarshal(raw, &probe); err != nil {
		return fmt.Errorf("musicxml: %w", err)
	}
	switch probe.XMLName.Local {
	case "score-partwise":
		return nil
	case "score-timewise":
		return errors.New("musicxml: score-timewise documents are not supported (only score-partwise is)")
	default:
		return fmt.Errorf("musicxml: unrecognized root element %q", probe.XMLName.Local)
	}
}

// extractContainer pulls the score document out of a .mxl zip: it reads
// META-INF/container.xml for the canonical rootfile path, and when that is
// missing or unreadable — some exporters get this wrong — falls back to the
// first non-META-INF entry that looks like a score.
func extractContainer(data []byte) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("musicxml: invalid .mxl container: %w", err)
	}

	name := rootfilePath(zr)
	if name == "" {
		name = firstScoreFile(zr)
	}
	if name == "" {
		return nil, errors.New("musicxml: .mxl container has no score file")
	}

	f, err := zr.Open(name)
	if err != nil {
		return nil, fmt.Errorf("musicxml: open %s in .mxl: %w", name, err)
	}
	defer f.Close()

	raw, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("musicxml: read %s in .mxl: %w", name, err)
	}
	return raw, nil
}

func rootfilePath(zr *zip.Reader) string {
	f, err := zr.Open("META-INF/container.xml")
	if err != nil {
		return ""
	}
	defer f.Close()

	var c containerXML
	if err := xml.NewDecoder(f).Decode(&c); err != nil {
		return ""
	}
	for _, rf := range c.Rootfiles.Rootfile {
		if rf.FullPath != "" {
			return rf.FullPath
		}
	}
	return ""
}

func firstScoreFile(zr *zip.Reader) string {
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "META-INF/") {
			continue
		}
		lower := strings.ToLower(f.Name)
		if strings.HasSuffix(lower, ".xml") || strings.HasSuffix(lower, ".musicxml") {
			return f.Name
		}
	}
	return ""
}
