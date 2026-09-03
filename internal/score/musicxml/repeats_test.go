package musicxml

import (
	"strings"
	"testing"

	"github.com/FullFran/riffhero/internal/practice"
)

// midis is the pitch sequence of a track in Normalize's order, which for these
// scores (one note per bar) is the order the music is played in.
func midis(notes []practice.Note) []uint8 {
	out := make([]uint8, len(notes))
	for i, n := range notes {
		out[i] = n.MIDI
	}
	return out
}

func equalMIDIs(a, b []uint8) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestParseBackwardRepeatIsExpanded(t *testing.T) {
	// docs/architecture.md: "Repeats are expanded at import. A practice
	// timeline is linear." Before <barline> was parsed at all it fell through
	// decodeChild's default Skip, so this score imported as two bars and the
	// player was reading four.
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
      <note>
        <pitch><step>D</step><octave>4</octave></pitch>
        <duration>4</duration>
        <notations><technical><string>2</string><fret>3</fret></technical></notations>
      </note>
      <barline location="right">
        <bar-style>light-heavy</bar-style>
        <repeat direction="backward"/>
      </barline>
    </measure>
  </part>
</score-partwise>`

	song, err := Parse([]byte(doc), testClock())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(song.Grid) != 4 {
		t.Fatalf("len(Grid) = %d, want 4 (two bars played twice)", len(song.Grid))
	}

	notes := song.Tracks[0].Notes
	want := []uint8{60, 62, 60, 62}
	if got := midis(notes); !equalMIDIs(got, want) {
		t.Fatalf("MIDIs = %v, want %v", got, want)
	}
	for i, n := range notes {
		if n.Start != song.Grid[i].Start {
			t.Errorf("note %d Start = %d, want bar %d start %d", i, n.Start, i+1, song.Grid[i].Start)
		}
	}
}

func TestParseForwardRepeatWithTimes(t *testing.T) {
	// Measure 1 is outside the repeated section; measures 2 and 3 are played
	// three times, so the whole thing is 1 + 3*2 = 7 bars.
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
      <note><pitch><step>C</step><octave>4</octave></pitch><duration>4</duration></note>
    </measure>
    <measure number="2">
      <barline location="left">
        <bar-style>heavy-light</bar-style>
        <repeat direction="forward"/>
      </barline>
      <note><pitch><step>D</step><octave>4</octave></pitch><duration>4</duration></note>
    </measure>
    <measure number="3">
      <note><pitch><step>E</step><octave>4</octave></pitch><duration>4</duration></note>
      <barline location="right">
        <repeat direction="backward" times="3"/>
      </barline>
    </measure>
  </part>
</score-partwise>`

	song, err := Parse([]byte(doc), testClock())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(song.Grid) != 7 {
		t.Fatalf("len(Grid) = %d, want 7", len(song.Grid))
	}
	want := []uint8{60, 62, 64, 62, 64, 62, 64}
	if got := midis(song.Tracks[0].Notes); !equalMIDIs(got, want) {
		t.Fatalf("MIDIs = %v, want %v", got, want)
	}
}

