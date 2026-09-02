package dsp

import (
	"math"
	"testing"
)

// The guitar range the detector has to cover, from the open low E to the top
// of the fretboard.
var guitarRange = []struct {
	name string
	hz   float64
	midi uint8
}{
	{"E2", 82.4069, 40},
	{"A2", 110.0000, 45},
	{"D3", 146.8324, 50},
	{"G3", 195.9977, 55},
	{"B3", 246.9417, 59},
	{"E4", 329.6276, 64},
	{"A4", 440.0000, 69},
	{"E5", 659.2551, 76},
	{"E6", 1318.5102, 88},
}

func TestMPMDetectsPureTonesAcrossGuitarRange(t *testing.T) {
	est := NewMPM(testSampleRate)
	for _, tc := range guitarRange {
		got := est.Detect(sine(tc.hz, est.WindowSize()))
		if !got.Voiced {
			t.Errorf("%s: not voiced", tc.name)
			continue
		}
		if err := math.Abs(centsBetween(got.Hz, tc.hz)); err > 5 {
			t.Errorf("%s: got %.2f Hz, want %.2f Hz (%.1f cents off)", tc.name, got.Hz, tc.hz, err)
		}
	}
}

// A weak fundamental is where naive autocorrelation reports the octave above.
// MPM's key-maximum rule exists precisely to avoid that.
func TestMPMResistsOctaveErrorOnRichTones(t *testing.T) {
	est := NewMPM(testSampleRate)
	for _, tc := range guitarRange[:6] {
		got := est.Detect(pluck(tc.hz, est.WindowSize(), 12))
		if !got.Voiced {
			t.Errorf("%s: not voiced", tc.name)
			continue
		}
		if err := math.Abs(centsBetween(got.Hz, tc.hz)); err > 25 {
			t.Errorf("%s: got %.2f Hz, want %.2f Hz (%.1f cents off)", tc.name, got.Hz, tc.hz, err)
		}
	}
}

func TestMPMReportsUnvoicedOnNoise(t *testing.T) {
	est := NewMPM(testSampleRate)
	got := est.Detect(noise(est.WindowSize(), 0.05))
	if got.Voiced {
		t.Errorf("noise reported as voiced at %.2f Hz (clarity %.2f)", got.Hz, got.Clarity)
	}
}

func TestMPMReportsUnvoicedOnSilence(t *testing.T) {
	est := NewMPM(testSampleRate)
	if got := est.Detect(make([]float32, est.WindowSize())); got.Voiced {
		t.Errorf("silence reported as voiced at %.2f Hz", got.Hz)
	}
}

// Clarity has to separate a clean string from hiss, otherwise the note tracker
// has no way to reject garbage.
func TestMPMClarityIsHigherForToneThanNoise(t *testing.T) {
	est := NewMPM(testSampleRate)
	tone := est.Detect(sine(220, est.WindowSize()))
	hiss := est.Detect(noise(est.WindowSize(), 0.2))
	if tone.Clarity <= hiss.Clarity {
		t.Errorf("clarity did not separate tone from noise: tone %.3f, noise %.3f", tone.Clarity, hiss.Clarity)
	}
}

func TestYINDetectsPureTonesAcrossGuitarRange(t *testing.T) {
	est := NewYIN(testSampleRate)
	for _, tc := range guitarRange {
		got := est.Detect(sine(tc.hz, est.WindowSize()))
		if !got.Voiced {
			t.Errorf("%s: not voiced", tc.name)
			continue
		}
		if err := math.Abs(centsBetween(got.Hz, tc.hz)); err > 5 {
			t.Errorf("%s: got %.2f Hz, want %.2f Hz (%.1f cents off)", tc.name, got.Hz, tc.hz, err)
		}
	}
}

func TestYINReportsUnvoicedOnNoise(t *testing.T) {
	est := NewYIN(testSampleRate)
	if got := est.Detect(noise(est.WindowSize(), 0.05)); got.Voiced {
		t.Errorf("noise reported as voiced at %.2f Hz", got.Hz)
	}
}

// The cross-check exists to catch the case where the two estimators disagree,
// which in practice means one of them took an octave.
func TestEstimatorAgreesWhenBothMethodsConcur(t *testing.T) {
	est := NewEstimator(testSampleRate)
	got := est.Detect(sine(196, est.WindowSize()))
	if !got.Voiced {
		t.Fatal("not voiced")
	}
	if err := math.Abs(centsBetween(got.Hz, 196)); err > 5 {
		t.Errorf("got %.2f Hz, want 196 Hz (%.1f cents off)", got.Hz, err)
	}
	if !got.Agreed {
		t.Error("Agreed = false, want true for a clean tone")
	}
}

func TestEstimatorRejectsSilence(t *testing.T) {
	est := NewEstimator(testSampleRate)
	if got := est.Detect(make([]float32, est.WindowSize())); got.Voiced {
		t.Errorf("silence reported as voiced at %.2f Hz", got.Hz)
	}
}

func TestEstimatorHandlesShortBuffer(t *testing.T) {
	est := NewEstimator(testSampleRate)
	if got := est.Detect(sine(220, 32)); got.Voiced {
		t.Errorf("a buffer too short to hold a period reported voiced at %.2f Hz", got.Hz)
	}
}

func TestNearestNoteConvertsHzToMIDIAndCents(t *testing.T) {
	for _, tc := range guitarRange {
		midi, cents := NearestNote(tc.hz)
		if midi != tc.midi {
			t.Errorf("%s: MIDI = %d, want %d", tc.name, midi, tc.midi)
		}
		if math.Abs(cents) > 1 {
			t.Errorf("%s: cents = %.2f, want ~0", tc.name, cents)
		}
	}
}

func TestNearestNoteReportsSharpAndFlat(t *testing.T) {
	sharp := 440 * math.Pow(2, 30.0/1200) // A4 + 30 cents
	midi, cents := NearestNote(sharp)
	if midi != 69 {
		t.Errorf("MIDI = %d, want 69", midi)
	}
	if math.Abs(cents-30) > 0.5 {
		t.Errorf("cents = %.2f, want 30", cents)
	}

	flat := 440 * math.Pow(2, -40.0/1200) // A4 - 40 cents
	midi, cents = NearestNote(flat)
	if midi != 69 {
		t.Errorf("MIDI = %d, want 69", midi)
	}
	if math.Abs(cents+40) > 0.5 {
		t.Errorf("cents = %.2f, want -40", cents)
	}
}

func TestNearestNoteClampsOutOfRangeInput(t *testing.T) {
	if midi, _ := NearestNote(0); midi != 0 {
		t.Errorf("MIDI for 0 Hz = %d, want 0", midi)
	}
	if midi, _ := NearestNote(-5); midi != 0 {
		t.Errorf("MIDI for negative Hz = %d, want 0", midi)
	}
}
