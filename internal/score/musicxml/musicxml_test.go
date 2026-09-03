package musicxml

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"

	"github.com/FullFran/riffhero/internal/practice"
)

func testClock() practice.Clock { return practice.Clock{SampleRate: 48000} }

func TestParseMinimalScore(t *testing.T) {
	const doc = `<?xml version="1.0" encoding="UTF-8"?>
<score-partwise version="3.1">
  <work><work-title>Test Song</work-title></work>
  <identification>
    <creator type="composer">Jane Doe</creator>
  </identification>
  <part-list>
    <score-part id="P1">
      <part-name>Guitar</part-name>
      <score-instrument id="P1-I1"><instrument-name>Acoustic Guitar</instrument-name></score-instrument>
    </score-part>
  </part-list>
  <part id="P1">
    <measure number="1">
      <attributes>
        <divisions>1</divisions>
        <time><beats>4</beats><beat-type>4</beat-type></time>
      </attributes>
      <direction><sound tempo="120"/></direction>
      <note>
        <pitch><step>C</step><octave>4</octave></pitch>
        <duration>1</duration>
      </note>
    </measure>
  </part>
</score-partwise>`

	clock := testClock()
	song, err := Parse([]byte(doc), clock)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if song.Title != "Test Song" {
		t.Errorf("Title = %q, want %q", song.Title, "Test Song")
	}
	if song.Artist != "Jane Doe" {
		t.Errorf("Artist = %q, want %q", song.Artist, "Jane Doe")
	}
	if len(song.Grid) != 1 {
		t.Fatalf("len(Grid) = %d, want 1", len(song.Grid))
	}
	if song.Grid[0].BPM != 120 {
		t.Errorf("Grid[0].BPM = %v, want 120", song.Grid[0].BPM)
	}
	if song.Grid[0].Sig != (practice.TimeSignature{Beats: 4, Unit: 4}) {
		t.Errorf("Grid[0].Sig = %+v, want 4/4", song.Grid[0].Sig)
	}

	if len(song.Tracks) != 1 {
		t.Fatalf("len(Tracks) = %d, want 1", len(song.Tracks))
	}
	tr := song.Tracks[0]
	if tr.Name != "Guitar" || tr.Instrument != "Acoustic Guitar" {
		t.Errorf("track meta = %+v", tr)
	}
	if tr.Tuning != practice.StandardTuning {
		t.Errorf("Tuning = %+v, want StandardTuning", tr.Tuning)
	}
	if len(tr.Notes) != 1 {
		t.Fatalf("len(Notes) = %d, want 1", len(tr.Notes))
	}

	want := practice.Note{Start: 0, Duration: clock.Frames(0.5), MIDI: 60, String: 2, Fret: 1}
	if got := tr.Notes[0]; got != want {
		t.Errorf("note = %+v, want %+v", got, want)
	}
}

func TestParseChord(t *testing.T) {
	const doc = `<?xml version="1.0" encoding="UTF-8"?>
<score-partwise version="3.1">
  <part-list><score-part id="P1"><part-name>Guitar</part-name></score-part></part-list>
  <part id="P1">
    <measure number="1">
      <attributes>
        <divisions>1</divisions>
        <time><beats>4</beats><beat-type>4</beat-type></time>
      </attributes>
      <direction><sound tempo="120"/></direction>
      <note>
        <pitch><step>C</step><octave>4</octave></pitch>
        <duration>4</duration>
      </note>
      <note>
        <chord/>
        <pitch><step>E</step><octave>4</octave></pitch>
        <duration>4</duration>
      </note>
      <note>
        <chord/>
        <pitch><step>G</step><octave>4</octave></pitch>
        <duration>4</duration>
      </note>
    </measure>
  </part>
</score-partwise>`

	clock := testClock()
	song, err := Parse([]byte(doc), clock)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(song.Tracks) != 1 {
		t.Fatalf("len(Tracks) = %d, want 1", len(song.Tracks))
	}
	notes := song.Tracks[0].Notes
	if len(notes) != 3 {
		t.Fatalf("len(Notes) = %d, want 3", len(notes))
	}

	wantDur := clock.Frames(2.0)
	wantMIDIs := []uint8{60, 64, 67} // C4, E4, G4, in Normalize's sort order
	for i, n := range notes {
		if n.Start != 0 {
			t.Errorf("note %d Start = %d, want 0 (a chord starts together)", i, n.Start)
		}
		if n.Duration != wantDur {
			t.Errorf("note %d Duration = %d, want %d", i, n.Duration, wantDur)
		}
		if n.MIDI != wantMIDIs[i] {
			t.Errorf("note %d MIDI = %d, want %d", i, n.MIDI, wantMIDIs[i])
		}
	}
}

