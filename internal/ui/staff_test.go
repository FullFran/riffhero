package ui

import (
	"math"
	"slices"
	"testing"

	"github.com/FullFran/riffhero/internal/practice"
)

func staffClock() practice.Clock { return practice.Clock{SampleRate: 48000} }

func staffFixture() StaffLayout { return StaffLayout{Top: 120, LineGap: 12} }

func TestOpenStringsLandWhereAGuitaristReadsThem(t *testing.T) {
	// Driven off the tuning table rather than off hard-coded MIDI numbers, so
	// that changing standard tuning breaks this test instead of quietly
	// changing what the staff claims the open strings are.
	cases := []struct {
		str     uint8
		sounds  uint8
		name    string
		step    int
		ledgers []int
	}{
		{1, 64, "E5", 7, nil},                // high E, top space
		{2, 59, "B4", 4, nil},                // middle line
		{3, 55, "G4", 2, nil},                // the G the clef curls around
		{4, 50, "D4", -1, nil},               // space below the staff
		{5, 45, "A3", -4, []int{-2, -4}},     // second ledger line
		{6, 40, "E3", -7, []int{-2, -4, -6}}, // below three ledger lines
	}

	for _, c := range cases {
		if got := practice.StandardTuning.OpenMIDI(c.str); got != c.sounds {
			t.Fatalf("string %d opens on MIDI %d, want %d", c.str, got, c.sounds)
		}
		p := PlaceOnStaff(c.sounds, Sharps)
		if p.Name != c.name {
			t.Fatalf("string %d (MIDI %d) is written %q, want %q", c.str, c.sounds, p.Name, c.name)
		}
		if p.Step != c.step {
			t.Fatalf("string %d (%s) is at step %d, want %d", c.str, c.name, p.Step, c.step)
		}
		if !slices.Equal(p.Ledgers, c.ledgers) {
			t.Fatalf("string %d (%s) has ledgers %v, want %v", c.str, c.name, p.Ledgers, c.ledgers)
		}
		if p.Accidental != NoAccidental {
			t.Fatalf("string %d (%s) picked up accidental %q", c.str, c.name, p.Accidental)
		}
	}
}

func TestGuitarIsWrittenAnOctaveAboveWhatItSounds(t *testing.T) {
	// The single easiest thing to get backwards in this file. The open high E
	// sounds E4 and is READ in the top space, E5; the low E sounds E2 and is
	// READ as E3, hanging below three ledger lines. Subtract the octave
	// instead of adding it and both land two octaves out.
	high := PlaceOnStaff(64, Sharps)
	if high.Name != "E5" || high.Step != 7 {
		t.Fatalf("sounding E4 is written %q at step %d, want E5 at step 7", high.Name, high.Step)
	}
	if len(high.Ledgers) != 0 {
		t.Fatalf("the open high E should sit inside the staff, got ledgers %v", high.Ledgers)
	}

	low := PlaceOnStaff(40, Sharps)
	if low.Name != "E3" || low.Step != -7 {
		t.Fatalf("sounding E2 is written %q at step %d, want E3 at step -7", low.Name, low.Step)
	}

	// Two E strings, two octaves apart in sound, fourteen staff steps apart on
	// the page — seven per octave, which only holds for a diatonic mapping.
	if high.Step-low.Step != 14 {
		t.Fatalf("two octaves span %d steps, want 14", high.Step-low.Step)
	}
}

func TestMiddleCSitsOnOneLedgerBelowTheStaff(t *testing.T) {
	// The note a reader calls middle C — C4 on the page, one ledger below a
	// treble staff — is sounded by a guitar as C3, MIDI 48.
	written := PlaceOnStaff(48, Sharps)
	if written.Name != "C4" || written.Step != -2 {
		t.Fatalf("sounding C3 is written %q at step %d, want C4 at step -2", written.Name, written.Step)
	}
	if !slices.Equal(written.Ledgers, []int{-2}) {
		t.Fatalf("written middle C has ledgers %v, want exactly one at -2", written.Ledgers)
	}

	// Sounding middle C, MIDI 60, is a different note on the page: it is
	// written C5 and sits inside the staff with no ledger at all. Anyone who
	// reaches for "middle C is one ledger below" and passes 60 gets this.
	sounding := PlaceOnStaff(60, Sharps)
	if sounding.Name != "C5" || sounding.Step != 5 {
		t.Fatalf("sounding C4 is written %q at step %d, want C5 at step 5", sounding.Name, sounding.Step)
	}
	if len(sounding.Ledgers) != 0 {
		t.Fatalf("sounding middle C should need no ledgers, got %v", sounding.Ledgers)
	}
}

