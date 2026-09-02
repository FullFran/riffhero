package practice

import "testing"

func TestTuningMIDIMatchesTheOldHelper(t *testing.T) {
	// MIDIForStringFret is Phase 0's API and a lot of the score generation
	// still goes through it; the tuning type must agree with it exactly.
	for str := uint8(1); str <= 6; str++ {
		for fret := uint8(0); fret <= 12; fret++ {
			if got, want := StandardTuning.MIDI(str, fret), MIDIForStringFret(str, fret); got != want {
				t.Fatalf("string %d fret %d: tuning says %d, helper says %d", str, fret, got, want)
			}
		}
	}
}

func TestTuningCapoRaisesEveryString(t *testing.T) {
	capoed := StandardTuning
	capoed.Capo = 3

	for str := uint8(1); str <= 6; str++ {
		open := int(StandardTuning.MIDI(str, 0))
		if got := int(capoed.MIDI(str, 0)); got != open+3 {
			t.Fatalf("string %d open with capo 3: got %d, want %d", str, got, open+3)
		}
	}
}

func TestPositionWithoutAHintPrefersTheLowestFret(t *testing.T) {
	// E4 is the open high E, so with no preference it must land there rather
	// than at the fifth fret of the B string.
	str, fret, ok := StandardTuning.Position(64, -1)
	if !ok {
		t.Fatal("E4 must be playable")
	}
	if str != 1 || fret != 0 {
		t.Fatalf("got string %d fret %d, want string 1 fret 0", str, fret)
	}
}

func TestPositionFollowsTheHintIntoTheBox(t *testing.T) {
	// The same pitch, but the hand is already at the fifth fret: the B string
	// position keeps it there instead of sending it back to open position.
	str, fret, ok := StandardTuning.Position(64, 5)
	if !ok {
		t.Fatal("E4 must be playable")
	}
	if str != 2 || fret != 5 {
		t.Fatalf("got string %d fret %d, want string 2 fret 5", str, fret)
	}
}

func TestPositionRejectsWhatTheNeckCannotSound(t *testing.T) {
	for _, midi := range []uint8{20, 30, 39, 120} {
		if _, _, ok := StandardTuning.Position(midi, -1); ok {
			t.Fatalf("MIDI %d should not be playable in standard tuning", midi)
		}
	}
}

func TestPositionResolvesEveryPitchInRange(t *testing.T) {
	lowest := StandardTuning.MIDI(6, 0)
	highest := StandardTuning.MIDI(1, MaxFret)
	for midi := lowest; midi <= highest; midi++ {
		str, fret, ok := StandardTuning.Position(midi, -1)
		if !ok {
			t.Fatalf("MIDI %d is inside the range but did not resolve", midi)
		}
		if got := StandardTuning.MIDI(str, fret); got != midi {
			t.Fatalf("MIDI %d resolved to string %d fret %d, which sounds %d", midi, str, fret, got)
		}
	}
}

func TestFretboardKeepsAPhraseTogether(t *testing.T) {
	// A minor pentatonic run that a hint-free placer would scatter across the
	// neck: every note has an open-position alternative.
	fb := NewFretboard(StandardTuning)
	run := []uint8{45, 48, 50, 53, 55, 58, 60, 63, 64}

	// Open strings are excluded: reaching for one is not a hand movement, and
	// a run that begins on an open string would otherwise look like a jump.
	maxFret, minFret := 0, 99
	for i, midi := range run {
		_, fret, ok := fb.Place(midi)
		if !ok {
			t.Fatalf("note %d (MIDI %d) did not place", i, midi)
		}
		if fret == 0 {
			continue
		}
		if int(fret) > maxFret {
			maxFret = int(fret)
		}
		if int(fret) < minFret {
			minFret = int(fret)
		}
	}
	if span := maxFret - minFret; span > 4 {
		t.Fatalf("phrase spans %d frets (%d..%d); the hand should stay in a box", span, minFret, maxFret)
	}
}

func TestFretboardResetForgetsTheHand(t *testing.T) {
	fb := NewFretboard(StandardTuning)
	fb.Place(60) // parks the hand somewhere high
	fb.Reset()

	str, fret, ok := fb.Place(64)
	if !ok || str != 1 || fret != 0 {
		t.Fatalf("after reset got string %d fret %d ok=%v, want the open high E", str, fret, ok)
	}
}

