package practice

import "strings"

// MaxFret is the highest fret RiffHero will place a note on. Nothing on a
// normal electric guitar goes past it, and bounding the search keeps position
// resolution total.
const MaxFret = 24

// Tuning is the open pitch of each string, indexed by tab string number minus
// one: index 0 is string 1, the high E. Capo shifts every string up by the
// same number of semitones, and fret numbers stay relative to it, which is how
// tablature is written.
type Tuning struct {
	Name    string
	Strings [6]uint8
	Capo    uint8
}

var (
	StandardTuning = Tuning{Name: "Standard", Strings: [6]uint8{64, 59, 55, 50, 45, 40}}
	DropDTuning    = Tuning{Name: "Drop D", Strings: [6]uint8{64, 59, 55, 50, 45, 38}}
	HalfStepDown   = Tuning{Name: "Eb Standard", Strings: [6]uint8{63, 58, 54, 49, 44, 39}}
)

// MIDI resolves a tab position into a MIDI note number. The string number is
// clamped into the 1..6 tab range.
func (t Tuning) MIDI(str, fret uint8) uint8 {
	v := int(t.Strings[clampString(str)-1]) + int(t.Capo) + int(fret)
	if v > 127 {
		return 127
	}
	return uint8(v)
}

// OpenMIDI is the sounding pitch of a string played open, capo included.
func (t Tuning) OpenMIDI(str uint8) uint8 { return t.MIDI(str, 0) }

// Position resolves a pitch to the tab position nearest to hintFret. Importers
// that carry no tablature use it to place a bare MIDI note somewhere playable,
// and passing the previous note's fret keeps a phrase inside one box instead of
// scattering it over the neck.
//
// A negative hint means "no preference", which resolves to the lowest fret that
// can sound the pitch.
func (t Tuning) Position(midi uint8, hintFret int) (str, fret uint8, ok bool) {
	return t.position(midi, hintFret, -1)
}

// position is Position with the string the hand was last on as well.
//
// The string matters because fret distance alone is not enough. Two positions
// an equal number of frets from the hand are not equally good: the one on a
// neighbouring string is the one a player would actually reach for, and
// without that tie-break a scale run keeps hopping across the neck between
// equally-scored alternatives.
func (t Tuning) position(midi uint8, hintFret, hintStr int) (str, fret uint8, ok bool) {
	if hintFret < 0 {
		hintFret = 0
	}

	bestScore := -1
	for s := uint8(1); s <= 6; s++ {
		open := int(t.Strings[s-1]) + int(t.Capo)
		f := int(midi) - open
		if f < 0 || f > MaxFret {
			continue
		}
		// Fret distance decides, then string distance, then the lower fret so
		// that a phrase with no history at all stays in open position.
		score := abs(f-hintFret) * 1000
		if hintStr > 0 {
			score += abs(int(s)-hintStr) * 10
		}
		score += f
		if bestScore < 0 || score < bestScore {
			bestScore, str, fret, ok = score, s, uint8(f), true
		}
	}
	return str, fret, ok
}

// HandSpan is how many frets past its anchor a hand covers. Five frets, one
// per finger plus the stretch, is the position every scale shape is taught in.
const HandSpan = 4

// Fretboard places a melody on the neck one note at a time, holding the hand
// in a position rather than merely near the last note.
//
// The difference is the whole point. Following the last *fret* looks right and
// is not: going up a scale, the next note is two frets along the same string
// and crossing to the next string is four or five away, so the cheapest move
// is always to stay put — and a two-octave scale comes out as a single string
// climbed from the 5th fret to the 20th, which nobody has ever played. Holding
// a position and crossing strings inside it is what a guitarist actually does,
// and the anchor only moves when the note genuinely will not fit.
type Fretboard struct {
	Tuning Tuning
	anchor int // lowest fret of the hand's position, -1 before the first note
	hintSt int
}

func NewFretboard(t Tuning) *Fretboard { return &Fretboard{Tuning: t, anchor: -1, hintSt: -1} }

// Place resolves one pitch and moves the hand only if it has to.
func (f *Fretboard) Place(midi uint8) (str, fret uint8, ok bool) {
	str, fret, ok = f.Tuning.inPosition(midi, f.anchor, f.hintSt)
	if !ok {
		return str, fret, ok
	}
	// An open string never moves the hand, in either direction: playing one
	// costs no reach, so it must not drag the position back down the neck
	// either.
	if fret != 0 && !inHand(int(fret), f.anchor) {
		f.anchor = anchorFor(int(fret))
	}
	f.hintSt = int(str)
	return str, fret, ok
}

// Anchor is the lowest fret of the position the hand is currently in, or -1
// before the first note.
func (f *Fretboard) Anchor() int { return f.anchor }