func TestParseBackupTwoVoices(t *testing.T) {
	const doc = `<?xml version="1.0" encoding="UTF-8"?>
<score-partwise version="3.1">
  <part-list><score-part id="P1"><part-name>Guitar</part-name></score-part></part-list>
  <part id="P1">
    <measure number="1">
      <attributes>
        <divisions>2</divisions>
        <time><beats>4</beats><beat-type>4</beat-type></time>
      </attributes>
      <direction><sound tempo="120"/></direction>
      <note>
        <pitch><step>C</step><octave>4</octave></pitch>
        <duration>2</duration>
        <voice>1</voice>
        <notations><technical><string>2</string><fret>1</fret></technical></notations>
      </note>
      <note>
        <pitch><step>D</step><octave>4</octave></pitch>
        <duration>2</duration>
        <voice>1</voice>
        <notations><technical><string>2</string><fret>3</fret></technical></notations>
      </note>
      <backup><duration>4</duration></backup>
      <note>
        <pitch><step>E</step><octave>3</octave></pitch>
        <duration>4</duration>
        <voice>2</voice>
        <notations><technical><string>4</string><fret>2</fret></technical></notations>
      </note>
    </measure>
  </part>
</score-partwise>`

	clock := testClock()
	song, err := Parse([]byte(doc), clock)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	notes := song.Tracks[0].Notes
	if len(notes) != 3 {
		t.Fatalf("len(Notes) = %d, want 3", len(notes))
	}

	// Normalize sorts by Start then MIDI: E3 (voice 2, backed up to the
	// measure start) and C4 (voice 1's first note) both start at 0, then D4
	// (voice 1's second note) a quarter note later.
	want := []practice.Note{
		{Start: 0, Duration: clock.Frames(1.0), MIDI: 52, String: 4, Fret: 2},
		{Start: 0, Duration: clock.Frames(0.5), MIDI: 60, String: 2, Fret: 1},
		{Start: clock.Frames(0.5), Duration: clock.Frames(0.5), MIDI: 62, String: 2, Fret: 3},
	}
	for i, w := range want {
		if notes[i] != w {
			t.Errorf("note %d = %+v, want %+v", i, notes[i], w)
		}
	}
}

func TestParseTieAcrossBarline(t *testing.T) {
	const doc = `<?xml version="1.0" encoding="UTF-8"?>
<score-partwise version="3.1">
  <part-list><score-part id="P1"><part-name>Guitar</part-name></score-part></part-list>
  <part id="P1">
    <measure number="1">
      <attributes>
        <divisions>1</divisions>
        <time><beats>4</beats><beat-type>4</beat-type></time>
      </attributes>
      <direction><sound tempo="120"/></direction>
      <note>
        <pitch><step>C</step><octave>4</octave></pitch>
        <duration>4</duration>
        <voice>1</voice>
        <tie type="start"/>
      </note>
    </measure>
    <measure number="2">
      <note>
        <pitch><step>C</step><octave>4</octave></pitch>
        <duration>2</duration>
        <voice>1</voice>
        <tie type="stop"/>
      </note>
    </measure>
  </part>
</score-partwise>`

	clock := testClock()
	song, err := Parse([]byte(doc), clock)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	notes := song.Tracks[0].Notes
	if len(notes) != 1 {
		t.Fatalf("len(Notes) = %d, want 1 (tied notes merge into one)", len(notes))
	}

	want := practice.Note{Start: 0, Duration: clock.Frames(2.0) + clock.Frames(1.0), MIDI: 60, String: 2, Fret: 1}
	if got := notes[0]; got != want {
		t.Errorf("tied note = %+v, want %+v", got, want)
	}
}