func TestChromaticOctaveUsesSevenStepsNotTwelve(t *testing.T) {
	// The test that catches a semitone-based mapping. Twelve chromatic pitches
	// occupy seven staff positions, because F# and F share a line and only the
	// accidental in front of them differs.
	cases := []struct {
		midi uint8
		name string
		step int
		acc  Accidental
	}{
		{60, "C5", 5, NoAccidental},
		{61, "C#5", 5, Sharp},
		{62, "D5", 6, NoAccidental},
		{63, "D#5", 6, Sharp},
		{64, "E5", 7, NoAccidental},
		{65, "F5", 8, NoAccidental},
		{66, "F#5", 8, Sharp},
		{67, "G5", 9, NoAccidental},
		{68, "G#5", 9, Sharp},
		{69, "A5", 10, NoAccidental},
		{70, "A#5", 10, Sharp},
		{71, "B5", 11, NoAccidental},
	}

	steps := map[int]bool{}
	for _, c := range cases {
		p := PlaceOnStaff(c.midi, Sharps)
		if p.Name != c.name {
			t.Fatalf("MIDI %d is written %q, want %q", c.midi, p.Name, c.name)
		}
		if p.Step != c.step {
			t.Fatalf("MIDI %d (%s) is at step %d, want %d", c.midi, c.name, p.Step, c.step)
		}
		if p.Accidental != c.acc {
			t.Fatalf("MIDI %d (%s) has accidental %q, want %q", c.midi, c.name, p.Accidental, c.acc)
		}
		steps[p.Step] = true
	}
	if len(steps) != 7 {
		t.Fatalf("a chromatic octave used %d staff positions, want 7", len(steps))
	}
}

func TestFlatsSpellAndPlaceDifferentlyFromSharps(t *testing.T) {
	// Eb and D# are the same key and different notes: different letter,
	// different accidental, and — the part a renderer cannot recover on its
	// own — a different line of the staff.
	cases := []struct {
		midi                  uint8
		sharpName, flatName   string
		sharpStep, flatStep   int
		sharpAcc, flatAccWant Accidental
	}{
		{61, "C#5", "Db5", 5, 6, Sharp, Flat},
		{63, "D#5", "Eb5", 6, 7, Sharp, Flat},
		{66, "F#5", "Gb5", 8, 9, Sharp, Flat},
		{68, "G#5", "Ab5", 9, 10, Sharp, Flat},
		{70, "A#5", "Bb5", 10, 11, Sharp, Flat},
	}

	for _, c := range cases {
		sh, fl := PlaceOnStaff(c.midi, Sharps), PlaceOnStaff(c.midi, Flats)
		if sh.Name != c.sharpName || fl.Name != c.flatName {
			t.Fatalf("MIDI %d spelled %q/%q, want %q/%q", c.midi, sh.Name, fl.Name, c.sharpName, c.flatName)
		}
		if sh.Step != c.sharpStep || fl.Step != c.flatStep {
			t.Fatalf("MIDI %d placed at %d/%d, want %d/%d", c.midi, sh.Step, fl.Step, c.sharpStep, c.flatStep)
		}
		if sh.Step == fl.Step {
			t.Fatalf("MIDI %d put %s and %s on the same staff position", c.midi, sh.Name, fl.Name)
		}
		if sh.Accidental != c.sharpAcc || fl.Accidental != c.flatAccWant {
			t.Fatalf("MIDI %d gave accidentals %q/%q", c.midi, sh.Accidental, fl.Accidental)
		}
	}

	// A white key is spelled the same either way; only the black keys move.
	for _, midi := range []uint8{60, 62, 64, 65, 67, 69, 71} {
		sh, fl := PlaceOnStaff(midi, Sharps), PlaceOnStaff(midi, Flats)
		if sh.Name != fl.Name || sh.Step != fl.Step || sh.Accidental != fl.Accidental {
			t.Fatalf("MIDI %d differs between spellings: %+v vs %+v", midi, sh, fl)
		}
		if sh.Accidental != NoAccidental {
			t.Fatalf("MIDI %d is a white key but was given accidental %q", midi, sh.Accidental)
		}
	}
}

