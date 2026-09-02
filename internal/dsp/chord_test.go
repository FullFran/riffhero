package dsp

import (
	"math"
	"testing"
)

// Voicings the verifier has to hold, written the way a player fingers them.
var (
	// E5 power chord: two strings, and the one case where the fundamentals are
	// so low that only the harmonics can decide.
	powerChordE5 = []uint8{40, 47} // E2 B2

	// Open E major, all six strings. Three of the six pitches are octaves of
	// each other, which is the hardest thing a magnitude spectrum is asked to
	// do here.
	openEMajor = []uint8{40, 47, 52, 56, 59, 64} // E2 B2 E3 G#3 B3 E4

	// F major barre at the first fret, all six strings.
	barreFMajor = []uint8{41, 48, 53, 57, 60, 65} // F2 C3 F3 A3 C4 F4

	// A major barre at the fifth fret, top string muted.
	barreAMajor = []uint8{45, 52, 57, 61, 64} // A2 E3 A3 C#4 E4
)

// chordAudio renders the given pitches struck together as decaying plucks,
// reusing the same synthesis the monophonic tests are built on.
func chordAudio(n int, midis ...uint8) []float32 {
	voices := make([]voice, 0, len(midis))
	for _, m := range midis {
		voices = append(voices, voice{at: 0, hz: midiHz(m)})
	}
	return perform(n, voices)
}

// absentNotes lists the expected pitches the verifier did not find.
func absentNotes(res ChordResult) []uint8 {
	var out []uint8
	for _, n := range res.Notes {
		if !n.Present {
			out = append(out, n.MIDI)
		}
	}
	return out
}

func TestMIDIHzRoundTripsThroughNearestNote(t *testing.T) {
	for midi := uint8(40); midi <= 88; midi++ {
		got, cents := NearestNote(midiHz(midi))
		if got != midi || math.Abs(cents) > 1e-6 {
			t.Fatalf("midiHz(%d) -> NearestNote = %d (%.4f cents), want %d at 0 cents", midi, got, cents, midi)
		}
	}
}

func TestChordVerifierWindowCoversTheLowRegister(t *testing.T) {
	v := NewChordVerifier(testSampleRate)
	binHz := float64(testSampleRate) / float64(v.WindowSize())
	if binHz > chordBinWidthHz {
		t.Errorf("bin width %.2f Hz, want no coarser than %.2f Hz", binHz, chordBinWidthHz)
	}
	if v.WindowSize() != 8192 {
		t.Errorf("WindowSize() = %d, want 8192 at 48 kHz", v.WindowSize())
	}
}

func TestChordVerifierFindsASingleNote(t *testing.T) {
	v := NewChordVerifier(testSampleRate)
	expected := []uint8{45}
	res := v.Verify(chordAudio(v.WindowSize(), 45), expected)

	if !res.Voiced {
		t.Fatal("a plucked A2 was not voiced")
	}
	if res.Found != 1 {
		t.Errorf("Found = %d, want 1 (score %.3f)", res.Found, res.Notes[0].Score)
	}
	if res.Score < 0.7 {
		t.Errorf("Score = %.3f, want at least 0.7", res.Score)
	}
	if res.Unexpected > 0.25 {
		t.Errorf("Unexpected = %.3f for a single expected note that is the only thing sounding", res.Unexpected)
	}
}

// The case the whole design is for. Both fundamentals are below the resolution
// of the transform, so if the harmonic sum did not decide, this would fail.
func TestChordVerifierFindsAPowerChord(t *testing.T) {
	v := NewChordVerifier(testSampleRate)
	res := v.Verify(chordAudio(v.WindowSize(), powerChordE5...), powerChordE5)

	if res.Found != len(powerChordE5) {
		t.Errorf("Found = %d of %d, absent: %v", res.Found, len(powerChordE5), absentNotes(res))
	}
	for _, n := range res.Notes {
		t.Logf("MIDI %d present=%v score=%.3f cents=%+.1f", n.MIDI, n.Present, n.Score, n.Cents)
	}
	if res.Unexpected > 0.25 {
		t.Errorf("Unexpected = %.3f, want low: nothing but the chord is sounding", res.Unexpected)
	}
	if res.Score < 0.7 {
		t.Errorf("Score = %.3f, want at least 0.7", res.Score)
	}
}

func TestChordVerifierFindsAnOpenEMajorVoicing(t *testing.T) {
	v := NewChordVerifier(testSampleRate)
	res := v.Verify(chordAudio(v.WindowSize(), openEMajor...), openEMajor)

	for _, n := range res.Notes {
		t.Logf("MIDI %d present=%v score=%.3f cents=%+.1f", n.MIDI, n.Present, n.Score, n.Cents)
	}
	if res.Found < 5 {
		t.Errorf("Found = %d of %d, absent: %v", res.Found, len(openEMajor), absentNotes(res))
	}
	if res.Score < 0.6 {
		t.Errorf("Score = %.3f, want at least 0.6", res.Score)
	}
	if res.Unexpected > 0.3 {
		t.Errorf("Unexpected = %.3f, want low for the chord that was asked for", res.Unexpected)
	}
}