func TestParseTechnicalStringFretRespectedVerbatim(t *testing.T) {
	// String 6 fret 15 sounds the same pitch (G3, MIDI 55) that a bare
	// fretboard placement would put at string 3 fret 0 with a fresh hand
	// position. Getting string 6 back proves the technical notation wins
	// instead of being silently overridden by the fretboard guess.
	const doc = `<?xml version="1.0" encoding="UTF-8"?>
<score-partwise version="3.1">
  <part-list><score-part id="P1"><part-name>Guitar</part-name></score-part></part-list>
  <part id="P1">
    <measure number="1">
      <attributes><divisions>1</divisions></attributes>
      <direction><sound tempo="120"/></direction>
      <note>
        <pitch><step>G</step><octave>3</octave></pitch>
        <duration>1</duration>
        <notations><technical><string>6</string><fret>15</fret></technical></notations>
      </note>
    </measure>
  </part>
</score-partwise>`

	song, err := Parse([]byte(doc), testClock())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	notes := song.Tracks[0].Notes
	if len(notes) != 1 {
		t.Fatalf("len(Notes) = %d, want 1", len(notes))
	}
	if notes[0].String != 6 || notes[0].Fret != 15 {
		t.Errorf("String/Fret = %d/%d, want 6/15", notes[0].String, notes[0].Fret)
	}
	if notes[0].MIDI != 55 {
		t.Errorf("MIDI = %d, want 55", notes[0].MIDI)
	}
}

func TestParseTabStaffTuning(t *testing.T) {
	// Drop D, written the way a real tab-staff export does: staff-tuning
	// line 1 is the BOTTOM line of the staff (the lowest string), not string
	// 1 in RiffHero's high-string-first convention.
	const doc = `<?xml version="1.0" encoding="UTF-8"?>
<score-partwise version="3.1">
  <part-list><score-part id="P1"><part-name>Guitar</part-name></score-part></part-list>
  <part id="P1">
    <measure number="1">
      <attributes>
        <divisions>1</divisions>
        <staff-details>
          <staff-lines>6</staff-lines>
          <staff-tuning line="1"><tuning-step>D</tuning-step><tuning-octave>2</tuning-octave></staff-tuning>
          <staff-tuning line="2"><tuning-step>A</tuning-step><tuning-octave>2</tuning-octave></staff-tuning>
          <staff-tuning line="3"><tuning-step>D</tuning-step><tuning-octave>3</tuning-octave></staff-tuning>
          <staff-tuning line="4"><tuning-step>G</tuning-step><tuning-octave>3</tuning-octave></staff-tuning>
          <staff-tuning line="5"><tuning-step>B</tuning-step><tuning-octave>3</tuning-octave></staff-tuning>
          <staff-tuning line="6"><tuning-step>E</tuning-step><tuning-octave>4</tuning-octave></staff-tuning>
        </staff-details>
      </attributes>
      <direction><sound tempo="120"/></direction>
      <note>
        <pitch><step>D</step><octave>2</octave></pitch>
        <duration>1</duration>
      </note>
    </measure>
  </part>
</score-partwise>`

	song, err := Parse([]byte(doc), testClock())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(song.Tracks) != 1 {
		t.Fatalf("len(Tracks) = %d, want 1", len(song.Tracks))
	}
	tuning := song.Tracks[0].Tuning
	if tuning.Strings != practice.DropDTuning.Strings {
		t.Errorf("Strings = %v, want %v (Drop D)", tuning.Strings, practice.DropDTuning.Strings)
	}
	// The open low string (D2) should fret on string 6 at 0, confirming the
	// fretboard placer is using the recovered tuning, not StandardTuning.
	notes := song.Tracks[0].Notes
	if len(notes) != 1 {
		t.Fatalf("len(Notes) = %d, want 1", len(notes))
	}
	if notes[0].String != 6 || notes[0].Fret != 0 {
		t.Errorf("String/Fret = %d/%d, want 6/0", notes[0].String, notes[0].Fret)
	}
}