func TestStaffLinesAreEvenStepsAndNeedNoLedgers(t *testing.T) {
	// EGBDF read from the bottom, at steps 0,2,4,6,8, sounded an octave lower
	// than each is written.
	cases := []struct {
		sounds uint8
		name   string
		step   int
		line   int // 0 is the top line, the order LineY counts in
	}{
		{52, "E4", 0, 4},
		{55, "G4", 2, 3},
		{59, "B4", 4, 2},
		{62, "D5", 6, 1},
		{65, "F5", 8, 0},
	}

	layout := staffFixture()
	for _, c := range cases {
		p := PlaceOnStaff(c.sounds, Sharps)
		if p.Name != c.name || p.Step != c.step {
			t.Fatalf("MIDI %d is %q at step %d, want %s at step %d", c.sounds, p.Name, p.Step, c.name, c.step)
		}
		if len(p.Ledgers) != 0 {
			t.Fatalf("%s is on the staff and needs no ledgers, got %v", c.name, p.Ledgers)
		}
		if got, want := layout.Y(p.Step), layout.LineY(c.line); got != want {
			t.Fatalf("%s draws at y=%v but line %d is at y=%v", c.name, got, c.line, want)
		}
	}

	// The spaces between them are the odd steps, and they are not lines.
	for _, step := range []int{1, 3, 5, 7} {
		if got := ledgersFor(step); len(got) != 0 {
			t.Fatalf("step %d is a space inside the staff, got ledgers %v", step, got)
		}
	}
}

func TestLedgerLinesEitherSideOfTheStaff(t *testing.T) {
	// A note in a space beyond the staff still needs the ledger on its staff
	// side drawn, or it floats with nothing under it. That is why B5 at step
	// 11 gets one and G5 at step 9 gets none.
	cases := []struct {
		sounds  uint8
		name    string
		step    int
		ledgers []int
	}{
		{67, "G5", 9, nil},                    // first space above, no ledger
		{69, "A5", 10, []int{10}},             // first ledger above
		{71, "B5", 11, []int{10}},             // above the first ledger
		{72, "C6", 12, []int{10, 12}},         // second ledger above
		{76, "E6", 14, []int{10, 12, 14}},     // third ledger above
		{50, "D4", -1, nil},                   // first space below, no ledger
		{48, "C4", -2, []int{-2}},             // first ledger below
		{47, "B3", -3, []int{-2}},             // below the first ledger
		{45, "A3", -4, []int{-2, -4}},         // second ledger below
		{40, "E3", -7, []int{-2, -4, -6}},     // below the third ledger
		{38, "D3", -8, []int{-2, -4, -6, -8}}, // fourth ledger below, drop D
	}

	for _, c := range cases {
		p := PlaceOnStaff(c.sounds, Sharps)
		if p.Name != c.name || p.Step != c.step {
			t.Fatalf("MIDI %d is %q at step %d, want %s at step %d", c.sounds, p.Name, p.Step, c.name, c.step)
		}
		if !slices.Equal(p.Ledgers, c.ledgers) {
			t.Fatalf("%s has ledgers %v, want %v", c.name, p.Ledgers, c.ledgers)
		}
	}
}