func TestChordVerifierFindsABarreChord(t *testing.T) {
	v := NewChordVerifier(testSampleRate)
	res := v.Verify(chordAudio(v.WindowSize(), barreFMajor...), barreFMajor)

	for _, n := range res.Notes {
		t.Logf("MIDI %d present=%v score=%.3f cents=%+.1f", n.MIDI, n.Present, n.Score, n.Cents)
	}
	if res.Found < 5 {
		t.Errorf("Found = %d of %d, absent: %v", res.Found, len(barreFMajor), absentNotes(res))
	}
	if res.Score < 0.6 {
		t.Errorf("Score = %.3f, want at least 0.6", res.Score)
	}
}

// Playing a different chord has to be told apart from playing the right one,
// and by a margin wide enough that a threshold sits comfortably between them.
func TestChordVerifierScoresTheWrongChordFarLower(t *testing.T) {
	v := NewChordVerifier(testSampleRate)
	n := v.WindowSize()

	right := v.Verify(chordAudio(n, barreAMajor...), barreAMajor)
	rightScore, rightUnexpected, rightFound := right.Score, right.Unexpected, right.Found

	wrong := v.Verify(chordAudio(n, openEMajor...), barreAMajor)

	t.Logf("A major expected, A major played: score=%.3f found=%d/%d unexpected=%.3f",
		rightScore, rightFound, len(barreAMajor), rightUnexpected)
	t.Logf("A major expected, E major played: score=%.3f found=%d/%d unexpected=%.3f",
		wrong.Score, wrong.Found, len(barreAMajor), wrong.Unexpected)
	t.Logf("separation: %.3f", rightScore-wrong.Score)

	if rightScore < 0.6 {
		t.Fatalf("the right chord only scored %.3f; there is nothing to separate from", rightScore)
	}
	if wrong.Score > rightScore-0.3 {
		t.Errorf("wrong chord scored %.3f against the right chord's %.3f; want a gap of at least 0.3", wrong.Score, rightScore)
	}
	if wrong.Unexpected < 0.3 {
		t.Errorf("Unexpected = %.3f for a chord that shares only two pitches with the expected one, want high", wrong.Unexpected)
	}
	if wrong.Unexpected <= rightUnexpected {
		t.Errorf("Unexpected did not rise: right %.3f, wrong %.3f", rightUnexpected, wrong.Unexpected)
	}
}

// A wrong string left ringing over the right chord is not the right chord.
// Every expected pitch is still there, so only the unexpected-energy term can
// notice.
func TestChordVerifierPenalizesAStringThatShouldNotBeRinging(t *testing.T) {
	v := NewChordVerifier(testSampleRate)
	n := v.WindowSize()

	clean := v.Verify(chordAudio(n, powerChordE5...), powerChordE5)
	cleanScore, cleanUnexpected := clean.Score, clean.Unexpected

	// G3 over an E5 power chord: an open third string caught by the pick.
	fouled := v.Verify(chordAudio(n, 40, 47, 55), powerChordE5)

	t.Logf("clean: score=%.3f unexpected=%.3f", cleanScore, cleanUnexpected)
	t.Logf("fouled: score=%.3f unexpected=%.3f", fouled.Score, fouled.Unexpected)

	if fouled.Found != len(powerChordE5) {
		t.Errorf("Found = %d, want %d: the expected pitches are still sounding", fouled.Found, len(powerChordE5))
	}
	if fouled.Unexpected <= cleanUnexpected+0.05 {
		t.Errorf("Unexpected barely moved: clean %.3f, fouled %.3f", cleanUnexpected, fouled.Unexpected)
	}
	if fouled.Score >= cleanScore {
		t.Errorf("Score did not fall: clean %.3f, fouled %.3f", cleanScore, fouled.Score)
	}
}

func TestChordVerifierReportsAMissingPitchAbsent(t *testing.T) {
	v := NewChordVerifier(testSampleRate)
	// The C#4 is not played; everything else in the barre is.
	res := v.Verify(chordAudio(v.WindowSize(), 45, 52, 57, 64), barreAMajor)

	for _, n := range res.Notes {
		t.Logf("MIDI %d present=%v score=%.3f", n.MIDI, n.Present, n.Score)
	}
	for _, n := range res.Notes {
		if n.MIDI == 61 {
			if n.Present {
				t.Errorf("MIDI 61 reported present at score %.3f, but it was never played", n.Score)
			}
			continue
		}
		if !n.Present {
			t.Errorf("MIDI %d reported absent at score %.3f, but it was played", n.MIDI, n.Score)
		}
	}
	if res.Found != len(barreAMajor)-1 {
		t.Errorf("Found = %d, want %d", res.Found, len(barreAMajor)-1)
	}
}

