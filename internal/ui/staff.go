package ui

import (
	"fmt"
	"math"

	"github.com/FullFran/riffhero/internal/practice"
)

// This file is the headless half of standard-notation rendering: where a
// sounding pitch sits on a five-line staff, and what rhythm a duration is.
// Nothing here draws anything, so all of it can be asserted on without a
// window.
//
// Deliberately not modelled, because the tab view never needed them and
// guessing at them would be worse than leaving them to the caller:
//
//   - Naturals. Accidental carries no natural sign, so a renderer that wants
//     to cancel an accidental earlier in the bar has to add one.
//   - Accidental memory. Every note is spelled from scratch; real notation
//     carries an accidental across the rest of its bar.
//   - Key signatures. Spelling is a caller's choice between sharps and flats,
//     which is what a score with no key can honestly offer.
//   - Double sharps and double flats, and therefore keys that need them.
//   - Rests, ties, slurs, tuplets and double dots. ClassifyDuration rounds a
//     duration to one writable note; a triplet quaver is not one of those.
//   - Beaming. Flags reports how many flags a note carries when it stands
//     alone; joining them into beams is a decision about a whole group.
//   - 8va and 8vb. An engraver moves the very high positions under an octave
//     line rather than stacking six ledger lines; here they get the ledgers.

// Accidental is what is drawn in front of a note head.
type Accidental uint8

const (
	NoAccidental Accidental = iota
	Sharp
	Flat
)

func (a Accidental) String() string {
	switch a {
	case Sharp:
		return "#"
	case Flat:
		return "b"
	default:
		return ""
	}
}

// Spelling picks how the black keys are named. A score model that carries no
// key signature cannot know, so the caller chooses.
type Spelling uint8

const (
	Sharps Spelling = iota
	Flats
)

// StaffPlacement is where one sounding pitch sits on a five-line staff.
//
// Step counts staff positions up from the bottom line: 0 is the bottom line,
// 1 the space above it, 2 the second line, and so on. 8 is the top line.
// Negative steps are below the staff.
type StaffPlacement struct {
	Step       int
	Accidental Accidental
	// Ledgers are the steps at which a ledger line must be drawn for this
	// note: the even-numbered line positions between the staff and the note.
	Ledgers []int
	// Name is the written pitch, e.g. "F#3" — the octave written, not sounded.
	Name string
}

// guitarTransposition is the whole reason this file cannot just place a MIDI
// number on a staff.
//
// Guitar is written in treble clef but SOUNDS AN OCTAVE LOWER THAN WRITTEN.
// The low E string sounds E2, MIDI 40, and is written E3. So the written pitch
// is the sounding pitch plus twelve semitones, not minus. Get the sign
// backwards and every note lands two octaves from where it belongs: the open
// high E, which a guitarist reads in the top space of the staff, drops to
// three ledger lines below it, and half the fretboard falls off the page.
const guitarTransposition = 12

const (
	// bottomLineIndex is the diatonic index of E4, the bottom line of a treble
	// staff. Step 0 means exactly that note.
	bottomLineIndex = 4*7 + 2
	// topStep is the top staff line: five lines, two steps apart.
	topStep = 8
	// middleLineStep is B4, the line stems turn around on.
	middleLineStep = 4
)

// staffLetters are the seven letter names in the order the staff stacks them,
// so an index into this table is also a diatonic step within an octave.
var staffLetters = [7]string{"C", "D", "E", "F", "G", "A", "B"}

// spelled is one pitch class written down: which letter carries it, and what
// goes in front of that letter.
type spelled struct {
	letter int
	acc    Accidental
}

// Two tables rather than one, because a black key has no intrinsic name. D#
// and Eb are the same key, a different letter, and — the part that matters
// here — a different line of the staff.
var (
	sharpSpelling = [12]spelled{
		{0, NoAccidental}, {0, Sharp}, {1, NoAccidental}, {1, Sharp},
		{2, NoAccidental}, {3, NoAccidental}, {3, Sharp}, {4, NoAccidental},
		{4, Sharp}, {5, NoAccidental}, {5, Sharp}, {6, NoAccidental},
	}
	flatSpelling = [12]spelled{
		{0, NoAccidental}, {1, Flat}, {1, NoAccidental}, {2, Flat},
		{2, NoAccidental}, {3, NoAccidental}, {4, Flat}, {4, NoAccidental},
		{5, Flat}, {5, NoAccidental}, {6, Flat}, {6, NoAccidental},
	}
)

// PlaceOnStaff resolves a sounding MIDI pitch onto the guitar's staff.
func PlaceOnStaff(midi uint8, spelling Spelling) StaffPlacement {
	// int, not uint8: the top of the MIDI range plus the transposition does
	// not fit in a byte, and wrapping would quietly place the highest notes
	// under the staff instead of far above it.
	written := int(midi) + guitarTransposition

	table := &sharpSpelling
	if spelling == Flats {
		table = &flatSpelling
	}
	s := table[written%12]

	// Steps are counted in letters, not semitones. A semitone-based mapping is
	// the obvious shortcut and it is wrong: it puts F# and G on the same line,
	// so an accidental would move a note head instead of just appearing in
	// front of it, and a chromatic run would climb the staff twelve positions
	// per octave instead of seven.
	octave := written/12 - 1 // MIDI 0 is C-1, so the octave runs one behind
	step := octave*7 + s.letter - bottomLineIndex

	return StaffPlacement{
		Step:       step,
		Accidental: s.acc,
		Ledgers:    ledgersFor(step),
		Name:       fmt.Sprintf("%s%s%d", staffLetters[s.letter], s.acc, octave),
	}
}

