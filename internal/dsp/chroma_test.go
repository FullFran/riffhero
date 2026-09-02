package dsp

import (
	"math"
	"testing"
)

const chromaFFTSize = 8192

// profileOf renders a signal, transforms it and folds it into twelve classes.
func profileOf(signal []float32) []float64 {
	f := NewFFT(chromaFFTSize)
	c := NewChroma(testSampleRate, chromaFFTSize)
	mag := make([]float64, f.Size()/2+1)
	out := make([]float64, 12)
	f.Magnitudes(signal, mag)
	c.Compute(mag, out)
	return out
}

// topClass is the strongest pitch class and the strongest of the other eleven.
func topClass(profile []float64) (best int, runnerUp float64) {
	for i, v := range profile {
		if v > profile[best] {
			best = i
		}
	}
	for i, v := range profile {
		if i != best && v > runnerUp {
			runnerUp = v
		}
	}
	return best, runnerUp
}

func TestPitchClassMapsMIDIOntoTwelveClasses(t *testing.T) {
	for _, tc := range []struct {
		name string
		midi uint8
		want int
	}{
		{"C4", 60, 0},
		{"E2", 40, 4},
		{"B2", 47, 11},
		{"A4", 69, 9},
		{"A2", 45, 9},
		{"G#3", 56, 8},
	} {
		if got := PitchClass(tc.midi); got != tc.want {
			t.Errorf("%s: PitchClass(%d) = %d, want %d", tc.name, tc.midi, got, tc.want)
		}
	}
}

func TestChromaPeaksOnTheClassOfAPureTone(t *testing.T) {
	profile := profileOf(sine(440, chromaFFTSize))

	best, runnerUp := topClass(profile)
	if best != PitchClass(69) {
		t.Fatalf("A440 peaked on class %d, want %d", best, PitchClass(69))
	}
	if profile[best] != 1 {
		t.Errorf("strongest class = %.4f, want exactly 1 after normalization", profile[best])
	}
	if runnerUp > 0.5 {
		t.Errorf("runner-up class reached %.4f; A440 should not be ambiguous", runnerUp)
	}
}

func TestChromaFoldsOctavesOntoOneClass(t *testing.T) {
	for _, hz := range []float64{110, 220, 440, 880} {
		if best, _ := topClass(profileOf(sine(hz, chromaFFTSize))); best != 9 {
			t.Errorf("%.0f Hz peaked on class %d, want 9 (A)", hz, best)
		}
	}
}

// This is the whole reason the profile is harmonic rather than a plain fold.
// A single low E is a full harmonic series: fold each bin into the class of
// its own frequency and the third partial votes B, the fifth votes G#, and one
// string reads as an E major chord. Weighting each bin as somebody's overtone
// piles those votes back onto E.
func TestChromaHarmonicWeightingKeepsAStringOnItsOwnClass(t *testing.T) {
	profile := profileOf(pluck(midiHz(40), chromaFFTSize, 10))

	best, runnerUp := topClass(profile)
	if best != PitchClass(40) {
		t.Fatalf("a plucked E2 peaked on class %d, want %d (E)", best, PitchClass(40))
	}
	if runnerUp > 0.65 {
		t.Errorf("runner-up class reached %.4f; the overtones are still voting for their own classes", runnerUp)
	}
}

func TestChromaIsAllZeroForSilence(t *testing.T) {
	profile := profileOf(make([]float32, chromaFFTSize))
	for i, v := range profile {
		if v != 0 {
			t.Fatalf("class %d = %.6f for silence, want 0", i, v)
		}
	}
}

func TestChromaToleratesAShortOutput(t *testing.T) {
	c := NewChroma(testSampleRate, chromaFFTSize)
	out := []float64{-1, -1}
	c.Compute(make([]float64, chromaFFTSize/2+1), out)
	if out[0] != -1 {
		t.Error("Compute wrote into an output slice too short to hold twelve classes")
	}
}

func TestChromaComputeAllocatesNothing(t *testing.T) {
	f := NewFFT(chromaFFTSize)
	c := NewChroma(testSampleRate, chromaFFTSize)
	mag := make([]float64, f.Size()/2+1)
	out := make([]float64, 12)
	f.Magnitudes(pluck(196, f.Size(), 10), mag)
	c.Compute(mag, out) // warm up

	if got := testing.AllocsPerRun(20, func() { c.Compute(mag, out) }); got != 0 {
		t.Errorf("Compute allocated %.1f times per call, want 0", got)
	}
}

func TestPitchClassOfReportsDeviationFromTheSemitone(t *testing.T) {
	class, dev := pitchClassOf(440)
	if class != 9 || math.Abs(dev) > 1e-9 {
		t.Errorf("440 Hz = class %d dev %.6f, want class 9 dev 0", class, dev)
	}

	// A quarter-tone above A440 sits halfway to A#.
	class, dev = pitchClassOf(440 * math.Exp2(0.25/12))
	if class != 9 || math.Abs(dev-0.25) > 1e-9 {
		t.Errorf("A440 + 25 cents = class %d dev %.6f, want class 9 dev 0.25", class, dev)
	}

	if class, _ := pitchClassOf(0); class != 0 {
		t.Errorf("pitchClassOf(0) = %d, want 0 rather than a NaN class", class)
	}
}
