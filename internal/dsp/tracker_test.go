package dsp

import (
	"math"
	"testing"
)

// voice is one note in a synthetic performance.
type voice struct {
	at int
	hz float64
}

// perform renders a sequence of plucked notes into one buffer.
func perform(total int, voices []voice) []float32 {
	out := make([]float32, total)
	for _, v := range voices {
		if v.at >= total {
			continue
		}
		tone := pluck(v.hz, total-v.at, 8)
		for i, s := range tone {
			out[v.at+i] += s
		}
	}
	return out
}

// track feeds a signal to the tracker in device-sized blocks and collects every
// note it emits.
func track(tr *Tracker, signal []float32, block int) []Note {
	var got []Note
	for i := 0; i < len(signal); i += block {
		end := i + block
		if end > len(signal) {
			end = len(signal)
		}
		got = append(got, tr.Push(signal[i:end])...)
	}
	return got
}

func TestTrackerEmitsNothingOnSilence(t *testing.T) {
	tr := NewTracker(testSampleRate)
	if got := track(tr, make([]float32, testSampleRate), 512); len(got) != 0 {
		t.Errorf("got %d notes on silence, want 0", len(got))
	}
}

func TestTrackerEmitsNothingOnNoise(t *testing.T) {
	tr := NewTracker(testSampleRate)
	if got := track(tr, noise(testSampleRate, 0.0008), 512); len(got) != 0 {
		t.Errorf("got %d notes on hiss, want 0", len(got))
	}
}

func TestTrackerEmitsOneNotePerPluck(t *testing.T) {
	tr := NewTracker(testSampleRate)
	got := track(tr, perform(testSampleRate, []voice{{at: 0, hz: 220}}), 512)
	if len(got) != 1 {
		t.Fatalf("got %d notes, want 1: %+v", len(got), got)
	}
	if got[0].MIDI != 57 { // A3
		t.Errorf("MIDI = %d, want 57", got[0].MIDI)
	}
}

func TestTrackerFollowsASequenceOfNotes(t *testing.T) {
	voices := []voice{
		{at: 0, hz: 110.0000},                          // A2, MIDI 45
		{at: int(0.40 * testSampleRate), hz: 146.8324}, // D3, MIDI 50
		{at: int(0.80 * testSampleRate), hz: 195.9977}, // G3, MIDI 55
		{at: int(1.20 * testSampleRate), hz: 246.9417}, // B3, MIDI 59
	}
	want := []uint8{45, 50, 55, 59}

	tr := NewTracker(testSampleRate)
	got := track(tr, perform(int(1.8*testSampleRate), voices), 512)

	if len(got) != len(want) {
		t.Fatalf("got %d notes, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].MIDI != w {
			t.Errorf("note %d: MIDI = %d, want %d (%.1f cents)", i, got[i].MIDI, w, got[i].Cents)
		}
		if diff := math.Abs(float64(got[i].At - int64(voices[i].at))); diff > 0.03*testSampleRate {
			t.Errorf("note %d: at sample %d, want near %d", i, got[i].At, voices[i].at)
		}
	}
}

// Intonation feedback is the whole point of reporting cents, so a string that
// is audibly sharp has to come back sharp.
func TestTrackerReportsCentsForADetunedString(t *testing.T) {
	const sharp = 30.0
	hz := 220 * math.Pow(2, sharp/1200)

	tr := NewTracker(testSampleRate)
	got := track(tr, perform(testSampleRate, []voice{{at: 0, hz: hz}}), 512)
	if len(got) != 1 {
		t.Fatalf("got %d notes, want 1", len(got))
	}
	if got[0].MIDI != 57 {
		t.Errorf("MIDI = %d, want 57", got[0].MIDI)
	}
	if math.Abs(got[0].Cents-sharp) > 12 {
		t.Errorf("Cents = %.1f, want about %.1f", got[0].Cents, sharp)
	}
}

func TestTrackerConfidenceIsHighForACleanNote(t *testing.T) {
	tr := NewTracker(testSampleRate)
	got := track(tr, perform(testSampleRate, []voice{{at: 0, hz: 196}}), 512)
	if len(got) != 1 {
		t.Fatalf("got %d notes, want 1", len(got))
	}
	if got[0].Confidence < 0.6 {
		t.Errorf("Confidence = %.2f, want >= 0.6 for a clean note", got[0].Confidence)
	}
}

// Latency has to stay inside what a player can tolerate, and it has to be a
// number we know rather than one we hope for.
func TestTrackerLatencyStaysWithinBudget(t *testing.T) {
	tr := NewTracker(testSampleRate)

	const block = 256
	signal := perform(testSampleRate, []voice{{at: 0, hz: 196}})
	var emittedAt int64 = -1
	for i := 0; i < len(signal); i += block {
		end := i + block
		if end > len(signal) {
			end = len(signal)
		}
		if notes := tr.Push(signal[i:end]); len(notes) > 0 {
			emittedAt = int64(end)
			break
		}
	}
	if emittedAt < 0 {
		t.Fatal("no note emitted")
	}

	latency := float64(emittedAt) / testSampleRate * 1000
	if budget := tr.LatencyMillis(); latency > budget+10 {
		t.Errorf("emitted after %.0f ms, over the declared %.0f ms budget", latency, budget)
	}
	if latency > 100 {
		t.Errorf("emitted after %.0f ms, too slow to practise against", latency)
	}
}

func TestTrackerHandlesBlockSizesItDoesNotChoose(t *testing.T) {
	// A device hands over whatever period size it likes, including sizes that
	// do not divide the analysis hop.
	for _, block := range []int{64, 100, 333, 1024} {
		tr := NewTracker(testSampleRate)
		got := track(tr, perform(testSampleRate, []voice{{at: 0, hz: 196}}), block)
		if len(got) != 1 {
			t.Errorf("block %d: got %d notes, want 1", block, len(got))
			continue
		}
		if got[0].MIDI != 55 {
			t.Errorf("block %d: MIDI = %d, want 55", block, got[0].MIDI)
		}
	}
}

func TestTrackerResetClearsState(t *testing.T) {
	tr := NewTracker(testSampleRate)
	track(tr, perform(testSampleRate, []voice{{at: 0, hz: 196}}), 512)
	tr.Reset()

	got := track(tr, perform(testSampleRate, []voice{{at: 0, hz: 196}}), 512)
	if len(got) != 1 {
		t.Fatalf("got %d notes after Reset, want 1", len(got))
	}
	if got[0].At > int64(0.03*testSampleRate) {
		t.Errorf("At = %d after Reset, want near 0", got[0].At)
	}
}
