package score

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/FullFran/riffhero/internal/practice"
)

func zipWith(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, body := range entries {
		f, err := w.Create(name)
		if err != nil {
			t.Fatalf("creating %s: %v", name, err)
		}
		if _, err := f.Write([]byte(body)); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("closing the archive: %v", err)
	}
	return buf.Bytes()
}

func TestDetectByContent(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		hint string
		want Format
	}{
		{"midi", []byte("MThd\x00\x00\x00\x06"), "", FormatMIDI},
		{"midi despite a wrong extension", []byte("MThd\x00\x00\x00\x06"), "song.xml", FormatMIDI},
		{"bare musicxml", []byte(`<?xml version="1.0"?><score-partwise/>`), "", FormatMusicXML},
		{"musicxml with a byte-order mark", append([]byte{0xEF, 0xBB, 0xBF}, `<score-partwise/>`...), "", FormatMusicXML},
		{"guitar pro 6", []byte("BCFS...."), "", FormatGuitarPro},
		{"guitar pro 5", []byte("\x18FICHIER GUITAR PRO v5.10"), "", FormatGuitarPro},
		{"nothing recognizable", []byte("hello"), "", FormatUnknown},
	}
	for _, c := range cases {
		if got := Detect(c.data, c.hint); got != c.want {
			t.Fatalf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDetectLooksInsideAnArchive(t *testing.T) {
	// A .gp and an .mxl are both ZIPs. Only what is inside can tell them
	// apart, and trusting the extension gets it wrong on a renamed file.
	gpBytes := zipWith(t, map[string]string{
		"VERSION":            "7.0",
		"Content/score.gpif": "<GPIF/>",
	})
	if got := Detect(gpBytes, "whatever.mxl"); got != FormatGuitarPro {
		t.Fatalf("guitar pro archive detected as %q", got)
	}

	mxlBytes := zipWith(t, map[string]string{
		"META-INF/container.xml": `<container><rootfiles><rootfile full-path="score.xml"/></rootfiles></container>`,
		"score.xml":              "<score-partwise/>",
	})
	if got := Detect(mxlBytes, "whatever.gp"); got != FormatMusicXML {
		t.Fatalf("compressed musicxml detected as %q", got)
	}
}

func TestDetectFallsBackToTheExtension(t *testing.T) {
	// Bytes we cannot read at all: the name is the only evidence left.
	cases := map[string]Format{
		"a.gp":       FormatGuitarPro,
		"a.gp5":      FormatGuitarPro,
		"a.mid":      FormatMIDI,
		"a.midi":     FormatMIDI,
		"a.musicxml": FormatMusicXML,
		"a.wav":      FormatUnknown,
	}
	for name, want := range cases {
		if got := Detect([]byte{0x00, 0x01, 0x02}, name); got != want {
			t.Fatalf("%s detected as %q, want %q", name, got, want)
		}
	}
}

func TestParseRoutesToTheRightImporter(t *testing.T) {
	clock := practice.Clock{SampleRate: 48000}

	xml := []byte(`<?xml version="1.0"?>
<score-partwise version="3.1">
  <part-list><score-part id="P1"><part-name>Guitar</part-name></score-part></part-list>
  <part id="P1"><measure number="1">
    <attributes><divisions>1</divisions><time><beats>4</beats><beat-type>4</beat-type></time></attributes>
    <note><pitch><step>E</step><octave>4</octave></pitch><duration>4</duration></note>
  </measure></part>
</score-partwise>`)

	song, err := Parse(xml, "test.musicxml", clock)
	if err != nil {
		t.Fatalf("parsing musicxml: %v", err)
	}
	if len(song.Tracks) == 0 || len(song.Tracks[0].Notes) == 0 {
		t.Fatalf("no notes came back: %+v", song)
	}
	if got := song.Tracks[0].Notes[0].MIDI; got != 64 {
		t.Fatalf("first note is MIDI %d, want 64", got)
	}
}

func TestParseRejectsWhatItCannotRead(t *testing.T) {
	_, err := Parse([]byte("not a score"), "mystery.bin", practice.Clock{SampleRate: 48000})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("mystery.bin")) {
		t.Fatalf("the error should name the file: %v", err)
	}
}

func TestLoadFillsInSourceAndTitle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "my riff.musicxml")
	xml := `<?xml version="1.0"?>
<score-partwise version="3.1">
  <part-list><score-part id="P1"><part-name>Guitar</part-name></score-part></part-list>
  <part id="P1"><measure number="1">
    <attributes><divisions>1</divisions></attributes>
    <note><pitch><step>A</step><octave>2</octave></pitch><duration>4</duration></note>
  </measure></part>
</score-partwise>`
	if err := os.WriteFile(path, []byte(xml), 0o644); err != nil {
		t.Fatal(err)
	}

	song, err := Load(path, practice.Clock{SampleRate: 48000})
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	if song.Source != "my riff.musicxml" {
		t.Fatalf("source %q", song.Source)
	}
	// The file carries no work title, so the file name stands in: a song with
	// no name at all is worse in a header than the name the player gave it.
	if song.Title != "my riff" {
		t.Fatalf("title %q, want the file name without its extension", song.Title)
	}
}

func TestLoadReportsAMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.gp"), practice.Clock{SampleRate: 48000})
	if err == nil {
		t.Fatal("expected an error for a missing file")
	}
}