// ledgersFor is the ledger lines a note at this step needs: the line positions
// between it and the staff, plus its own line when it sits on one.
//
// It belongs here rather than in the renderer because the renderer would have
// to rediscover two things to get it right. Lines live at even steps only, and
// a note sitting in a space beyond the staff still needs the ledger on its
// staff side drawn — B5 is above the first ledger line and would otherwise
// float with nothing under it.
func ledgersFor(step int) []int {
	var out []int
	switch {
	case step > topStep:
		for s := topStep + 2; s <= step; s += 2 {
			out = append(out, s)
		}
	case step < 0:
		for s := -2; s >= step; s -= 2 {
			out = append(out, s)
		}
	}
	return out
}

// NoteValue is the rhythmic value a duration rounds to.
type NoteValue uint8

const (
	Whole NoteValue = iota
	Half
	Quarter
	Eighth
	Sixteenth
	ThirtySecond
)

var noteValueNames = [...]string{"whole", "half", "quarter", "eighth", "sixteenth", "thirty-second"}

// noteValueQuarters is how long each value lasts in quarter notes, which is
// the unit BPM is quoted in.
var noteValueQuarters = [...]float64{4, 2, 1, 0.5, 0.25, 0.125}

func (v NoteValue) String() string {
	if int(v) >= len(noteValueNames) {
		return "unknown"
	}
	return noteValueNames[v]
}

// Hollow reports whether the head is drawn open rather than filled.
func (v NoteValue) Hollow() bool { return v == Whole || v == Half }

// Stem reports whether the note carries a stem.
func (v NoteValue) Stem() bool { return v != Whole }

// Flags is how many flags hang off the stem.
func (v NoteValue) Flags() int {
	if v <= Quarter || int(v) >= len(noteValueQuarters) {
		return 0
	}
	return int(v - Quarter)
}

// Rhythm is a duration rounded to something writable.
type Rhythm struct {
	Value  NoteValue
	Dotted bool
}

// dotFactor is what a dot does to a duration: it adds half of it again.
const dotFactor = 1.5

// tieEpsilon is how much closer a candidate has to be before the difference
// counts as real.
//
// It exists because a duration landing on the geometric midpoint between a
// plain value and a dotted one is not a hypothetical — a quantised importer
// produces them exactly — and the two log distances then differ in their last
// bit, by around 1e-16, in whichever direction the library's rounding happens
// to fall. Measured, it falls towards the dotted value, so without this the
// simpler note would lose every time on nothing but arithmetic noise. No two
// distinguishable rhythms are anywhere near this close.
const tieEpsilon = 1e-12

// ClassifyDuration turns a sounding duration into the rhythm it is closest to,
// given the tempo it is played at. bpm always counts quarter notes.
func ClassifyDuration(clock practice.Clock, bpm float64, d practice.Frame) Rhythm {
	quarters := clock.Seconds(d) * bpm / 60

	// Written as a failed positive test so a NaN — from a tempo map that
	// divided by an empty section, which is where these come from — lands here
	// rather than sailing through into math.Log and picking a value at random.
	//
	// A plain quarter is the neutral answer. The tempting alternative is the
	// shortest value, which is where a log metric tends as the duration goes
	// to zero, but a thirty-second note reads as a real and very fast note and
	// sends the player hunting for a phrase nobody played.
	if !(quarters > 0) || math.IsInf(quarters, 0) {
		return Rhythm{Value: Quarter}
	}
	return classifyQuarters(quarters)
}

// classifyQuarters picks the nearest writable rhythm to a positive, finite
// length measured in quarter notes.
//
// Nearest is measured in log duration, not in seconds. A player's eighth note
// runs late by a few milliseconds and their whole note by a few hundred, and a
// linear metric calls the second one the far worse mistake — so it rounds long
// notes into the wrong value while getting short ones trivially right. Ratios
// are what a musician hears, and the boundary between a quarter and a dotted
// eighth belongs at their geometric midpoint, not halfway between them.
func classifyQuarters(quarters float64) Rhythm {
	target := math.Log(quarters)

	best := Rhythm{}
	bestDist := math.Inf(1)

	// Every plain value is tried before any dotted one, and displacing the
	// incumbent takes a distance smaller by more than rounding. Together those
	// are the tie-break: a duration landing exactly between a plain value and
	// a dotted one is written the simpler way, because a dot is a claim about
	// the music and should have to earn its ink.
	for _, dotted := range [...]bool{false, true} {
		for v := Whole; v <= ThirtySecond; v++ {
			q := noteValueQuarters[v]
			if dotted {
				q *= dotFactor
			}
			if dist := math.Abs(target - math.Log(q)); dist < bestDist-tieEpsilon {
				best, bestDist = Rhythm{Value: v, Dotted: dotted}, dist
			}
		}
	}
	return best
}

// StaffLayout maps staff steps onto screen coordinates.
type StaffLayout struct {
	Top     float64 // y of the top staff line
	LineGap float64 // distance between adjacent staff lines
}

// Y is the vertical position of a staff step.
//
// Steps count upwards and screen y counts downwards, and a step is half a line
// gap because the spaces are positions too. Both of those are sign and factor
// errors worth making exactly once, here, rather than in every caller.
func (s StaffLayout) Y(step int) float64 {
	return s.Top + float64(topStep-step)*s.LineGap/2
}

// LineY is the vertical position of staff line n, 0 being the top line.
func (s StaffLayout) LineY(n int) float64 { return s.Y(topStep - 2*n) }

// Height is the distance from the top line to the bottom line.
func (s StaffLayout) Height() float64 { return 4 * s.LineGap }

// StemUp reports whether a note at this step should have its stem drawn
// upwards: below the middle line it points up, at or above it points down.
func StemUp(step int) bool { return step < middleLineStep }
