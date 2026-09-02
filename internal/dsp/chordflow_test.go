package dsp

import (
	"testing"

	"github.com/FullFran/riffhero/internal/practice"
)

// strum renders several strings struck together, each slightly after the last,
// the way a pick crosses them. Simultaneous to the sample would be a keyboard,
// not a guitar, and the spread is exactly what a monophonic estimator trips on.
func strum(midis []uint8, n, spreadFrames int) []float32 {
	out := make([]float32, n)
	for i, midi := range midis {
		hz := midiHz(midi)
		offset := i * spreadFrames
		if offset >= n {
			break
		}
		voice := pluck(hz, n-offset, 12)
		for j, v := range voice {
			out[offset+j] += v * 0.4
		}
	}
	return out
}

// chordScore drives a detector over a signal and returns what it emitted.
func chordScore(t *testing.T, det *Detector, signal []float32, block int) []practice.DetectedNote {
	t.Helper()
	var out []practice.DetectedNote
	for i := 0; i < len(signal); i += block {
		end := i + block
		if end > len(signal) {
			end = len(signal)
		}
		det.Write(signal[i:end])
		out = append(out, det.Poll(practice.Frame(end))...)
	}
	// Anything still inside the analysis window needs a few more polls.
	for i := 0; i < 8; i++ {
		out = append(out, det.Poll(practice.Frame(len(signal)+i))...)
	}
	return out
}

func TestDetectorResolvesAPowerChordWhenTheScoreExpectsOne(t *testing.T) {
	// The Phase 5 exit criterion. A monophonic estimator cannot answer this at
	// all: two strings ringing together give it no single period to lock on to,
	// and its quorum rule makes it emit nothing rather than guess. Conditioned
	// on the score, the same audio resolves into the two notes that are there.
	const rate = testSampleRate
	expected := []uint8{40, 47} // E2 and B2, an E5 power chord

	signal := make([]float32, rate)
	copy(signal[rate/10:], strum(expected, rate-rate/10, 200))

	notes := []practice.Note{
		{Start: practice.Frame(rate / 10), Duration: practice.Frame(rate / 2), MIDI: 40, String: 6, Fret: 0},
		{Start: practice.Frame(rate / 10), Duration: practice.Frame(rate / 2), MIDI: 47, String: 5, Fret: 2},
	}

	det := NewDetector(rate)
	det.Expect(notes, practice.Frame(rate/10))

	got := chordScore(t, det, signal, 512)

	found := map[uint8]bool{}
	for _, n := range got {
		found[n.MIDI] = true
	}
	for _, want := range expected {
		if !found[want] {
			t.Fatalf("MIDI %d was never detected; got %+v", want, got)
		}
	}
}

func TestExpectedChordScoresThroughTheSession(t *testing.T) {
	// End to end: synthetic strum, real detector, real scoring session. This is
	// what "power chords score reliably enough for practice" has to mean.
	const rate = testSampleRate
	clock := practice.Clock{SampleRate: rate}

	notes := []practice.Note{
		{Start: practice.Frame(rate / 10), Duration: practice.Frame(rate / 2), MIDI: 40, String: 6, Fret: 0},
		{Start: practice.Frame(rate / 10), Duration: practice.Frame(rate / 2), MIDI: 47, String: 5, Fret: 2},
	}

	signal := make([]float32, rate)
	copy(signal[rate/10:], strum([]uint8{40, 47}, rate-rate/10, 200))

	windows := practice.TimingWindows{Perfect: clock.Frames(0.05), Good: clock.Frames(0.12)}
	session := practice.NewSession(notes, practice.SessionConfig{Windows: windows, MaxCents: 40})

	det := NewDetector(rate)
	det.Expect(notes, windows.Good)

	for _, d := range chordScore(t, det, signal, 512) {
		session.Feed(d)
	}
	session.Advance(practice.Frame(len(signal)))

	st := session.Stats()
	if st.Perfect+st.Good != 2 {
		t.Fatalf("scored %d hits out of 2 (perfect %d good %d miss %d)", st.Perfect+st.Good, st.Perfect, st.Good, st.Miss)
	}
}

func TestExpectOnlyEngagesForSimultaneousNotes(t *testing.T) {
	// A single expected note must keep the monophonic path and its much
	// shorter latency; only a written chord is worth the long window.
	const rate = testSampleRate
	notes := []practice.Note{
		{Start: 1000, Duration: 4800, MIDI: 40},
		{Start: 40000, Duration: 4800, MIDI: 45},
	}

	det := NewDetector(rate)
	det.Expect(notes, 2400)

	if got := det.tracker.Expect(1000); got != nil {
		t.Fatalf("a lone note asked for chord verification: %v", got)
	}
	if got := det.tracker.Expect(500000); got != nil {
		t.Fatalf("a position with no written event asked for chord verification: %v", got)
	}
}

func TestExpectReportsTheChordAtAnAttack(t *testing.T) {
	const rate = testSampleRate
	notes := []practice.Note{
		{Start: 1000, Duration: 4800, MIDI: 40},
		{Start: 1100, Duration: 4800, MIDI: 47},
		{Start: 1200, Duration: 4800, MIDI: 52},
	}

	det := NewDetector(rate)
	det.Expect(notes, 2400)

	got := det.tracker.Expect(1050)
	if len(got) != 3 {
		t.Fatalf("got %v, want three pitches", got)
	}
	// Far enough away and the event no longer applies.
	if got := det.tracker.Expect(1000 + 4000); got != nil {
		t.Fatalf("an attack outside the tolerance matched anyway: %v", got)
	}
}

func TestClearExpectRestoresMonophonicDetection(t *testing.T) {
	det := NewDetector(testSampleRate)
	det.Expect([]practice.Note{{Start: 0, Duration: 100, MIDI: 40}, {Start: 0, Duration: 100, MIDI: 47}}, 2400)
	det.ClearExpect()
	if det.tracker.Expect != nil {
		t.Fatal("ClearExpect left the hook installed")
	}
}
