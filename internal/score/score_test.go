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
		{"bare gpif is xml too, and its root element is what says so",
			[]byte(`<?xml version="1.0"?><GPIF><Score/></GPIF>`), "", FormatGuitarPro},
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

// buildMIDI writes a one-track SMF whose notes are deliberately outside a
// guitar's range, so the importer's transposition is exercised.
func buildMIDI(pitches []byte) []byte {
	var track []byte
	vlq := func(v int) []byte {
		if v == 0 {
			return []byte{0}
		}
		var out []byte
		for v > 0 {
			out = append([]byte{byte(v & 0x7f)}, out...)
			v >>= 7
		}
		for i := 0; i < len(out)-1; i++ {
			out[i] |= 0x80
		}
		return out
	}
	for _, p := range pitches {
		track = append(track, 0, 0x90, p, 100)
		track = append(track, vlq(240)...)
		track = append(track, 0x80, p, 0)
	}
	track = append(track, 0, 0xFF, 0x2F, 0)

	head := []byte{'M', 'T', 'h', 'd', 0, 0, 0, 6, 0, 0, 0, 1, 0x01, 0xE0}
	n := len(track)
	chunk := append([]byte{'M', 'T', 'r', 'k', byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}, track...)
	return append(head, chunk...)
}

// TestEveryImportedNoteSoundsWhatItsTabSays is a property every importer has
// to hold, and one of them did not: an out-of-range note kept its written
// pitch while its string and fret came from a clamped one, so the tab showed a
// position that sounds something else and the scorer waited for a pitch no
// guitar can produce. The player could satisfy neither.
func TestEveryImportedNoteSoundsWhatItsTabSays(t *testing.T) {
	clock := practice.Clock{SampleRate: 48000}

	cases := []struct {
		name string
		data []byte
		hint string
	}{
		{
			name: "midi far outside the guitar's range",
			data: buildMIDI([]byte{20, 33, 40, 64, 88, 100, 120}),
			hint: "part.mid",
		},
		{
			name: "guitar pro, whose tablature is the source of truth",
			hint: "part.gp",
			data: []byte(`<GPIF>
  <Score><Title>Riff</Title></Score>
  <MasterTrack><Tracks>0</Tracks><Automations><Automation><Type>Tempo</Type><Bar>0</Bar><Position>0</Position><Value>120 2</Value></Automation></Automations></MasterTrack>
  <Tracks><Track id="0"><Name>Guitar</Name><Staves><Staff><Properties>
    <Property name="Tuning"><Pitches>40 45 50 55 59 64</Pitches></Property>
  </Properties></Staff></Staves></Track></Tracks>
  <MasterBars><MasterBar><Time>4/4</Time><Bars>0</Bars></MasterBar></MasterBars>
  <Bars><Bar id="0"><Voices>0 -1 -1 -1</Voices></Bar></Bars>
  <Voices><Voice id="0"><Beats>0 1 2 3</Beats></Voice></Voices>
  <Beats>
    <Beat id="0"><Rhythm ref="0"/><Notes>0</Notes></Beat>
    <Beat id="1"><Rhythm ref="0"/><Notes>1</Notes></Beat>
    <Beat id="2"><Rhythm ref="0"/><Notes>2</Notes></Beat>
    <Beat id="3"><Rhythm ref="0"/><Notes>3</Notes></Beat>
  </Beats>
  <Notes>
    <Note id="0"><Properties><Property name="String"><String>0</String></Property><Property name="Fret"><Fret>0</Fret></Property></Properties></Note>
    <Note id="1"><Properties><Property name="String"><String>2</String></Property><Property name="Fret"><Fret>7</Fret></Property></Properties></Note>
    <Note id="2"><Properties><Property name="String"><String>5</String></Property><Property name="Fret"><Fret>12</Fret></Property></Properties></Note>
    <Note id="3"><Properties><Property name="String"><String>4</String></Property><Property name="Fret"><Fret>24</Fret></Property></Properties></Note>
  </Notes>
  <Rhythms><Rhythm id="0"><NoteValue>Quarter</NoteValue></Rhythm></Rhythms>
</GPIF>`),
		},
		{
			name: "musicxml whose tablature contradicts its own pitch",
			hint: "part.musicxml",
			data: []byte(`<?xml version="1.0"?>
<score-partwise version="3.1">
  <part-list><score-part id="P1"><part-name>Guitar</part-name></score-part></part-list>
  <part id="P1"><measure number="1">
    <attributes><divisions>1</divisions></attributes>
    <note><pitch><step>C</step><octave>4</octave></pitch><duration>1</duration>
      <notations><technical><string>3</string><fret>5</fret></technical></notations></note>
    <note><pitch><step>E</step><octave>1</octave></pitch><duration>1</duration></note>
    <note><pitch><step>A</step><octave>2</octave></pitch><duration>1</duration>
      <notations><technical><string>5</string><fret>0</fret></technical></notations></note>
  </measure></part>
</score-partwise>`),
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			song, err := Parse(c.data, c.hint, clock)
			if err != nil {
				t.Fatalf("parsing: %v", err)
			}
			total := 0
			for _, track := range song.Tracks {
				for i, n := range track.Notes {
					total++
					if !track.Tuning.Sounds(n.MIDI, n.String, n.Fret) {
						t.Fatalf("note %d says MIDI %d but string %d fret %d sounds %d",
							i, n.MIDI, n.String, n.Fret, track.Tuning.MIDI(n.String, n.Fret))
					}
				}
			}
			if total == 0 {
				t.Fatal("nothing was imported, so the property proves nothing")
			}
		})
	}
}