func TestParseAlternateEndingsSpanMultipleMeasures(t *testing.T) {
	// A two-measure first ending. MusicXML writes the ending number only on
	// the bracket's start and stop barlines, so measure 3 — the middle of the
	// first ending — carries nothing at all and has to inherit the bracket.
	//
	// Played: 1 2 3 4 | 1 5. The second pass skips the whole first ending.
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
      <barline location="left"><repeat direction="forward"/></barline>
      <note><pitch><step>C</step><octave>4</octave></pitch><duration>4</duration></note>
    </measure>
    <measure number="2">
      <barline location="left"><ending number="1" type="start"/></barline>
      <note><pitch><step>D</step><octave>4</octave></pitch><duration>4</duration></note>
    </measure>
    <measure number="3">
      <note><pitch><step>E</step><octave>4</octave></pitch><duration>4</duration></note>
    </measure>
    <measure number="4">
      <note><pitch><step>F</step><octave>4</octave></pitch><duration>4</duration></note>
      <barline location="right">
        <ending number="1" type="stop"/>
        <repeat direction="backward"/>
      </barline>
    </measure>
    <measure number="5">
      <barline location="left"><ending number="2" type="start"/></barline>
      <note><pitch><step>G</step><octave>4</octave></pitch><duration>4</duration></note>
      <barline location="right"><ending number="2" type="discontinue"/></barline>
    </measure>
  </part>
</score-partwise>`

	song, err := Parse([]byte(doc), testClock())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(song.Grid) != 6 {
		t.Fatalf("len(Grid) = %d, want 6", len(song.Grid))
	}
	want := []uint8{60, 62, 64, 65, 60, 67}
	if got := midis(song.Tracks[0].Notes); !equalMIDIs(got, want) {
		t.Fatalf("MIDIs = %v, want %v", got, want)
	}
}

func TestParseRepeatRestatesItsOwnTempo(t *testing.T) {
	// Tempo follows the played order, not the written one: measure 1 carries
	// its own 120 and restates it every pass, so the repeat does not inherit
	// measure 2's 90.
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
      <note><pitch><step>C</step><octave>4</octave></pitch><duration>4</duration></note>
    </measure>
    <measure number="2">
      <direction><sound tempo="90"/></direction>
      <note><pitch><step>D</step><octave>4</octave></pitch><duration>4</duration></note>
      <barline location="right"><repeat direction="backward"/></barline>
    </measure>
  </part>
</score-partwise>`

	clock := testClock()
	song, err := Parse([]byte(doc), clock)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(song.Grid) != 4 {
		t.Fatalf("len(Grid) = %d, want 4", len(song.Grid))
	}
	wantBPM := []float64{120, 90, 120, 90}
	for i, want := range wantBPM {
		if song.Grid[i].BPM != want {
			t.Errorf("Grid[%d].BPM = %v, want %v", i, song.Grid[i].BPM, want)
		}
	}

	notes := song.Tracks[0].Notes
	if len(notes) != 4 {
		t.Fatalf("len(Notes) = %d, want 4", len(notes))
	}
	// Bar 3 is measure 1 again, and it must be a 120 BPM bar's worth of note,
	// not a 90 BPM one.
	if got, want := notes[2].Duration, clock.Frames(4.0*60.0/120.0); got != want {
		t.Errorf("second-pass measure 1 Duration = %d, want %d", got, want)
	}
}

