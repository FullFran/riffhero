package dsp

import (
	"math"
	"testing"
)

// withPlucks lays decaying plucks onto a silent buffer at the given sample
// positions, which is the closest thing to "someone played four notes" that a
// test can build without a guitar.
func withPlucks(total int, hz float64, at []int) []float32 {
	out := make([]float32, total)
	for _, start := range at {
		tone := pluck(hz, total-start, 8)
		for i, v := range tone {
			out[start+i] += v
		}
	}
	return out
}

// stream feeds a signal through the onset detector one hop at a time and
// returns the sample position of every onset it reported.
func stream(o *Onset, signal []float32) []int64 {
	var got []int64
	hop := o.Hop()
	for i := 0; i+hop <= len(signal); i += hop {
		if at, fired := o.Push(signal[i : i+hop]); fired {
			got = append(got, at)
		}
	}
	return got
}

func TestOnsetIgnoresSilence(t *testing.T) {
	o := NewOnset(testSampleRate)
	if got := stream(o, make([]float32, testSampleRate)); len(got) != 0 {
		t.Errorf("got %d onsets on silence, want 0", len(got))
	}
}

func TestOnsetIgnoresQuietNoise(t *testing.T) {
	o := NewOnset(testSampleRate)
	if got := stream(o, noise(testSampleRate, 0.0008)); len(got) != 0 {
		t.Errorf("got %d onsets on hiss, want 0", len(got))
	}
}

func TestOnsetFiresOnceForOnePluck(t *testing.T) {
	o := NewOnset(testSampleRate)
	got := stream(o, withPlucks(testSampleRate, 196, []int{0}))
	if len(got) != 1 {
		t.Fatalf("got %d onsets, want 1: %v", len(got), got)
	}
	if got[0] > int64(0.02*testSampleRate) {
		t.Errorf("onset at sample %d, want within 20 ms of the attack", got[0])
	}
}

// A note that rings on must not re-trigger every hop, or the tracker would see
// a new note several times a second on one held string.
func TestOnsetDoesNotRetriggerOnSustain(t *testing.T) {
	o := NewOnset(testSampleRate)
	got := stream(o, sine(196, 2*testSampleRate))
	if len(got) != 1 {
		t.Errorf("got %d onsets on a steady tone, want 1: %v", len(got), got)
	}
}

func TestOnsetFindsSeparatePlucks(t *testing.T) {
	positions := []int{0, int(0.35 * testSampleRate), int(0.70 * testSampleRate)}
	o := NewOnset(testSampleRate)
	got := stream(o, withPlucks(testSampleRate, 147, positions))

	if len(got) != len(positions) {
		t.Fatalf("got %d onsets, want %d: %v", len(got), len(positions), got)
	}
	for i, want := range positions {
		if diff := math.Abs(float64(got[i] - int64(want))); diff > 0.025*testSampleRate {
			t.Errorf("onset %d at sample %d, want near %d (%.0f ms off)",
				i, got[i], want, diff/testSampleRate*1000)
		}
	}
}

// Two attacks closer together than the refractory period are one event: a
// guitar cannot be re-picked that fast, so a second trigger is ringing, not
// a new note.
func TestOnsetRefractoryPeriodSuppressesDoubleTrigger(t *testing.T) {
	o := NewOnset(testSampleRate)
	got := stream(o, withPlucks(testSampleRate, 147, []int{0, int(0.008 * testSampleRate)}))
	if len(got) != 1 {
		t.Errorf("got %d onsets for two attacks 8 ms apart, want 1: %v", len(got), got)
	}
}

func TestOnsetPositionsAreAbsoluteAcrossCalls(t *testing.T) {
	o := NewOnset(testSampleRate)
	got := stream(o, withPlucks(2*testSampleRate, 110, []int{int(1.2 * testSampleRate)}))
	if len(got) != 1 {
		t.Fatalf("got %d onsets, want 1: %v", len(got), got)
	}
	if diff := math.Abs(float64(got[0]) - 1.2*testSampleRate); diff > 0.025*testSampleRate {
		t.Errorf("onset at sample %d, want near %d", got[0], int(1.2*testSampleRate))
	}
}

func TestOnsetResetClearsPosition(t *testing.T) {
	o := NewOnset(testSampleRate)
	stream(o, withPlucks(testSampleRate, 110, []int{0}))
	o.Reset()
	got := stream(o, withPlucks(testSampleRate, 110, []int{0}))
	if len(got) != 1 {
		t.Fatalf("got %d onsets after Reset, want 1: %v", len(got), got)
	}
	if got[0] > int64(0.02*testSampleRate) {
		t.Errorf("onset at sample %d after Reset, want near 0", got[0])
	}
}