func TestPlaceOnStaffIsTotalAcrossTheMIDIRange(t *testing.T) {
	// A panic here would be an out-of-range pitch from an importer taking the
	// whole practice view down, which is a poor trade for a note nobody can
	// play. The arithmetic also has to stay in int: 255 plus the octave
	// transposition does not fit in the byte it arrived in.
	for _, spelling := range []Spelling{Sharps, Flats, Spelling(200)} {
		prev := math.MinInt
		for m := 0; m <= 255; m++ {
			p := PlaceOnStaff(uint8(m), spelling)

			if p.Name == "" {
				t.Fatalf("MIDI %d produced no written name", m)
			}
			if prev != math.MinInt && p.Step-prev != 0 && p.Step-prev != 1 {
				t.Fatalf("MIDI %d jumped %d steps from its neighbour", m, p.Step-prev)
			}
			prev = p.Step

			for _, l := range p.Ledgers {
				if l%2 != 0 {
					t.Fatalf("MIDI %d wants a ledger at odd step %d, which is a space", m, l)
				}
				if p.Step > topStep && (l < topStep+2 || l > p.Step) {
					t.Fatalf("MIDI %d (step %d) wants a stray ledger at %d", m, p.Step, l)
				}
				if p.Step < 0 && (l > -2 || l < p.Step) {
					t.Fatalf("MIDI %d (step %d) wants a stray ledger at %d", m, p.Step, l)
				}
			}
			if p.Step >= 0 && p.Step <= topStep && len(p.Ledgers) != 0 {
				t.Fatalf("MIDI %d is on the staff at step %d but wants ledgers %v", m, p.Step, p.Ledgers)
			}
		}
	}
}

func TestAccidentalString(t *testing.T) {
	cases := map[Accidental]string{
		NoAccidental: "", Sharp: "#", Flat: "b", Accidental(9): "",
	}
	for acc, want := range cases {
		if got := acc.String(); got != want {
			t.Fatalf("accidental %d = %q, want %q", acc, got, want)
		}
	}
}

func TestNoteValueDrawing(t *testing.T) {
	cases := []struct {
		v      NoteValue
		name   string
		hollow bool
		stem   bool
		flags  int
	}{
		{Whole, "whole", true, false, 0},
		{Half, "half", true, true, 0},
		{Quarter, "quarter", false, true, 0},
		{Eighth, "eighth", false, true, 1},
		{Sixteenth, "sixteenth", false, true, 2},
		{ThirtySecond, "thirty-second", false, true, 3},
		// Nothing constructs this, but a renderer indexing a glyph table off
		// Flags must not be handed a number that walks off the end of it.
		{NoteValue(99), "unknown", false, true, 0},
	}

	for _, c := range cases {
		if got := c.v.String(); got != c.name {
			t.Fatalf("value %d = %q, want %q", c.v, got, c.name)
		}
		if got := c.v.Hollow(); got != c.hollow {
			t.Fatalf("%s hollow = %v, want %v", c.name, got, c.hollow)
		}
		if got := c.v.Stem(); got != c.stem {
			t.Fatalf("%s stem = %v, want %v", c.name, got, c.stem)
		}
		if got := c.v.Flags(); got != c.flags {
			t.Fatalf("%s flags = %d, want %d", c.name, got, c.flags)
		}
	}
}

