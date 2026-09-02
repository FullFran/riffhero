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