func TestParseTempoChangeMidScore(t *testing.T) {
	const doc = `<?xml version="1.0" encoding="UTF-8"?>
<score-partwise version="3.1">
  <part-list><score-part id="P1"><part-name>Guitar</part-name></score-part></part-list>
  <part id="P1">
    <measure number="1">
      <attributes>
        <divisions>1</divisions>
        <time><beats>4</beats><beat-type>4</beat-type></time>
      </attributes>
      <direction><sound tempo="120"/></direction>
      <note>
        <pitch><step>C</step><octave>4</octave></pitch>
        <duration>4</duration>
        <notations><technical><string>2</string><fret>1</fret></technical></notations>
      </note>
    </measure>
    <measure number="2">
      <direction><sound tempo="90"/></direction>
      <note>
        <pitch><step>C</step><octave>4</octave></pitch>
        <duration>4</duration>
        <notations><technical><string>2</string><fret>1</fret></technical></notations>
      </note>
    </measure>
  </part>
</score-partwise>`

	clock := testClock()
	song, err := Parse([]byte(doc), clock)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(song.Grid) != 2 {
		t.Fatalf("len(Grid) = %d, want 2", len(song.Grid))
	}
	if song.Grid[0].BPM != 120 || song.Grid[1].BPM != 90 {
		t.Errorf("bar BPMs = %v/%v, want 120/90", song.Grid[0].BPM, song.Grid[1].BPM)
	}

	notes := song.Tracks[0].Notes
	if len(notes) != 2 {
		t.Fatalf("len(Notes) = %d, want 2", len(notes))
	}
	if notes[0].Start != 0 {
		t.Errorf("note 0 Start = %d, want 0", notes[0].Start)
	}
	if notes[1].Start != song.Grid[1].Start {
		t.Errorf("note 1 Start = %d, want bar 2 start %d", notes[1].Start, song.Grid[1].Start)
	}
	wantDur2 := clock.Frames(4.0 * 60.0 / 90.0)
	if notes[1].Duration != wantDur2 {
		t.Errorf("note 1 Duration = %d, want %d (computed at 90 BPM)", notes[1].Duration, wantDur2)
	}
}

func TestParseGraceNoteDoesNotAdvanceCursor(t *testing.T) {
	const doc = `<?xml version="1.0" encoding="UTF-8"?>
<score-partwise version="3.1">
  <part-list><score-part id="P1"><part-name>Guitar</part-name></score-part></part-list>
  <part id="P1">
    <measure number="1">
      <attributes>
        <divisions>1</divisions>
        <time><beats>4</beats><beat-type>4</beat-type></time>
      </attributes>
      <direction><sound tempo="120"/></direction>
      <note>
        <grace/>
        <pitch><step>D</step><octave>4</octave></pitch>
        <notations><technical><string>1</string><fret>10</fret></technical></notations>
      </note>
      <note>
        <pitch><step>C</step><octave>4</octave></pitch>
        <duration>4</duration>
        <notations><technical><string>2</string><fret>1</fret></technical></notations>
      </note>
    </measure>
  </part>
</score-partwise>`

	clock := testClock()
	song, err := Parse([]byte(doc), clock)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	notes := song.Tracks[0].Notes
	if len(notes) != 2 {
		t.Fatalf("len(Notes) = %d, want 2", len(notes))
	}
	// Both notes start at 0: the grace note because it never moves the
	// cursor, and the main note because the cursor is still where it was
	// before the grace note was processed.
	for i, n := range notes {
		if n.Start != 0 {
			t.Errorf("note %d Start = %d, want 0", i, n.Start)
		}
	}
	if notes[0].MIDI != 60 || notes[0].Duration != clock.Frames(2.0) {
		t.Errorf("main note = %+v", notes[0])
	}
	if notes[1].MIDI != 62 || notes[1].Duration != clock.Frames(graceNoteFraction*60.0/120.0) {
		t.Errorf("grace note = %+v", notes[1])
	}
}