func TestClassifyDuration(t *testing.T) {
	clock := staffClock()
	cases := []struct {
		bpm  float64
		secs float64
		want Rhythm
	}{
		// 60 BPM: a quarter note is exactly one second, so the table reads
		// straight off the clock.
		{60, 4, Rhythm{Value: Whole}},
		{60, 6, Rhythm{Value: Whole, Dotted: true}},
		{60, 2, Rhythm{Value: Half}},
		{60, 3, Rhythm{Value: Half, Dotted: true}},
		{60, 1, Rhythm{Value: Quarter}},
		{60, 1.5, Rhythm{Value: Quarter, Dotted: true}},
		{60, 0.5, Rhythm{Value: Eighth}},
		{60, 0.75, Rhythm{Value: Eighth, Dotted: true}},
		{60, 0.25, Rhythm{Value: Sixteenth}},
		{60, 0.375, Rhythm{Value: Sixteenth, Dotted: true}},
		{60, 0.125, Rhythm{Value: ThirtySecond}},
		{60, 0.1875, Rhythm{Value: ThirtySecond, Dotted: true}},

		// Durations off the end of what is writable clamp rather than wrap.
		{60, 30, Rhythm{Value: Whole, Dotted: true}},
		{60, 0.004, Rhythm{Value: ThirtySecond}},

		// Real playing, not a grid. A quarter held 5% long is still a quarter.
		{60, 1.05, Rhythm{Value: Quarter}},
		{60, 0.96, Rhythm{Value: Quarter}},
		{60, 2.9, Rhythm{Value: Half, Dotted: true}},

		// 100 BPM: a quarter is 0.6 s.
		{100, 2.4, Rhythm{Value: Whole}},
		{100, 1.2, Rhythm{Value: Half}},
		{100, 0.6, Rhythm{Value: Quarter}},
		{100, 0.9, Rhythm{Value: Quarter, Dotted: true}},
		{100, 0.3, Rhythm{Value: Eighth}},
		{100, 0.45, Rhythm{Value: Eighth, Dotted: true}},
		{100, 0.15, Rhythm{Value: Sixteenth}},

		// 180 BPM: a quarter is a third of a second, and the same physical
		// half-second is a dotted quarter here and a dotted eighth at 60.
		{180, 4.0 / 3, Rhythm{Value: Whole}},
		{180, 2.0 / 3, Rhythm{Value: Half}},
		{180, 1.0 / 3, Rhythm{Value: Quarter}},
		{180, 0.5, Rhythm{Value: Quarter, Dotted: true}},
		{180, 1.0 / 6, Rhythm{Value: Eighth}},
		{180, 0.25, Rhythm{Value: Eighth, Dotted: true}},
		{180, 1.0 / 12, Rhythm{Value: Sixteenth}},
		{60, 0.5, Rhythm{Value: Eighth}},
	}

	for _, c := range cases {
		got := ClassifyDuration(clock, c.bpm, clock.Frames(c.secs))
		if got != c.want {
			t.Fatalf("%.4f s at %g BPM = %v (dotted %v), want %v (dotted %v)",
				c.secs, c.bpm, got.Value, got.Dotted, c.want.Value, c.want.Dotted)
		}
	}
}

func TestClassifyDurationRoundsOnTheGeometricMidpoint(t *testing.T) {
	// The boundary between a dotted eighth (0.75 quarters) and a quarter sits
	// at sqrt(0.75) = 0.86603 quarters, not at 0.875. At 60 BPM and 48 kHz
	// that lands between frame 41569 and frame 41570, so the crossover is one
	// frame wide and can be pinned exactly.
	clock := staffClock()

	if got := ClassifyDuration(clock, 60, 41569); got != (Rhythm{Value: Eighth, Dotted: true}) {
		t.Fatalf("just under the midpoint = %v (dotted %v), want a dotted eighth", got.Value, got.Dotted)
	}
	if got := ClassifyDuration(clock, 60, 41570); got != (Rhythm{Value: Quarter}) {
		t.Fatalf("just over the midpoint = %v (dotted %v), want a plain quarter", got.Value, got.Dotted)
	}

	// 0.87 s is the case that separates the two metrics: a linear one puts the
	// boundary at 0.875 and would still call this a dotted eighth.
	if got := ClassifyDuration(clock, 60, clock.Frames(0.87)); got != (Rhythm{Value: Quarter}) {
		t.Fatalf("0.87 s at 60 BPM = %v (dotted %v), want a plain quarter", got.Value, got.Dotted)
	}
}

func TestClassifyDurationPrefersPlainOverDotted(t *testing.T) {
	// A duration sitting exactly between a plain value and a dotted one is
	// written the simpler way. This goes at the helper rather than through a
	// frame count, because 48 kHz cannot land on these ratios exactly and the
	// rounding would decide the answer instead of the rule.
	//
	// It is not a hypothetical tie either: measured, the two log distances at
	// a midpoint differ by about 1e-16 in the dotted value's favour, so
	// without tieEpsilon every one of these picks up a dot it has not earned.
	for _, c := range []struct {
		quarters float64
		between  string
		want     NoteValue
	}{
		{math.Sqrt(0.1875 * 0.25), "a dotted thirty-second and a sixteenth", Sixteenth},
		{math.Sqrt(0.75 * 1), "a dotted eighth and a quarter", Quarter},
		{math.Sqrt(1 * 1.5), "a quarter and a dotted quarter", Quarter},
		{math.Sqrt(1.5 * 2), "a dotted quarter and a half", Half},
		{math.Sqrt(3 * 4), "a dotted half and a whole", Whole},
	} {
		got := classifyQuarters(c.quarters)
		if got.Dotted || got.Value != c.want {
			t.Fatalf("exactly between %s = %v (dotted %v), want a plain %v",
				c.between, got.Value, got.Dotted, c.want)
		}
	}
}