// The octave trap: E3 contains every harmonic E2 predicts except the odd ones,
// so a verifier that summed evidence without asking which harmonics were
// missing would call an E3 a perfectly good E2.
func TestChordVerifierDoesNotMistakeAnOctaveForItsFundamental(t *testing.T) {
	v := NewChordVerifier(testSampleRate)
	n := v.WindowSize()
	expected := []uint8{40}

	real := v.Verify(chordAudio(n, 40), expected)
	realScore := real.Notes[0].Score

	octave := v.Verify(chordAudio(n, 52), expected)
	got := octave.Notes[0]

	t.Logf("E2 expected: E2 played scores %.3f, E3 played scores %.3f", realScore, got.Score)

	if realScore < 0.7 {
		t.Fatalf("an actual E2 only scored %.3f; the comparison below means nothing", realScore)
	}
	if got.Present {
		t.Errorf("E2 reported present at score %.3f with only an E3 sounding", got.Score)
	}
	if got.Score > realScore-0.3 {
		t.Errorf("E3 scored %.3f against a real E2's %.3f; the odd harmonics are not being missed", got.Score, realScore)
	}
}

func TestChordVerifierReportsCentsForADetunedString(t *testing.T) {
	v := NewChordVerifier(testSampleRate)
	detuned := midiHz(45) * math.Exp2(25.0/1200) // A2, 25 cents sharp

	res := v.Verify(perform(v.WindowSize(), []voice{{at: 0, hz: detuned}}), []uint8{45})
	if !res.Notes[0].Present {
		t.Fatalf("a string 25 cents sharp was not recognised at all (score %.3f)", res.Notes[0].Score)
	}
	got := res.Notes[0].Cents
	t.Logf("A2 detuned by +25 cents measured as %+.1f cents", got)
	if math.Abs(got-25) > 15 {
		t.Errorf("Cents = %+.1f, want ~+25", got)
	}
}

func TestChordVerifierReportsSilenceAsUnvoiced(t *testing.T) {
	v := NewChordVerifier(testSampleRate)
	res := v.Verify(make([]float32, v.WindowSize()), openEMajor)

	if res.Voiced {
		t.Error("silence reported as voiced")
	}
	if res.Score != 0 || res.Found != 0 {
		t.Errorf("silence scored %.3f with %d notes found, want 0 and 0", res.Score, res.Found)
	}
	if len(res.Notes) != len(openEMajor) {
		t.Fatalf("Notes has %d entries, want %d even for silence", len(res.Notes), len(openEMajor))
	}
	for _, n := range res.Notes {
		if n.Present || n.Score != 0 {
			t.Errorf("MIDI %d: present=%v score=%.3f, want absent at 0", n.MIDI, n.Present, n.Score)
		}
	}
}

func TestChordVerifierHandlesAnEmptyExpectedSet(t *testing.T) {
	v := NewChordVerifier(testSampleRate)
	res := v.Verify(chordAudio(v.WindowSize(), openEMajor...), nil)

	if !res.Voiced {
		t.Error("a strummed chord was not voiced just because nothing was expected")
	}
	if res.Found != 0 || res.Score != 0 {
		t.Errorf("Found = %d, Score = %.3f, want 0 and 0 when nothing is expected", res.Found, res.Score)
	}
	if res.Unexpected < 0.9 {
		t.Errorf("Unexpected = %.3f, want ~1: no expected pitch explains anything", res.Unexpected)
	}
}

func TestChordVerifierRejectsNoise(t *testing.T) {
	v := NewChordVerifier(testSampleRate)
	res := v.Verify(noise(v.WindowSize(), 0.2), openEMajor)

	if res.Found != 0 {
		t.Errorf("Found = %d in broadband hiss: %v", res.Found, res.Notes)
	}
	if res.Score > 0.2 {
		t.Errorf("Score = %.3f for noise, want near 0", res.Score)
	}
}

// Verify runs once per chord event inside the game loop, so it has to be as
// allocation-free as the monophonic path already is.
func TestChordVerifyAllocatesNothing(t *testing.T) {
	v := NewChordVerifier(testSampleRate)
	buf := chordAudio(v.WindowSize(), openEMajor...)
	v.Verify(buf, openEMajor) // warm up: the scratch grows to the chord size once

	if got := testing.AllocsPerRun(20, func() { v.Verify(buf, openEMajor) }); got != 0 {
		t.Errorf("Verify allocated %.1f times per call, want 0", got)
	}
}

// A chord event is verified once per onset, alongside everything else the
// frame has to do. This is the number that says whether that is affordable.
func BenchmarkChordVerify(b *testing.B) {
	v := NewChordVerifier(testSampleRate)
	buf := chordAudio(v.WindowSize(), openEMajor...)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v.Verify(buf, openEMajor)
	}
}

func BenchmarkChordVerifyPowerChord(b *testing.B) {
	v := NewChordVerifier(testSampleRate)
	buf := chordAudio(v.WindowSize(), powerChordE5...)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v.Verify(buf, powerChordE5)
	}
}
