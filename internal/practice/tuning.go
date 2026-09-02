package practice

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

// Fretboard places a melody on the neck one note at a time, remembering where
// the hand was left so consecutive notes stay near each other.
type Fretboard struct {
	Tuning Tuning
	hint   int
	hintSt int
}

func NewFretboard(t Tuning) *Fretboard { return &Fretboard{Tuning: t, hint: -1, hintSt: -1} }

// Place resolves one pitch and moves the remembered hand position to it.
func (f *Fretboard) Place(midi uint8) (str, fret uint8, ok bool) {
	str, fret, ok = f.Tuning.position(midi, f.hint, f.hintSt)
	if ok {
		f.hint, f.hintSt = int(fret), int(str)
	}
	return str, fret, ok
}

// Reset forgets the hand position.
func (f *Fretboard) Reset() { f.hint, f.hintSt = -1, -1 }

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