func TestClassifyDurationSurvivesNonsense(t *testing.T) {
	// Every one of these has a real caller: a note with no measured length, a
	// section whose tempo was never filled in, a clock built before a device
	// was opened, and a tempo map that divided by an empty bar count.
	clock := staffClock()
	want := Rhythm{Value: Quarter}

	cases := []struct {
		what  string
		clock practice.Clock
		bpm   float64
		d     practice.Frame
	}{
		{"zero duration", clock, 120, 0},
		{"negative duration", clock, 120, -48000},
		{"zero tempo", clock, 0, 48000},
		{"negative tempo", clock, -120, 48000},
		{"NaN tempo", clock, math.NaN(), 48000},
		{"infinite tempo", clock, math.Inf(1), 48000},
		{"clock with no sample rate", practice.Clock{}, 120, 48000},
	}

	for _, c := range cases {
		if got := ClassifyDuration(c.clock, c.bpm, c.d); got != want {
			t.Fatalf("%s = %v (dotted %v), want a plain quarter", c.what, got.Value, got.Dotted)
		}
	}
}

func TestStaffLayoutMeasurements(t *testing.T) {
	s := staffFixture()

	if got := s.Y(topStep); got != s.Top {
		t.Fatalf("the top line draws at y=%v, want Top %v", got, s.Top)
	}
	if got, want := s.Y(0), s.Top+s.Height(); got != want {
		t.Fatalf("the bottom line draws at y=%v, want %v", got, want)
	}
	if got := s.Height(); got != 4*s.LineGap {
		t.Fatalf("height %v spans the wrong number of gaps", got)
	}
	// A space is half a gap, not a whole one. Getting that wrong doubles the
	// height of the staff and every note lands between the lines.
	if got, want := s.Y(1)-s.Y(2), s.LineGap/2; got != want {
		t.Fatalf("one step is %v, want half a line gap %v", got, want)
	}
	for n := 0; n <= 4; n++ {
		if got, want := s.LineY(n), s.Top+float64(n)*s.LineGap; got != want {
			t.Fatalf("line %d draws at y=%v, want %v", n, got, want)
		}
	}
}

func TestStaffLayoutYRisesAsStepsRise(t *testing.T) {
	// Screen y counts downwards and staff steps count upwards, so a higher
	// note must come out with a smaller y. This is the sign every staff
	// renderer gets wrong once.
	s := staffFixture()

	prev := math.Inf(1)
	for step := -12; step <= 20; step++ {
		y := s.Y(step)
		if y >= prev {
			t.Fatalf("step %d draws at y=%v, not above step %d at y=%v", step, y, step-1, prev)
		}
		prev = y
	}

	high := PlaceOnStaff(64, Sharps) // sounding E4, written E5
	low := PlaceOnStaff(40, Sharps)  // sounding E2, written E3
	if s.Y(high.Step) >= s.Y(low.Step) {
		t.Fatalf("the high E draws at y=%v, not above the low E at y=%v", s.Y(high.Step), s.Y(low.Step))
	}
}

func TestStemUpFlipsAtTheMiddleLine(t *testing.T) {
	cases := map[int]bool{
		-7: true, -2: true, 0: true, 3: true, // below the middle line
		4: false, 5: false, 8: false, 14: false, // at it or above it
	}
	for step, want := range cases {
		if got := StemUp(step); got != want {
			t.Fatalf("step %d stem up = %v, want %v", step, got, want)
		}
	}

	// The middle line is B4, which is the second string played open.
	middle := PlaceOnStaff(59, Sharps)
	if middle.Name != "B4" || middle.Step != middleLineStep {
		t.Fatalf("the middle line is %q at step %d, want B4 at %d", middle.Name, middle.Step, middleLineStep)
	}
	if StemUp(middle.Step) {
		t.Fatal("a note on the middle line points its stem down")
	}
	if !StemUp(middle.Step - 1) {
		t.Fatal("the space below the middle line points its stem up")
	}
}
