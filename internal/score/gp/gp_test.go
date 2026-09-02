package gp

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FullFran/riffhero/internal/practice"
)

// Every test runs at 48 kHz, where a quarter note at 120 BPM is exactly 24000
// frames. The numbers below are written out rather than recomputed from the
// same formula the importer uses, so a sign error in that formula cannot make
// the test agree with itself.
var testClock = practice.Clock{SampleRate: 48000}

const (
	quarterAt120 = practice.Frame(24000)
	barAt120     = practice.Frame(96000) // 4/4
)

const standardPitches = "40 45 50 55 59 64" // low E first, as GPIF writes it

// --- GPIF document builder -------------------------------------------------
//
// The fixtures are written as XML rather than as parsed structs on purpose:
// the thing under test is the reading of a format, so the tests have to state
// the format.

type doc struct {
	Title       string
	Artist      string
	Automations string
	Tracks      string
	MasterBars  string
	Bars        string
	Voices      string
	Beats       string
	Notes       string
	Rhythms     string
}

func (d doc) xml() []byte {
	return []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<GPIF>
  <Score><Title>%s</Title><Artist>%s</Artist></Score>
  <MasterTrack><Tracks>0</Tracks><Automations>%s</Automations></MasterTrack>
  <Tracks>%s</Tracks>
  <MasterBars>%s</MasterBars>
  <Bars>%s</Bars>
  <Voices>%s</Voices>
  <Beats>%s</Beats>
  <Notes>%s</Notes>
  <Rhythms>%s</Rhythms>
</GPIF>`,
		d.Title, d.Artist, d.Automations, d.Tracks,
		d.MasterBars, d.Bars, d.Voices, d.Beats, d.Notes, d.Rhythms))
}

func trackXML(id int, name, pitches string, capo int) string {
	return fmt.Sprintf(`<Track id="%d"><Name>%s</Name><ShortName>gtr</ShortName>`+
		`<Sounds><Sound><Name>Clean Guitar</Name><Label>Clean</Label></Sound></Sounds>`+
		`<Staves><Staff><Properties>`+
		`<Property name="Tuning"><Pitches>%s</Pitches></Property>`+
		`<Property name="CapoFret"><Fret>%d</Fret></Property>`+
		`</Properties></Staff></Staves></Track>`, id, name, pitches, capo)
}

func guitarTrack() string { return trackXML(0, "Electric Guitar", standardPitches, 0) }

func tempoXML(bar int, position, bpm float64) string {
	return fmt.Sprintf(`<Automation><Type>Tempo</Type><Linear>false</Linear>`+
		`<Bar>%d</Bar><Position>%g</Position><Value>%g 2</Value></Automation>`, bar, position, bpm)
}

func masterBarXML(time, bars, extra string) string {
	return fmt.Sprintf(`<MasterBar><Time>%s</Time><Bars>%s</Bars>%s</MasterBar>`, time, bars, extra)
}

func barXML(id int, voices string) string {
	return fmt.Sprintf(`<Bar id="%d"><Clef>G2</Clef><Voices>%s</Voices></Bar>`, id, voices)
}

func voiceXML(id int, beats string) string {
	return fmt.Sprintf(`<Voice id="%d"><Beats>%s</Beats></Voice>`, id, beats)
}

// beatXML with an empty note list is a rest: it owns time and sounds nothing.
func beatXML(id, rhythmRef int, notes string) string {
	if notes == "" {
		return fmt.Sprintf(`<Beat id="%d"><Rhythm ref="%d"/></Beat>`, id, rhythmRef)
	}
	return fmt.Sprintf(`<Beat id="%d"><Rhythm ref="%d"/><Notes>%s</Notes></Beat>`, id, rhythmRef, notes)
}

func noteXML(id, gpifString, fret int, extra string) string {
	return fmt.Sprintf(`<Note id="%d"><Properties>`+
		`<Property name="String"><String>%d</String></Property>`+
		`<Property name="Fret"><Fret>%d</Fret></Property>`+
		`</Properties>%s</Note>`, id, gpifString, fret, extra)
}

func rhythmXML(id int, value, extra string) string {
	return fmt.Sprintf(`<Rhythm id="%d"><NoteValue>%s</NoteValue>%s</Rhythm>`, id, value, extra)
}

// --- assertions ------------------------------------------------------------

type wantNote struct {
	start    practice.Frame
	duration practice.Frame
	midi     uint8
	str      uint8
	fret     uint8
}

func assertNotes(t *testing.T, got []practice.Note, want []wantNote) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d notes, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		w, g := want[i], got[i]
		if g.Start != w.start || g.Duration != w.duration || g.MIDI != w.midi || g.String != w.str || g.Fret != w.fret {
			t.Errorf("note %d = {start %d dur %d midi %d string %d fret %d}, want {start %d dur %d midi %d string %d fret %d}",
				i, g.Start, g.Duration, g.MIDI, g.String, g.Fret,
				w.start, w.duration, w.midi, w.str, w.fret)
		}
	}
}

func mustParse(t *testing.T, d doc) *practice.Song {
	t.Helper()
	song, err := ParseGPIF(d.xml(), testClock)
	if err != nil {
		t.Fatalf("ParseGPIF: %v", err)
	}
	return song
}

func onlyTrack(t *testing.T, song *practice.Song) practice.Track {
	t.Helper()
	if len(song.Tracks) != 1 {
		t.Fatalf("got %d tracks, want 1", len(song.Tracks))
	}
	return song.Tracks[0]
}

// --- tests -----------------------------------------------------------------

func TestMinimalScoreLandsNotesOnExactFrames(t *testing.T) {
	song := mustParse(t, doc{
		Title:       "Warmup",
		Artist:      "Nobody",
		Automations: tempoXML(0, 0, 120),
		Tracks:      guitarTrack(),
		MasterBars:  masterBarXML("4/4", "0", ""),
		Bars:        barXML(0, "0 -1 -1 -1"),
		Voices:      voiceXML(0, "0 1 2 3"),
		Beats: beatXML(0, 0, "0") + beatXML(1, 0, "1") +
			beatXML(2, 0, "2") + beatXML(3, 0, "3"),
		Notes: noteXML(0, 5, 0, "") + noteXML(1, 5, 1, "") +
			noteXML(2, 5, 2, "") + noteXML(3, 5, 3, ""),
		Rhythms: rhythmXML(0, "Quarter", ""),
	})

	if song.Title != "Warmup" || song.Artist != "Nobody" {
		t.Errorf("metadata = %q / %q", song.Title, song.Artist)
	}
	if len(song.Grid) != 1 {
		t.Fatalf("grid has %d bars, want 1", len(song.Grid))
	}
	if song.Grid[0].Start != 0 || song.Grid[0].End != barAt120 {
		t.Errorf("bar 1 spans [%d,%d), want [0,%d)", song.Grid[0].Start, song.Grid[0].End, barAt120)
	}

	assertNotes(t, onlyTrack(t, song).Notes, []wantNote{
		{start: 0 * quarterAt120, duration: quarterAt120, midi: 64, str: 1, fret: 0},
		{start: 1 * quarterAt120, duration: quarterAt120, midi: 65, str: 1, fret: 1},
		{start: 2 * quarterAt120, duration: quarterAt120, midi: 66, str: 1, fret: 2},
		{start: 3 * quarterAt120, duration: quarterAt120, midi: 67, str: 1, fret: 3},
	})
}

// The whole importer turns on this inversion: GPIF counts strings up from the
// lowest, RiffHero counts them down from the highest.
func TestStringNumberingIsInvertedFromGPIF(t *testing.T) {
	song := mustParse(t, doc{
		Automations: tempoXML(0, 0, 120),
		Tracks:      guitarTrack(),
		MasterBars:  masterBarXML("4/4", "0", ""),
		Bars:        barXML(0, "0 -1 -1 -1"),
		Voices:      voiceXML(0, "0"),
		Beats:       beatXML(0, 0, "0 1 2"),
		Notes:       noteXML(0, 0, 0, "") + noteXML(1, 3, 0, "") + noteXML(2, 5, 0, ""),
		Rhythms:     rhythmXML(0, "Whole", ""),
	})

	// Sorted by pitch, so the low E written as GPIF string 0 must come out
	// first, as RiffHero string 6 sounding E2.
	assertNotes(t, onlyTrack(t, song).Notes, []wantNote{
		{start: 0, duration: barAt120, midi: 40, str: 6, fret: 0},
		{start: 0, duration: barAt120, midi: 55, str: 3, fret: 0},
		{start: 0, duration: barAt120, midi: 64, str: 1, fret: 0},
	})
}

func TestTuningAndCapoAreRecoveredFromPitches(t *testing.T) {
	t.Run("drop D", func(t *testing.T) {
		song := mustParse(t, doc{
			Automations: tempoXML(0, 0, 120),
			Tracks:      trackXML(0, "Guitar", "38 45 50 55 59 64", 0),
			MasterBars:  masterBarXML("4/4", "0", ""),
			Bars:        barXML(0, "0 -1 -1 -1"),
			Voices:      voiceXML(0, "0"),
			Beats:       beatXML(0, 0, "0"),
			Notes:       noteXML(0, 0, 0, ""),
			Rhythms:     rhythmXML(0, "Whole", ""),
		})
		tr := onlyTrack(t, song)

		// Strings is indexed by string number minus one and string 1 is the
		// highest, so it is <Pitches> reversed.
		if want := [6]uint8{64, 59, 55, 50, 45, 38}; tr.Tuning.Strings != want {
			t.Errorf("tuning = %v, want %v", tr.Tuning.Strings, want)
		}
		if tr.Tuning.Name != "Drop D" {
			t.Errorf("tuning name = %q, want %q", tr.Tuning.Name, "Drop D")
		}
		assertNotes(t, tr.Notes, []wantNote{
			{start: 0, duration: barAt120, midi: 38, str: 6, fret: 0},
		})
	})

	t.Run("capo raises every string", func(t *testing.T) {
		song := mustParse(t, doc{
			Automations: tempoXML(0, 0, 120),
			Tracks:      trackXML(0, "Guitar", standardPitches, 2),
			MasterBars:  masterBarXML("4/4", "0", ""),
			Bars:        barXML(0, "0 -1 -1 -1"),
			Voices:      voiceXML(0, "0"),
			Beats:       beatXML(0, 0, "0 1"),
			Notes:       noteXML(0, 0, 0, "") + noteXML(1, 5, 3, ""),
			Rhythms:     rhythmXML(0, "Whole", ""),
		})
		tr := onlyTrack(t, song)
		if tr.Tuning.Capo != 2 {
			t.Errorf("capo = %d, want 2", tr.Tuning.Capo)
		}
		// Fret numbers stay as written; the capo is added on top of them.
		assertNotes(t, tr.Notes, []wantNote{
			{start: 0, duration: barAt120, midi: 42, str: 6, fret: 0},
			{start: 0, duration: barAt120, midi: 69, str: 1, fret: 3},
		})
	})

	t.Run("unnamed tuning is spelled out low to high", func(t *testing.T) {
		song := mustParse(t, doc{
			Automations: tempoXML(0, 0, 120),
			Tracks:      trackXML(0, "Guitar", "38 45 50 55 57 62", 0),
			MasterBars:  masterBarXML("4/4", "0", ""),
			Bars:        barXML(0, "0 -1 -1 -1"),
			Voices:      voiceXML(0, "0"),
			Beats:       beatXML(0, 0, "0"),
			Notes:       noteXML(0, 0, 0, ""),
			Rhythms:     rhythmXML(0, "Whole", ""),
		})
		if got, want := onlyTrack(t, song).Tuning.Name, "D A D G A D"; got != want {
			t.Errorf("tuning name = %q, want %q", got, want)
		}
	})
}

// The inversion is derived from the instrument's own string count, not from a
// constant 6. On a four-string bass a hard-coded 6 would send GPIF string 0 to
// string 6, which does not exist on the instrument.
func TestFourStringBassMapsFromItsOwnStringCount(t *testing.T) {
	song := mustParse(t, doc{
		Automations: tempoXML(0, 0, 120),
		Tracks:      trackXML(0, "Bass", "28 33 38 43", 0),
		MasterBars:  masterBarXML("4/4", "0", ""),
		Bars:        barXML(0, "0 -1 -1 -1"),
		Voices:      voiceXML(0, "0"),
		Beats:       beatXML(0, 0, "0 1"),
		Notes:       noteXML(0, 0, 0, "") + noteXML(1, 3, 2, ""),
		Rhythms:     rhythmXML(0, "Whole", ""),
	})

	tr := onlyTrack(t, song)
	if want := [6]uint8{43, 38, 33, 28, 0, 0}; tr.Tuning.Strings != want {
		t.Errorf("tuning = %v, want %v", tr.Tuning.Strings, want)
	}
	if tr.Tuning.Name != "E A D G" {
		t.Errorf("tuning name = %q, want %q", tr.Tuning.Name, "E A D G")
	}
	assertNotes(t, tr.Notes, []wantNote{
		{start: 0, duration: barAt120, midi: 28, str: 4, fret: 0},
		{start: 0, duration: barAt120, midi: 45, str: 1, fret: 2},
	})
}

func TestRhythmDurations(t *testing.T) {
	tests := []struct {
		name  string
		xml   string
		want  float64
		valid bool
	}{
		{name: "whole", xml: rhythmXML(0, "Whole", ""), want: 4, valid: true},
		{name: "half", xml: rhythmXML(0, "Half", ""), want: 2, valid: true},
		{name: "quarter", xml: rhythmXML(0, "Quarter", ""), want: 1, valid: true},
		{name: "eighth", xml: rhythmXML(0, "Eighth", ""), want: 0.5, valid: true},
		{name: "sixteenth", xml: rhythmXML(0, "16th", ""), want: 0.25, valid: true},
		{name: "256th", xml: rhythmXML(0, "256th", ""), want: 0.015625, valid: true},
		{name: "dotted quarter", xml: rhythmXML(0, "Quarter", `<AugmentationDot count="1"/>`), want: 1.5, valid: true},
		{name: "double dotted quarter", xml: rhythmXML(0, "Quarter", `<AugmentationDot count="2"/>`), want: 1.75, valid: true},
		{name: "triplet eighth", xml: rhythmXML(0, "Eighth", `<PrimaryTuplet num="3" den="2"/>`), want: 0.5 * 2 / 3, valid: true},
		{name: "dotted triplet", xml: rhythmXML(0, "Quarter", `<AugmentationDot count="1"/><PrimaryTuplet num="3" den="2"/>`), want: 1, valid: true},
		{name: "unknown value", xml: rhythmXML(0, "Sesquialtera", ""), want: 0, valid: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := doc{
				Automations: tempoXML(0, 0, 120),
				Tracks:      guitarTrack(),
				MasterBars:  masterBarXML("4/4", "0", ""),
				Bars:        barXML(0, "0 -1 -1 -1"),
				Voices:      voiceXML(0, "0"),
				Beats:       beatXML(0, 0, "0"),
				Notes:       noteXML(0, 5, 0, ""),
				Rhythms:     tc.xml,
			}
			var parsed gpif
			if err := unmarshalDoc(d, &parsed); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			got, ok := parsed.Rhythms[0].quarters()
			if ok != tc.valid {
				t.Fatalf("valid = %v, want %v", ok, tc.valid)
			}
			if ok && got != tc.want {
				t.Errorf("quarters = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDottedTripletAndRestBeatsConsumeTime(t *testing.T) {
	// The triplet is deliberately first. A third of a beat is not representable
	// in binary, so a cursor that has crossed one lands a hair below every
	// exact frame after it; truncating instead of rounding would put this whole
	// bar one frame early from its second note onwards.
	song := mustParse(t, doc{
		Automations: tempoXML(0, 0, 120),
		Tracks:      guitarTrack(),
		MasterBars:  masterBarXML("4/4", "0", ""),
		Bars:        barXML(0, "0 -1 -1 -1"),
		Voices:      voiceXML(0, "0 1 2 3 4 5"),
		Beats: beatXML(0, 3, "0") + beatXML(1, 3, "1") + beatXML(2, 3, "2") + // eighth triplet
			beatXML(3, 1, "3") + // dotted quarter
			beatXML(4, 2, "4") + // eighth
			beatXML(5, 0, ""), // quarter rest, sounds nothing and fills the bar
		Notes: noteXML(0, 5, 0, "") + noteXML(1, 5, 1, "") +
			noteXML(2, 5, 2, "") + noteXML(3, 5, 3, "") + noteXML(4, 5, 4, ""),
		Rhythms: rhythmXML(0, "Quarter", "") +
			rhythmXML(1, "Quarter", `<AugmentationDot count="1"/>`) +
			rhythmXML(2, "Eighth", "") +
			rhythmXML(3, "Eighth", `<PrimaryTuplet num="3" den="2"/>`),
	})

	const third = practice.Frame(8000) // an eighth-note triplet at 120 BPM
	assertNotes(t, onlyTrack(t, song).Notes, []wantNote{
		{start: 0, duration: third, midi: 64, str: 1, fret: 0},
		{start: third, duration: third, midi: 65, str: 1, fret: 1},
		{start: 2 * third, duration: third, midi: 66, str: 1, fret: 2},
		// Three triplet eighths are worth exactly one quarter note, so the
		// dotted quarter starts on beat 2 and lasts a beat and a half.
		{start: quarterAt120, duration: 36000, midi: 67, str: 1, fret: 3},
		{start: quarterAt120 + 36000, duration: 12000, midi: 68, str: 1, fret: 4},
	})

	// The trailing rest is not a note, but it is time: the bar it fills has to
	// still be four beats long.
	if got := song.Grid[0].End - song.Grid[0].Start; got != barAt120 {
		t.Errorf("bar length = %d frames, want %d", got, barAt120)
	}
}

// Most tempos put every note on a whole frame, which hides the difference
// between rounding and truncating. 130 BPM does not: a triplet eighth lands on
// 7384.6 frames, and truncating would place it, and everything else in the
// bar, early.
func TestSubFrameOffsetsRoundToTheNearestFrame(t *testing.T) {
	song := mustParse(t, doc{
		Automations: tempoXML(0, 0, 130),
		Tracks:      guitarTrack(),
		MasterBars:  masterBarXML("4/4", "0", ""),
		Bars:        barXML(0, "0 -1 -1 -1"),
		Voices:      voiceXML(0, "0 1 2 3"),
		Beats: beatXML(0, 0, "0") + beatXML(1, 0, "1") + beatXML(2, 0, "2") +
			beatXML(3, 1, ""), // dotted half rest, filling out the bar
		Notes: noteXML(0, 5, 0, "") + noteXML(1, 5, 1, "") + noteXML(2, 5, 2, ""),
		Rhythms: rhythmXML(0, "Eighth", `<PrimaryTuplet num="3" den="2"/>`) +
			rhythmXML(1, "Half", `<AugmentationDot count="1"/>`),
	})

	assertNotes(t, onlyTrack(t, song).Notes, []wantNote{
		{start: 0, duration: 7385, midi: 64, str: 1, fret: 0},
		{start: 7385, duration: 7384, midi: 65, str: 1, fret: 1},
		{start: 14769, duration: 7385, midi: 66, str: 1, fret: 2},
	})
}

func TestVoicesInABarStartTogether(t *testing.T) {
	song := mustParse(t, doc{
		Automations: tempoXML(0, 0, 120),
		Tracks:      guitarTrack(),
		MasterBars:  masterBarXML("4/4", "0", ""),
		Bars:        barXML(0, "0 1 -1 -1"),
		Voices:      voiceXML(0, "0") + voiceXML(1, "1 2"),
		Beats:       beatXML(0, 0, "0") + beatXML(1, 1, "1") + beatXML(2, 1, "2"),
		Notes:       noteXML(0, 5, 0, "") + noteXML(1, 0, 0, "") + noteXML(2, 0, 2, ""),
		Rhythms:     rhythmXML(0, "Whole", "") + rhythmXML(1, "Half", ""),
	})

	// Voice 2 is a second layer over the same bar, not a continuation of voice
	// 1: its first note starts at the barline, not after voice 1's whole note.
	assertNotes(t, onlyTrack(t, song).Notes, []wantNote{
		{start: 0, duration: barAt120 / 2, midi: 40, str: 6, fret: 0},
		{start: 0, duration: barAt120, midi: 64, str: 1, fret: 0},
		{start: barAt120 / 2, duration: barAt120 / 2, midi: 42, str: 6, fret: 2},
	})
}

func TestTieExtendsNoteAcrossBarline(t *testing.T) {
	song := mustParse(t, doc{
		Automations: tempoXML(0, 0, 120),
		Tracks:      guitarTrack(),
		MasterBars:  masterBarXML("4/4", "0", "") + masterBarXML("4/4", "1", ""),
		Bars:        barXML(0, "0 -1 -1 -1") + barXML(1, "1 -1 -1 -1"),
		Voices:      voiceXML(0, "0 1") + voiceXML(1, "2 3"),
		Beats: beatXML(0, 0, "") + beatXML(1, 0, "0") + // half rest, then a half note
			beatXML(2, 0, "1") + beatXML(3, 0, ""), // the tied half, then a half rest
		Notes:   noteXML(0, 5, 7, `<Tie origin="true" destination="false"/>`) + noteXML(1, 5, 7, `<Tie origin="false" destination="true"/>`),
		Rhythms: rhythmXML(0, "Half", ""),
	})

	// One note, not two: the tie is a duration, not an attack.
	assertNotes(t, onlyTrack(t, song).Notes, []wantNote{
		{start: barAt120 / 2, duration: barAt120, midi: 71, str: 1, fret: 7},
	})
}

func TestRepeatCountExpandsEveryPass(t *testing.T) {
	song := mustParse(t, doc{
		Automations: tempoXML(0, 0, 120),
		Tracks:      guitarTrack(),
		MasterBars:  masterBarXML("4/4", "0", `<Repeat start="true" end="true" count="3"/>`),
		Bars:        barXML(0, "0 -1 -1 -1"),
		Voices:      voiceXML(0, "0"),
		Beats:       beatXML(0, 0, "0"),
		Notes:       noteXML(0, 5, 5, ""),
		Rhythms:     rhythmXML(0, "Whole", ""),
	})

	if len(song.Grid) != 3 {
		t.Fatalf("grid has %d bars, want 3 (the repeat is unrolled)", len(song.Grid))
	}
	assertNotes(t, onlyTrack(t, song).Notes, []wantNote{
		{start: 0 * barAt120, duration: barAt120, midi: 69, str: 1, fret: 5},
		{start: 1 * barAt120, duration: barAt120, midi: 69, str: 1, fret: 5},
		{start: 2 * barAt120, duration: barAt120, midi: 69, str: 1, fret: 5},
	})
}

func TestAlternateEndingsSelectBarsPerPass(t *testing.T) {
	song := mustParse(t, doc{
		Automations: tempoXML(0, 0, 120),
		Tracks:      guitarTrack(),
		MasterBars: masterBarXML("4/4", "0", `<Repeat start="true" end="false" count="0"/>`) +
			masterBarXML("4/4", "1", `<Repeat start="false" end="true" count="2"/><AlternateEndings>1</AlternateEndings>`) +
			masterBarXML("4/4", "2", `<AlternateEndings>2</AlternateEndings>`),
		Bars:    barXML(0, "0 -1 -1 -1") + barXML(1, "1 -1 -1 -1") + barXML(2, "2 -1 -1 -1"),
		Voices:  voiceXML(0, "0") + voiceXML(1, "1") + voiceXML(2, "2"),
		Beats:   beatXML(0, 0, "0") + beatXML(1, 0, "1") + beatXML(2, 0, "2"),
		Notes:   noteXML(0, 5, 0, "") + noteXML(1, 5, 1, "") + noteXML(2, 5, 2, ""),
		Rhythms: rhythmXML(0, "Whole", ""),
	})

	// Played bars: body, first ending, body again, second ending.
	if len(song.Grid) != 4 {
		t.Fatalf("grid has %d bars, want 4", len(song.Grid))
	}
	assertNotes(t, onlyTrack(t, song).Notes, []wantNote{
		{start: 0 * barAt120, duration: barAt120, midi: 64, str: 1, fret: 0},
		{start: 1 * barAt120, duration: barAt120, midi: 65, str: 1, fret: 1},
		{start: 2 * barAt120, duration: barAt120, midi: 64, str: 1, fret: 0},
		{start: 3 * barAt120, duration: barAt120, midi: 66, str: 1, fret: 2},
	})
}

func TestTempoChangeMovesLaterBars(t *testing.T) {
	song := mustParse(t, doc{
		Automations: tempoXML(0, 0, 120) + tempoXML(1, 0, 60),
		Tracks:      guitarTrack(),
		MasterBars:  masterBarXML("4/4", "0", "") + masterBarXML("4/4", "1", ""),
		Bars:        barXML(0, "0 -1 -1 -1") + barXML(1, "1 -1 -1 -1"),
		Voices:      voiceXML(0, "0 1") + voiceXML(1, "2 3"),
		Beats:       beatXML(0, 0, "0") + beatXML(1, 0, "1") + beatXML(2, 0, "2") + beatXML(3, 0, "3"),
		Notes: noteXML(0, 5, 0, "") + noteXML(1, 5, 1, "") +
			noteXML(2, 5, 2, "") + noteXML(3, 5, 3, ""),
		Rhythms: rhythmXML(0, "Half", ""),
	})

	if got, want := song.Grid[1].BPM, 60.0; got != want {
		t.Errorf("bar 2 BPM = %v, want %v", got, want)
	}
	// At 60 BPM a half note lasts two seconds, twice what it did in bar 1.
	assertNotes(t, onlyTrack(t, song).Notes, []wantNote{
		{start: 0, duration: 2 * quarterAt120, midi: 64, str: 1, fret: 0},
		{start: 48000, duration: 2 * quarterAt120, midi: 65, str: 1, fret: 1},
		{start: 96000, duration: 96000, midi: 66, str: 1, fret: 2},
		{start: 192000, duration: 96000, midi: 67, str: 1, fret: 3},
	})
}

func TestAlternateEndingSharedByTwoPasses(t *testing.T) {
	// "1 2" is a list of ending numbers, not the number 12 and not a raw
	// bitmask: the bar belongs to both the first and the second ending.
	song := mustParse(t, doc{
		Automations: tempoXML(0, 0, 120),
		Tracks:      guitarTrack(),
		MasterBars: masterBarXML("4/4", "0", `<Repeat start="true" end="false" count="0"/>`) +
			masterBarXML("4/4", "1", `<Repeat start="false" end="true" count="3"/><AlternateEndings>1 2</AlternateEndings>`) +
			masterBarXML("4/4", "2", `<AlternateEndings>3</AlternateEndings>`),
		Bars:    barXML(0, "0 -1 -1 -1") + barXML(1, "1 -1 -1 -1") + barXML(2, "2 -1 -1 -1"),
		Voices:  voiceXML(0, "0") + voiceXML(1, "1") + voiceXML(2, "2"),
		Beats:   beatXML(0, 0, "0") + beatXML(1, 0, "1") + beatXML(2, 0, "2"),
		Notes:   noteXML(0, 5, 0, "") + noteXML(1, 5, 1, "") + noteXML(2, 5, 2, ""),
		Rhythms: rhythmXML(0, "Whole", ""),
	})

	var frets []uint8
	for _, n := range onlyTrack(t, song).Notes {
		frets = append(frets, n.Fret)
	}
	// Body, shared ending, body, shared ending, body, third ending.
	if want := []uint8{0, 1, 0, 1, 0, 2}; fmt.Sprint(frets) != fmt.Sprint(want) {
		t.Errorf("played bars = %v, want %v", frets, want)
	}
}

func TestMidBarTempoAutomationAppliesAtTheBarStart(t *testing.T) {
	song := mustParse(t, doc{
		// Bar 1 speeds up halfway through, and bar 2 slows down halfway
		// through. Both are applied at their barline, and the second
		// automation inside bar 1 does not override the one it entered on.
		Automations: tempoXML(0, 0, 120) + tempoXML(0, 2, 200) + tempoXML(1, 2, 60),
		Tracks:      guitarTrack(),
		MasterBars:  masterBarXML("4/4", "0", "") + masterBarXML("4/4", "1", ""),
		Bars:        barXML(0, "0 -1 -1 -1") + barXML(1, "1 -1 -1 -1"),
		Voices:      voiceXML(0, "0") + voiceXML(1, "1"),
		Beats:       beatXML(0, 0, "0") + beatXML(1, 0, "1"),
		Notes:       noteXML(0, 5, 0, "") + noteXML(1, 5, 1, ""),
		Rhythms:     rhythmXML(0, "Whole", ""),
	})

	if got := song.Grid[0].BPM; got != 120 {
		t.Errorf("bar 1 BPM = %v, want 120: the earliest automation in a bar wins", got)
	}
	if got := song.Grid[1].BPM; got != 60 {
		t.Errorf("bar 2 BPM = %v, want 60 from its barline", got)
	}
	assertNotes(t, onlyTrack(t, song).Notes, []wantNote{
		{start: 0, duration: barAt120, midi: 64, str: 1, fret: 0},
		{start: barAt120, duration: 2 * barAt120, midi: 65, str: 1, fret: 1},
	})
}

func TestInstrumentFallsBackThroughTheTrackLabels(t *testing.T) {
	labelled := func(labels string) string {
		return `<Track id="0">` + labels +
			`<Staves><Staff><Properties><Property name="Tuning"><Pitches>` +
			standardPitches + `</Pitches></Property></Properties></Staff></Staves></Track>`
	}

	tests := []struct {
		name   string
		labels string
		want   string
	}{
		{
			name:   "sound name wins",
			labels: `<Name>Rhythm</Name><ShortName>rhy</ShortName><Sounds><Sound><Name>Overdrive</Name></Sound></Sounds><InstrumentSet><Name>Guitar</Name></InstrumentSet>`,
			want:   "Overdrive",
		},
		{
			name:   "instrument set name",
			labels: `<Name>Rhythm</Name><ShortName>rhy</ShortName><InstrumentSet><Name>Guitar</Name><Type>guitar</Type></InstrumentSet>`,
			want:   "Guitar",
		},
		{
			name:   "instrument set type",
			labels: `<Name>Rhythm</Name><ShortName>rhy</ShortName><InstrumentSet><Type>guitar</Type></InstrumentSet>`,
			want:   "guitar",
		},
		{name: "short name", labels: `<Name>Rhythm</Name><ShortName>rhy</ShortName>`, want: "rhy"},
		{name: "track name", labels: `<Name>Rhythm</Name>`, want: "Rhythm"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			song := mustParse(t, doc{
				Automations: tempoXML(0, 0, 120),
				Tracks:      labelled(tc.labels),
				MasterBars:  masterBarXML("4/4", "0", ""),
				Bars:        barXML(0, "0 -1 -1 -1"),
				Voices:      voiceXML(0, "0"),
				Beats:       beatXML(0, 0, "0"),
				Notes:       noteXML(0, 5, 0, ""),
				Rhythms:     rhythmXML(0, "Whole", ""),
			})
			tr := onlyTrack(t, song)
			if tr.Instrument != tc.want {
				t.Errorf("instrument = %q, want %q", tr.Instrument, tc.want)
			}
			// The tracks here carry no CapoFret property at all.
			if tr.Tuning.Capo != 0 {
				t.Errorf("capo = %d, want 0 when the property is absent", tr.Tuning.Capo)
			}
		})
	}
}

func TestScoreMetadataAndInstrument(t *testing.T) {
	song := mustParse(t, doc{
		Title:       "  Song  ",
		Automations: tempoXML(0, 0, 120),
		Tracks:      guitarTrack(),
		MasterBars:  masterBarXML("4/4", "0", ""),
		Bars:        barXML(0, "0 -1 -1 -1"),
		Voices:      voiceXML(0, "0"),
		Beats:       beatXML(0, 0, "0"),
		Notes:       noteXML(0, 5, 0, ""),
		Rhythms:     rhythmXML(0, "Whole", ""),
	})

	if song.Title != "Song" {
		t.Errorf("title = %q, want %q", song.Title, "Song")
	}
	if song.Source != sourceName {
		t.Errorf("source = %q, want %q", song.Source, sourceName)
	}
	tr := onlyTrack(t, song)
	if tr.Name != "Electric Guitar" || tr.Instrument != "Clean Guitar" {
		t.Errorf("track = %q / %q", tr.Name, tr.Instrument)
	}
	if song.GuitarTrack() != 0 {
		t.Errorf("GuitarTrack() = %d, want 0", song.GuitarTrack())
	}
}

func TestParseReadsGPArchive(t *testing.T) {
	payload := doc{
		Title:       "Zipped",
		Automations: tempoXML(0, 0, 120),
		Tracks:      guitarTrack(),
		MasterBars:  masterBarXML("4/4", "0", ""),
		Bars:        barXML(0, "0 -1 -1 -1"),
		Voices:      voiceXML(0, "0"),
		Beats:       beatXML(0, 0, "0"),
		Notes:       noteXML(0, 5, 0, ""),
		Rhythms:     rhythmXML(0, "Whole", ""),
	}.xml()

	for _, entry := range []string{"Content/score.gpif", "score.gpif"} {
		t.Run(entry, func(t *testing.T) {
			song, err := Parse(makeArchive(t, entry, payload), testClock)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if song.Title != "Zipped" {
				t.Errorf("title = %q", song.Title)
			}
			assertNotes(t, onlyTrack(t, song).Notes, []wantNote{
				{start: 0, duration: barAt120, midi: 64, str: 1, fret: 0},
			})
		})
	}

	t.Run("bare GPIF is still accepted", func(t *testing.T) {
		if _, err := Parse(payload, testClock); err != nil {
			t.Fatalf("Parse: %v", err)
		}
	})

	t.Run("archive without a score", func(t *testing.T) {
		_, err := Parse(makeArchive(t, "Content/notes.txt", []byte("hello")), testClock)
		if err == nil || !strings.Contains(err.Error(), "score.gpif") {
			t.Fatalf("err = %v, want it to name score.gpif", err)
		}
	})
}

func TestParseFileSetsSourceToPath(t *testing.T) {
	payload := doc{
		Automations: tempoXML(0, 0, 120),
		Tracks:      guitarTrack(),
		MasterBars:  masterBarXML("4/4", "0", ""),
		Bars:        barXML(0, "0 -1 -1 -1"),
		Voices:      voiceXML(0, "0"),
		Beats:       beatXML(0, 0, "0"),
		Notes:       noteXML(0, 5, 0, ""),
		Rhythms:     rhythmXML(0, "Whole", ""),
	}.xml()

	path := filepath.Join(t.TempDir(), "riff.gp")
	if err := os.WriteFile(path, makeArchive(t, "Content/score.gpif", payload), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	song, err := ParseFile(path, testClock)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if song.Source != path {
		t.Errorf("source = %q, want %q", song.Source, path)
	}

	t.Run("missing file", func(t *testing.T) {
		if _, err := ParseFile(filepath.Join(t.TempDir(), "absent.gp"), testClock); err == nil {
			t.Fatal("want an error for a missing file")
		}
	})
}

func TestGuitarPro6ArchiveIsRejected(t *testing.T) {
	for _, magic := range []string{"BCFS", "BCFZ"} {
		data := append([]byte(magic), bytes.Repeat([]byte{0x01}, 32)...)
		_, err := Parse(data, testClock)
		if !errors.Is(err, errUnsupportedFormat) {
			t.Fatalf("%s: err = %v, want errUnsupportedFormat", magic, err)
		}
		for _, want := range []string{".gpx", "MusicXML", ".gp"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: error %q does not mention %q", magic, err, want)
			}
		}
	}
}

func TestGuitarPro5FileIsRejected(t *testing.T) {
	version := "FICHIER GUITAR PRO v5.10"
	data := append([]byte{byte(len(version))}, version...)
	data = append(data, bytes.Repeat([]byte{0x00}, 16)...)

	_, err := Parse(data, testClock)
	if !errors.Is(err, errUnsupportedFormat) {
		t.Fatalf("err = %v, want errUnsupportedFormat", err)
	}
	for _, want := range []string{version, "MusicXML"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestMalformedInputIsRejected(t *testing.T) {
	good := doc{
		Automations: tempoXML(0, 0, 120),
		Tracks:      guitarTrack(),
		MasterBars:  masterBarXML("4/4", "0", ""),
		Bars:        barXML(0, "0 -1 -1 -1"),
		Voices:      voiceXML(0, "0"),
		Beats:       beatXML(0, 0, "0"),
		Notes:       noteXML(0, 5, 0, ""),
		Rhythms:     rhythmXML(0, "Whole", ""),
	}

	tests := []struct {
		name string
		data []byte
	}{
		{name: "empty", data: nil},
		{name: "unterminated xml", data: []byte(`<GPIF><Score><Title>x</Title>`)},
		{name: "wrong root", data: []byte(`<?xml version="1.0"?><MusicXML/>`)},
		{name: "not xml at all", data: []byte("\x00\x01\x02 this is a wav file")},
		{name: "no bars", data: doc{Tracks: guitarTrack()}.xml()},
		{name: "truncated archive", data: []byte("PK\x03\x04 and then nothing useful")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse(tc.data, testClock); err == nil {
				t.Fatal("want an error")
			}
		})
	}

	t.Run("non-positive sample rate", func(t *testing.T) {
		if _, err := ParseGPIF(good.xml(), practice.Clock{}); err == nil {
			t.Fatal("want an error for a zero sample rate")
		}
	})
}

func TestRunawayRepeatIsRejected(t *testing.T) {
	// A count this large is either a corrupt file or a hand-edit; unrolling it
	// would allocate a timeline nobody could practise.
	_, err := ParseGPIF(doc{
		Automations: tempoXML(0, 0, 120),
		Tracks:      guitarTrack(),
		MasterBars:  masterBarXML("4/4", "0", `<Repeat start="true" end="true" count="50000"/>`),
		Bars:        barXML(0, "0 -1 -1 -1"),
		Voices:      voiceXML(0, "0"),
		Beats:       beatXML(0, 0, "0"),
		Notes:       noteXML(0, 5, 0, ""),
		Rhythms:     rhythmXML(0, "Whole", ""),
	}.xml(), testClock)

	if err == nil {
		t.Fatal("want an error instead of an unbounded expansion")
	}
	if !strings.Contains(err.Error(), "repeat structure") {
		t.Errorf("error %q does not explain that the repeat structure is the problem", err)
	}
}

func TestDirectionJumpsArePlayedStraightThrough(t *testing.T) {
	song := mustParse(t, doc{
		Automations: tempoXML(0, 0, 120),
		Tracks:      guitarTrack(),
		MasterBars: masterBarXML("4/4", "0", `<DirectionTarget>DaCapo</DirectionTarget>`) +
			masterBarXML("4/4", "1", `<Directions><Jump>DaCapo</Jump></Directions>`),
		Bars:    barXML(0, "0 -1 -1 -1") + barXML(1, "1 -1 -1 -1"),
		Voices:  voiceXML(0, "0") + voiceXML(1, "1"),
		Beats:   beatXML(0, 0, "0") + beatXML(1, 0, "1"),
		Notes:   noteXML(0, 5, 0, "") + noteXML(1, 5, 1, ""),
		Rhythms: rhythmXML(0, "Whole", ""),
	})

	// Documented limitation: the jump is not followed, so the song is two bars
	// long rather than four.
	if len(song.Grid) != 2 {
		t.Fatalf("grid has %d bars, want 2 (direction jumps are not expanded)", len(song.Grid))
	}
	assertNotes(t, onlyTrack(t, song).Notes, []wantNote{
		{start: 0, duration: barAt120, midi: 64, str: 1, fret: 0},
		{start: barAt120, duration: barAt120, midi: 65, str: 1, fret: 1},
	})
}

func TestDanglingReferencesAreSurvived(t *testing.T) {
	song, err := ParseGPIF(doc{
		Automations: tempoXML(0, 0, 120) + `<Automation><Type>Tempo</Type><Bar>nope</Bar><Value>oops</Value></Automation>`,
		Tracks:      guitarTrack(),
		MasterBars: masterBarXML("4/4", "99", "") + // no such bar
			masterBarXML("", "0", "") + // no meter, inherits 4/4
			masterBarXML("4/4", "1", "") +
			masterBarXML("4/4", "2", "") +
			masterBarXML("4/4", "3", ""),
		Bars: barXML(0, "42 -1 -1 -1") + // no such voice
			barXML(1, "0 -1 -1 -1") +
			barXML(2, "1 -1 -1 -1") +
			barXML(3, "2 -1 -1 -1"),
		Voices: voiceXML(0, "7") + // no such beat
			voiceXML(1, "0") +
			voiceXML(2, "1"),
		Beats: beatXML(0, 9, "0") + // no such rhythm
			beatXML(1, 0, "77"), // no such note
		Notes:   noteXML(0, 5, 0, ""),
		Rhythms: rhythmXML(0, "Whole", ""),
	}.xml(), testClock)
	if err != nil {
		t.Fatalf("ParseGPIF: %v", err)
	}

	if len(song.Grid) != 5 {
		t.Errorf("grid has %d bars, want 5", len(song.Grid))
	}
	if len(song.Tracks) != 0 {
		t.Errorf("got %d tracks, want none: nothing in this file resolves to a note", len(song.Tracks))
	}
	if song.Grid[0].BPM != 120 {
		t.Errorf("BPM = %v, want the unparsable automation to be ignored", song.Grid[0].BPM)
	}
}

func TestSevenStringTrackIsSkipped(t *testing.T) {
	song := mustParse(t, doc{
		Automations: tempoXML(0, 0, 120),
		Tracks:      trackXML(0, "7-String", "35 "+standardPitches, 0) + trackXML(1, "Guitar", standardPitches, 0),
		MasterBars:  masterBarXML("4/4", "0 1", ""),
		Bars:        barXML(0, "0 -1 -1 -1") + barXML(1, "1 -1 -1 -1"),
		Voices:      voiceXML(0, "0") + voiceXML(1, "1"),
		Beats:       beatXML(0, 0, "0") + beatXML(1, 0, "1"),
		Notes:       noteXML(0, 0, 0, "") + noteXML(1, 0, 0, ""),
		Rhythms:     rhythmXML(0, "Whole", ""),
	})

	// The per-track bar list is positional, so the surviving track must still
	// read bar id 1 and not bar id 0.
	tr := onlyTrack(t, song)
	if tr.Name != "Guitar" {
		t.Errorf("track = %q, want the six-string one", tr.Name)
	}
	assertNotes(t, tr.Notes, []wantNote{
		{start: 0, duration: barAt120, midi: 40, str: 6, fret: 0},
	})
}

func TestGraceBeatsAreDropped(t *testing.T) {
	song := mustParse(t, doc{
		Automations: tempoXML(0, 0, 120),
		Tracks:      guitarTrack(),
		MasterBars:  masterBarXML("4/4", "0", ""),
		Bars:        barXML(0, "0 -1 -1 -1"),
		Voices:      voiceXML(0, "0 1"),
		Beats: `<Beat id="0"><Rhythm ref="0"/><GraceNotes>BeforeBeat</GraceNotes><Notes>0</Notes></Beat>` +
			beatXML(1, 1, "1"),
		Notes:   noteXML(0, 5, 3, "") + noteXML(1, 5, 5, ""),
		Rhythms: rhythmXML(0, "16th", "") + rhythmXML(1, "Whole", ""),
	})

	// The grace note is dropped and, crucially, takes none of the bar's time
	// with it: the real note still starts on the barline.
	assertNotes(t, onlyTrack(t, song).Notes, []wantNote{
		{start: 0, duration: barAt120, midi: 69, str: 1, fret: 5},
	})
}

// --- fixtures --------------------------------------------------------------

// unmarshalDoc parses a fixture without running the rest of the importer, for
// the tests that assert on one table in isolation.
func unmarshalDoc(d doc, out *gpif) error { return xml.Unmarshal(d.xml(), out) }

func makeArchive(t *testing.T, name string, payload []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatalf("creating %s: %v", name, err)
	}
	if _, err := w.Write(payload); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("closing archive: %v", err)
	}
	return buf.Bytes()
}