func TestParseMXLContainer(t *testing.T) {
	const score = `<?xml version="1.0" encoding="UTF-8"?>
<score-partwise version="3.1">
  <work><work-title>Zipped Song</work-title></work>
  <part-list><score-part id="P1"><part-name>Guitar</part-name></score-part></part-list>
  <part id="P1">
    <measure number="1">
      <attributes><divisions>1</divisions></attributes>
      <direction><sound tempo="120"/></direction>
      <note>
        <pitch><step>C</step><octave>4</octave></pitch>
        <duration>1</duration>
      </note>
    </measure>
  </part>
</score-partwise>`

	const container = `<?xml version="1.0" encoding="UTF-8"?>
<container>
  <rootfiles>
    <rootfile full-path="score.xml" media-type="application/vnd.recordare.musicxml+xml"/>
  </rootfiles>
</container>`

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	writeZipFile(t, zw, "META-INF/container.xml", container)
	writeZipFile(t, zw, "score.xml", score)
	if err := zw.Close(); err != nil {
		t.Fatalf("zip Close: %v", err)
	}

	song, err := Parse(buf.Bytes(), testClock())
	if err != nil {
		t.Fatalf("Parse(.mxl): %v", err)
	}
	if song.Title != "Zipped Song" {
		t.Errorf("Title = %q, want %q", song.Title, "Zipped Song")
	}
	if len(song.Tracks) != 1 || len(song.Tracks[0].Notes) != 1 {
		t.Fatalf("Tracks = %+v", song.Tracks)
	}
	if song.Tracks[0].Notes[0].MIDI != 60 {
		t.Errorf("MIDI = %d, want 60", song.Tracks[0].Notes[0].MIDI)
	}
}

func writeZipFile(t *testing.T, zw *zip.Writer, name, content string) {
	t.Helper()
	w, err := zw.Create(name)
	if err != nil {
		t.Fatalf("zip Create(%s): %v", name, err)
	}
	if _, err := w.Write([]byte(content)); err != nil {
		t.Fatalf("zip Write(%s): %v", name, err)
	}
}

func TestParseScoreTimewiseRejected(t *testing.T) {
	const doc = `<?xml version="1.0" encoding="UTF-8"?>
<score-timewise version="3.1">
  <part-list><score-part id="P1"><part-name>Guitar</part-name></score-part></part-list>
  <measure number="1">
    <part id="P1">
      <note><pitch><step>C</step><octave>4</octave></pitch><duration>1</duration></note>
    </part>
  </measure>
</score-timewise>`

	_, err := Parse([]byte(doc), testClock())
	if err == nil {
		t.Fatal("Parse: want error for score-timewise, got nil")
	}
	if !strings.Contains(err.Error(), "score-timewise") {
		t.Errorf("error = %q, want it to mention score-timewise", err.Error())
	}
}

func TestParseMalformedXMLReturnsError(t *testing.T) {
	const doc = `<?xml version="1.0"?><score-partwise><part-list><score-part id="P1">`

	_, err := Parse([]byte(doc), testClock())
	if err == nil {
		t.Fatal("Parse: want error for malformed XML, got nil")
	}
}

func TestParseFileReadError(t *testing.T) {
	_, err := ParseFile("/nonexistent/does-not-exist.musicxml", testClock())
	if err == nil {
		t.Fatal("ParseFile: want error for a missing file, got nil")
	}
}