func inHand(fret, anchor int) bool {
	return anchor >= 0 && fret >= anchor && fret <= anchor+HandSpan
}

// anchorFor centres a new position on the note that forced the move, so a run
// that has to shift can carry on in either direction.
func anchorFor(fret int) int {
	anchor := fret - HandSpan/2
	if anchor < 0 {
		return 0
	}
	return anchor
}

// inPosition resolves a pitch, preferring anywhere the hand can already reach.
//
// An open string counts as reachable from any position, because it is: the
// hand does not move to play one.
func (t Tuning) inPosition(midi uint8, anchor, hintStr int) (str, fret uint8, ok bool) {
	bestScore := -1
	for s := uint8(1); s <= 6; s++ {
		open := int(t.Strings[s-1]) + int(t.Capo)
		f := int(midi) - open
		if f < 0 || f > MaxFret {
			continue
		}

		var score int
		switch {
		case f == 0 || inHand(f, anchor):
			// Inside the hand: the only question left is which string, and the
			// nearest one is the one a player reaches for.
			score = abs(int(s)-hintStr) * 10
			if hintStr < 0 {
				score = f
			}
		default:
			// Outside it: how far the hand would have to move, then the same
			// tie-breaks as before.
			score = 10000 + reachCost(f, anchor)*1000 + abs(int(s)-hintStr)*10 + f
		}
		if bestScore < 0 || score < bestScore {
			bestScore, str, fret, ok = score, s, uint8(f), true
		}
	}
	return str, fret, ok
}

// reachCost is how far the hand must shift to reach a fret.
func reachCost(fret, anchor int) int {
	if anchor < 0 {
		return fret
	}
	switch {
	case fret < anchor:
		return anchor - fret
	case fret > anchor+HandSpan:
		return fret - anchor - HandSpan
	}
	return 0
}

// PlaceOrTranspose resolves a pitch to a position on the neck, moving it by
// whole octaves when it does not fit, and returns the pitch it actually
// placed.
//
// Importers need this because a file written for another instrument routinely
// goes outside a guitar's range, and the two obvious alternatives are both
// worse. Dropping the note loses the part. Clamping to the nearest end of the
// range flattens a bass line into a drone — and if the caller then keeps the
// original pitch, as one importer did, the tablature and the expected note
// contradict each other and nothing the player does can resolve either.
//
// ok is false only for a pitch no octave of which fits, which cannot happen
// for a six-string guitar and any input under MIDI 128.
func (f *Fretboard) PlaceOrTranspose(midi uint8) (placed, str, fret uint8, ok bool) {
	lowest := f.Tuning.MIDI(6, 0)
	highest := f.Tuning.MIDI(1, MaxFret)

	placed = midi
	for placed < lowest && placed <= 127-12 {
		placed += 12
	}
	for placed > highest && placed >= 12 {
		placed -= 12
	}
	if placed < lowest || placed > highest {
		return midi, 0, 0, false
	}

	str, fret, ok = f.Place(placed)
	return placed, str, fret, ok
}

// knownTunings are the ones worth recognizing by name in a header.
var knownTunings = []Tuning{StandardTuning, DropDTuning, HalfStepDown}

// Named fills in a name for a tuning recovered from a file.
//
// An importer reading a tab staff knows the pitches and not what they are
// called, and "Tab" in the header of a song in standard tuning tells the
// player nothing the strings themselves do not. A tuning nobody has a word for
// is spelled out low to high, the way a guitarist reads one off a headstock.
func (t Tuning) Named() Tuning {
	for _, known := range knownTunings {
		if t.Strings == known.Strings {
			t.Name = known.Name
			return t
		}
	}

	// A zero slot is a string the instrument does not have. The array is six
	// long because a guitar is, and a four-string bass leaves the top two
	// empty rather than inventing pitches for them.
	names := make([]string, 0, len(t.Strings))
	for i := len(t.Strings) - 1; i >= 0; i-- {
		if t.Strings[i] == 0 {
			continue
		}
		names = append(names, pitchClassNames[t.Strings[i]%12])
	}
	if len(names) == 0 {
		t.Name = "Unknown"
		return t
	}
	t.Name = strings.Join(names, " ")
	return t
}

var pitchClassNames = [12]string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}

// Sounds reports whether a tab position produces the given pitch. Importers
// that carry their own tablature use it to check the file agrees with itself:
// a position that sounds something other than the written note would show the
// player one thing and score another.
func (t Tuning) Sounds(midi, str, fret uint8) bool {
	if str < 1 || str > 6 || fret > MaxFret {
		return false
	}
	return t.MIDI(str, fret) == midi
}

// Reset forgets the hand position.
func (f *Fretboard) Reset() { f.anchor, f.hintSt = -1, -1 }

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