func TestParseRepeatKeepsWrittenNoteLengthsWhenDivisionsChange(t *testing.T) {
	// Measure 2 changes the tick resolution. Carrying divisions through the
	// PLAYED order would re-scale measure 1's notes on the second pass, so the
	// same written whole note would come out half as long the second time.
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
      <note><pitch><step>C</step><octave>4</octave></pitch><duration>4</duration></note>
    </measure>
    <measure number="2">
      <attributes><divisions>2</divisions></attributes>
      <note><pitch><step>D</step><octave>4</octave></pitch><duration>8</duration></note>
      <barline location="right"><repeat direction="backward"/></barline>
    </measure>
  </part>
</score-partwise>`

	clock := testClock()
	song, err := Parse([]byte(doc), clock)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	notes := song.Tracks[0].Notes
	if len(notes) != 4 {
		t.Fatalf("len(Notes) = %d, want 4", len(notes))
	}
	whole := clock.Frames(4.0 * 60.0 / 120.0)
	for i, n := range notes {
		if n.Duration != whole {
			t.Errorf("note %d Duration = %d, want %d (a whole bar on every pass)", i, n.Duration, whole)
		}
	}
}

func TestParseTieIntoRepeatBoundaryFlushesTheFirstPass(t *testing.T) {
	// The tie starts in the last measure of the repeated section and stops
	// after it. The first pass never reaches the stop — the music jumps back
	// instead — so that note sounds its written length, and only the second
	// pass is actually tied through.
	//
	// Played: 1 2 | 1 2 3. Notes: C4, D4(1 bar), C4, D4(2 bars tied into 3).
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
      <barline location="left"><repeat direction="forward"/></barline>
      <note>
        <pitch><step>C</step><octave>4</octave></pitch>
        <duration>4</duration>
        <voice>1</voice>
      </note>
    </measure>
    <measure number="2">
      <note>
        <pitch><step>D</step><octave>4</octave></pitch>
        <duration>4</duration>
        <voice>1</voice>
        <tie type="start"/>
      </note>
      <barline location="right"><repeat direction="backward"/></barline>
    </measure>
    <measure number="3">
      <note>
        <pitch><step>D</step><octave>4</octave></pitch>
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
	if len(song.Grid) != 5 {
		t.Fatalf("len(Grid) = %d, want 5", len(song.Grid))
	}

	notes := song.Tracks[0].Notes
	bar := clock.Frames(4.0 * 60.0 / 120.0)
	want := []practice.Note{
		{Start: song.Grid[0].Start, Duration: bar, MIDI: 60, String: 2, Fret: 1},
		{Start: song.Grid[1].Start, Duration: bar, MIDI: 62, String: 2, Fret: 3},
		{Start: song.Grid[2].Start, Duration: bar, MIDI: 60, String: 2, Fret: 1},
		{Start: song.Grid[3].Start, Duration: 2 * bar, MIDI: 62, String: 2, Fret: 3},
	}
	if len(notes) != len(want) {
		t.Fatalf("len(Notes) = %d, want %d: %+v", len(notes), len(want), notes)
	}
	for i := range want {
		if notes[i] != want[i] {
			t.Errorf("note %d = %+v, want %+v", i, notes[i], want[i])
		}
	}
}

func TestParseRunawayRepeatIsRefused(t *testing.T) {
	// A times attribute no real score would carry. Unrolling it would allocate
	// a million bars, so the walk gives up and says so rather than trying.
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
      <note><pitch><step>C</step><octave>4</octave></pitch><duration>4</duration></note>
      <barline location="right"><repeat direction="backward" times="999999"/></barline>
    </measure>
  </part>
</score-partwise>`

	_, err := Parse([]byte(doc), testClock())
	if err == nil {
		t.Fatal("Parse: want an error for a runaway repeat, got nil")
	}
	if !strings.Contains(err.Error(), "repeat structure") {
		t.Errorf("error = %q, want it to name the repeat structure", err.Error())
	}
}

func TestParseWithoutBarlinesIsUnchanged(t *testing.T) {
	// The expansion has to be invisible to a score with no repeat signs: the
	// played order is just the written order.
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
      <note><pitch><step>C</step><octave>4</octave></pitch><duration>4</duration></note>
      <barline location="right"><bar-style>light-light</bar-style></barline>
    </measure>
    <measure number="2">
      <note><pitch><step>D</step><octave>4</octave></pitch><duration>4</duration></note>
      <barline location="right"><bar-style>light-heavy</bar-style></barline>
    </measure>
  </part>
</score-partwise>`

	song, err := Parse([]byte(doc), testClock())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(song.Grid) != 2 {
		t.Fatalf("len(Grid) = %d, want 2", len(song.Grid))
	}
	if got := midis(song.Tracks[0].Notes); !equalMIDIs(got, []uint8{60, 62}) {
		t.Fatalf("MIDIs = %v, want [60 62]", got)
	}
}

func TestEndingMaskReadsTheNumberList(t *testing.T) {
	cases := []struct {
		in   string
		want uint32
	}{
		{"", 0},
		{"1", 1 << 0},
		{"2", 1 << 1},
		{"1, 2", 1<<0 | 1<<1},
		{"1 2", 1<<0 | 1<<1},
		{"1.", 1 << 0},
		{"first", 0}, // nothing usable: the measure stays unconditional
	}
	for _, c := range cases {
		if got := endingMask(c.in); got != c.want {
			t.Errorf("endingMask(%q) = %b, want %b", c.in, got, c.want)
		}
	}
}