func TestParseDanglingTieDoesNotSwallowTheRestOfThePart(t *testing.T) {
	// Three written whole notes on the same pitch and voice. Only the first
	// carries a <tie type="start"/> and nothing ever carries the stop, which
	// is what a truncated export or a sloppy hand edit looks like.
	//
	// The pending tie used to absorb every later note on its key regardless of
	// whether that note continued the tie, and only an explicit stop could
	// clear it — so all three came back as one note of triple length and the
	// rest of the part disappeared with them.
	const doc = `<?xml version="1.0" encoding="UTF-8"?>
<score-partwise version="3.1">
  <part-list><score-part id="P1"><part-name>Guitar</part-name></score-part></part-list>
  <part id="P1">
    <measure number="1">
      <attributes>
        <divisions>1</divisions>
        <time><beats>4</beats><beat-type>4</beat-type></time>
      </attributes>
      <direction><sound tempo="120"/></direction>
      <note>
        <pitch><step>C</step><octave>4</octave></pitch>
        <duration>4</duration>
        <voice>1</voice>
        <tie type="start"/>
      </note>
    </measure>
    <measure number="2">
      <note>
        <pitch><step>C</step><octave>4</octave></pitch>
        <duration>4</duration>
        <voice>1</voice>
      </note>
    </measure>
    <measure number="3">
      <note>
        <pitch><step>C</step><octave>4</octave></pitch>
        <duration>4</duration>
        <voice>1</voice>
      </note>
    </measure>
  </part>
</score-partwise>`

	clock := testClock()
	song, err := Parse([]byte(doc), clock)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	notes := song.Tracks[0].Notes
	if len(notes) != 3 {
		t.Fatalf("len(Notes) = %d, want 3 (an unclosed tie must not eat the following notes): %+v", len(notes), notes)
	}

	bar := clock.Frames(4.0 * 60.0 / 120.0)
	for i, n := range notes {
		if n.Duration != bar {
			t.Errorf("note %d Duration = %d, want %d (one written whole note)", i, n.Duration, bar)
		}
		if n.Start != song.Grid[i].Start {
			t.Errorf("note %d Start = %d, want bar %d start %d", i, n.Start, i+1, song.Grid[i].Start)
		}
	}
}

func TestParseTieChainStopStartContinues(t *testing.T) {
	// The middle note of a three-note chain carries both a stop and a start.
	// It has to keep the tie open, not close it and open a second one.
	const doc = `<?xml version="1.0" encoding="UTF-8"?>
<score-partwise version="3.1">
  <part-list><score-part id="P1"><part-name>Guitar</part-name></score-part></part-list>
  <part id="P1">
    <measure number="1">
      <attributes>
        <divisions>1</divisions>
        <time><beats>4</beats><beat-type>4</beat-type></time>
      </attributes>
      <direction><sound tempo="120"/></direction>
      <note>
        <pitch><step>C</step><octave>4</octave></pitch>
        <duration>4</duration>
        <voice>1</voice>
        <tie type="start"/>
      </note>
    </measure>
    <measure number="2">
      <note>
        <pitch><step>C</step><octave>4</octave></pitch>
        <duration>4</duration>
        <voice>1</voice>
        <tie type="stop"/>
        <tie type="start"/>
      </note>
    </measure>
    <measure number="3">
      <note>
        <pitch><step>C</step><octave>4</octave></pitch>
        <duration>4</duration>
        <voice>1</voice>
        <tie type="stop"/>
      </note>
    </measure>
  </part>
</score-partwise>`

	clock := testClock()
	song, err := Parse([]byte(doc), clock)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	notes := song.Tracks[0].Notes
	if len(notes) != 1 {
		t.Fatalf("len(Notes) = %d, want 1 (the whole chain is one note): %+v", len(notes), notes)
	}
	want := practice.Note{Start: 0, Duration: 3 * clock.Frames(4.0*60.0/120.0), MIDI: 60, String: 2, Fret: 1}
	if notes[0] != want {
		t.Errorf("tied note = %+v, want %+v", notes[0], want)
	}
}