func TestDropDLowersOnlyTheSixthString(t *testing.T) {
	for str := uint8(1); str <= 5; str++ {
		if DropDTuning.Strings[str-1] != StandardTuning.Strings[str-1] {
			t.Fatalf("drop D changed string %d", str)
		}
	}
	if got, want := DropDTuning.MIDI(6, 0), StandardTuning.MIDI(6, 0)-2; got != want {
		t.Fatalf("drop D low string sounds %d, want %d", got, want)
	}
}

func TestPlaceOrTransposeMovesByOctaves(t *testing.T) {
	fb := NewFretboard(StandardTuning)

	// A bass part, two octaves below the guitar.
	placed, str, fret, ok := fb.PlaceOrTranspose(20)
	if !ok {
		t.Fatal("MIDI 20 should place after transposition")
	}
	if placed != 44 {
		t.Fatalf("MIDI 20 placed as %d, want 44 — two octaves up", placed)
	}
	if got := StandardTuning.MIDI(str, fret); got != placed {
		t.Fatalf("string %d fret %d sounds %d, not the placed %d", str, fret, got, placed)
	}

	// And a piccolo part above the neck.
	placed, str, fret, ok = fb.PlaceOrTranspose(110)
	if !ok || placed != 86 {
		t.Fatalf("MIDI 110 placed as %d ok=%v, want 86", placed, ok)
	}
	if got := StandardTuning.MIDI(str, fret); got != placed {
		t.Fatalf("string %d fret %d sounds %d, not the placed %d", str, fret, got, placed)
	}
}

func TestPlaceOrTransposeLeavesPlayablePitchesAlone(t *testing.T) {
	fb := NewFretboard(StandardTuning)
	for _, midi := range []uint8{40, 55, 64, 88} {
		placed, str, fret, ok := fb.PlaceOrTranspose(midi)
		if !ok || placed != midi {
			t.Fatalf("MIDI %d was moved to %d", midi, placed)
		}
		if got := StandardTuning.MIDI(str, fret); got != midi {
			t.Fatalf("MIDI %d placed at string %d fret %d, which sounds %d", midi, str, fret, got)
		}
	}
}

func TestPlaceOrTransposeUsesTheTrackTuning(t *testing.T) {
	// Drop D reaches two semitones lower, so a note that standard tuning has
	// to move stays where it was written.
	fb := NewFretboard(DropDTuning)
	placed, str, fret, ok := fb.PlaceOrTranspose(38)
	if !ok || placed != 38 {
		t.Fatalf("MIDI 38 placed as %d ok=%v in drop D", placed, ok)
	}
	if got := DropDTuning.MIDI(str, fret); got != 38 {
		t.Fatalf("string %d fret %d sounds %d in drop D", str, fret, got)
	}
}

func TestSoundsChecksATabPositionAgainstAPitch(t *testing.T) {
	if !StandardTuning.Sounds(64, 1, 0) {
		t.Fatal("the open high E should sound MIDI 64")
	}
	if StandardTuning.Sounds(64, 1, 1) {
		t.Fatal("the first fret of the high E is not MIDI 64")
	}
	// A position that does not exist can never sound anything, which is what
	// stops a zero-valued string number reading as string 1.
	if StandardTuning.Sounds(64, 0, 0) || StandardTuning.Sounds(64, 7, 0) || StandardTuning.Sounds(64, 1, 99) {
		t.Fatal("an impossible position should not match")
	}
}

func TestNamedRecognizesThePresets(t *testing.T) {
	for _, want := range []Tuning{StandardTuning, DropDTuning, HalfStepDown} {
		got := Tuning{Name: "Tab", Strings: want.Strings}.Named()
		if got.Name != want.Name {
			t.Fatalf("named %q, want %q", got.Name, want.Name)
		}
	}
}

func TestNamedSpellsOutAnUnknownTuning(t *testing.T) {
	// Low to high, the way a guitarist reads a tuning off a headstock.
	open := StandardTuning
	open.Strings = [6]uint8{64, 61, 57, 52, 45, 40} // open A
	if got := open.Named().Name; got != "E A E A C# E" {
		t.Fatalf("got %q", got)
	}
}

func TestNamedSkipsStringsTheInstrumentDoesNotHave(t *testing.T) {
	// The array is six long because a guitar is. A four-string bass leaves the
	// top two slots empty, and spelling them out would invent two strings.
	bass := Tuning{Strings: [6]uint8{0, 0, 43, 38, 33, 28}}
	if got := bass.Named().Name; got != "E A D G" {
		t.Fatalf("got %q", got)
	}
	if got := (Tuning{}).Named().Name; got != "Unknown" {
		t.Fatalf("an empty tuning named %q", got)
	}
}