func TestParseUnterminatedTiesFlushInAStableOrder(t *testing.T) {
	// Two voices open a tie at the same instant on the same pitch, and neither
	// is ever stopped. They differ only in their tablature, so Normalize's
	// stable sort on (Start, MIDI) cannot separate them: whatever order the
	// leftovers are flushed in is the order the song comes out in.
	//
	// That flush used to be a range over a Go map. 200 runs of this document
	// split 172/28 between the two orderings — the same bytes, two songs.
	const doc = `<?xml version="1.0" encoding="UTF-8"?>
<score-partwise version="3.1">
  <part-list><score-part id="P1"><part-name>Guitar</part-name></score-part></part-list>
  <part id="P1">
    <measure number="1">
      <attributes>
        <divisions>1</divisions>
        <time><beats>4</beats><beat-type>4</beat-type></time>
      </attributes>
      <direction><sound tempo="120"/></direction>
      <note>
        <pitch><step>G</step><octave>3</octave></pitch>
        <duration>4</duration>
        <voice>1</voice>
        <tie type="start"/>
        <notations><technical><string>3</string><fret>0</fret></technical></notations>
      </note>
      <backup><duration>4</duration></backup>
      <note>
        <pitch><step>G</step><octave>3</octave></pitch>
        <duration>4</duration>
        <voice>2</voice>
        <tie type="start"/>
        <notations><technical><string>6</string><fret>15</fret></technical></notations>
      </note>
    </measure>
  </part>
</score-partwise>`

	first, err := Parse([]byte(doc), testClock())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := first.Tracks[0].Notes
	if len(want) != 2 {
		t.Fatalf("len(Notes) = %d, want 2: %+v", len(want), want)
	}
	if want[0].String == want[1].String {
		t.Fatalf("the two notes must differ in tablature or the ordering is unobservable: %+v", want)
	}

	// The ties are opened in document order, so voice 1's note comes first.
	if want[0].String != 3 || want[1].String != 6 {
		t.Errorf("flush order = strings %d then %d, want 3 then 6 (the order the ties were opened)", want[0].String, want[1].String)
	}

	for run := 0; run < 200; run++ {
		song, err := Parse([]byte(doc), testClock())
		if err != nil {
			t.Fatalf("Parse (run %d): %v", run, err)
		}
		got := song.Tracks[0].Notes
		if len(got) != len(want) {
			t.Fatalf("run %d: len(Notes) = %d, want %d", run, len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("run %d: note %d = %+v, want %+v (the same bytes must give the same song)", run, i, got[i], want[i])
			}
		}
	}
}

func TestParseMXLEntryLargerThanTheLimitIsRefused(t *testing.T) {
	// A .mxl entry is deflated XML, and deflate reaches about 1000x: a 255 KiB
	// upload unpacks to 256 MiB. The reader used to io.ReadAll the entry with
	// no bound at all, so the archive decided how much memory to take.
	//
	// The real limit is 128 MiB; shrinking it here buys the same coverage
	// without building a 128 MiB bomb. Nothing in this package runs in
	// parallel, so the swap is safe.
	defer func(saved int64) { maxScoreSize = saved }(maxScoreSize)
	maxScoreSize = 4096

	const container = `<?xml version="1.0" encoding="UTF-8"?>
<container>
  <rootfiles>
    <rootfile full-path="score.xml" media-type="application/vnd.recordare.musicxml+xml"/>
  </rootfiles>
</container>`

	// Highly compressible padding inside an XML comment: small on disk, well
	// past the limit once inflated, and still a valid document if it ever got
	// that far.
	bomb := `<?xml version="1.0" encoding="UTF-8"?>
<score-partwise version="3.1"><!--` + strings.Repeat("A", int(maxScoreSize)*4) + `-->
  <part-list><score-part id="P1"><part-name>Guitar</part-name></score-part></part-list>
  <part id="P1"><measure number="1"/></part>
</score-partwise>`

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	writeZipFile(t, zw, "META-INF/container.xml", container)
	writeZipFile(t, zw, "score.xml", bomb)
	if err := zw.Close(); err != nil {
		t.Fatalf("zip Close: %v", err)
	}

	if int64(buf.Len()) > maxScoreSize {
		t.Fatalf("the compressed archive is %d bytes, which is not smaller than the limit it has to defeat", buf.Len())
	}

	_, err := Parse(buf.Bytes(), testClock())
	if err == nil {
		t.Fatal("Parse: want an error for an oversized .mxl entry, got nil")
	}
	if !strings.Contains(err.Error(), "larger than") {
		t.Errorf("error = %q, want it to say the entry is too large", err.Error())
	}
}
