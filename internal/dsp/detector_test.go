package dsp

import (
	"testing"

	"github.com/FullFran/riffhero/internal/practice"
)

// Detector must be usable wherever ScriptedDetector is, or Phase 0's tests stop
// being a regression net for Phase 1.
var _ practice.Detector = (*Detector)(nil)

// farBehind is a playhead far enough back that no detection is ever due, so
// feed can drain the ring without releasing notes the test has not asked for.
const farBehind = practice.Frame(-1 << 40)

// feed hands a signal to the detector the way an audio callback would: in
// small blocks, polling as it goes. Dumping more than the ring holds without
// polling would drop samples, which is the ring doing its job, not a bug.
func feed(d *Detector, signal []float32) {
	const block = 512
	for i := 0; i < len(signal); i += block {
		end := i + block
		if end > len(signal) {
			end = len(signal)
		}
		d.Write(signal[i:end])
		d.Poll(farBehind) // drain and analyse without releasing anything yet
	}
}

func TestDetectorPollReturnsNotesUpToThePlayhead(t *testing.T) {
	d := NewDetector(testSampleRate)
	signal := perform(int(1.3*testSampleRate), []voice{
		{at: 0, hz: 110},
		{at: int(0.40 * testSampleRate), hz: 146.8324},
		{at: int(0.80 * testSampleRate), hz: 195.9977},
	})
	feed(d, signal)

	// A note whose onset the playhead has not reached is not due yet.
	if got := d.Poll(practice.Frame(0.2 * testSampleRate)); len(got) != 1 {
		t.Errorf("Poll at 0.2 s returned %d notes, want 1 (only the note at zero)", len(got))
	}

	early := d.Poll(practice.Frame(0.5 * testSampleRate))
	if len(early) != 1 {
		t.Fatalf("Poll at 0.5 s returned %d notes, want 1: %+v", len(early), early)
	}
	if early[0].MIDI != 50 {
		t.Errorf("MIDI = %d, want 50", early[0].MIDI)
	}

	// Already-returned notes must not come back a second time.
	late := d.Poll(practice.Frame(1.3 * testSampleRate))
	if len(late) != 1 {
		t.Fatalf("second Poll returned %d notes, want 1: %+v", len(late), late)
	}
	if late[0].MIDI != 55 {
		t.Errorf("MIDI = %d, want 55", late[0].MIDI)
	}
}

func TestDetectorReportsEachNoteExactlyOnce(t *testing.T) {
	d := NewDetector(testSampleRate)
	feed(d, perform(testSampleRate, []voice{{at: 0, hz: 196}}))

	total := 0
	for p := 0; p <= testSampleRate; p += testSampleRate / 10 {
		total += len(d.Poll(practice.Frame(p)))
	}
	if total != 1 {
		t.Errorf("got %d notes across repeated polls, want 1", total)
	}
}

// The audio callback writes into the ring; the DSP side drains it on Poll.
// Nothing may be lost in that handoff.
func TestDetectorDrainsTheRingAcrossManyWrites(t *testing.T) {
	d := NewDetector(testSampleRate)
	signal := perform(int(1.3*testSampleRate), []voice{
		{at: 0, hz: 110},
		{at: int(0.40 * testSampleRate), hz: 146.8324},
		{at: int(0.80 * testSampleRate), hz: 195.9977},
	})

	got := 0
	for i := 0; i < len(signal); i += 256 {
		end := i + 256
		if end > len(signal) {
			end = len(signal)
		}
		d.Write(signal[i:end])
		got += len(d.Poll(practice.Frame(end)))
	}
	if got != 3 {
		t.Errorf("got %d notes, want 3", got)
	}
	if dropped := d.Dropped(); dropped != 0 {
		t.Errorf("dropped %d samples, want 0", dropped)
	}
}

// Latency calibration shifts detections back onto the timeline the player
// actually heard.
func TestDetectorLatencyOffsetShiftsOnsets(t *testing.T) {
	const offset = 480 // 10 ms at 48 kHz

	plain := NewDetector(testSampleRate)
	feed(plain, perform(testSampleRate, []voice{{at: int(0.3 * testSampleRate), hz: 196}}))
	base := plain.Poll(practice.Frame(testSampleRate))

	shifted := NewDetector(testSampleRate)
	shifted.LatencyOffset = offset
	feed(shifted, perform(testSampleRate, []voice{{at: int(0.3 * testSampleRate), hz: 196}}))
	got := shifted.Poll(practice.Frame(testSampleRate))

	if len(base) != 1 || len(got) != 1 {
		t.Fatalf("got %d and %d notes, want 1 each", len(base), len(got))
	}
	if diff := base[0].Onset - got[0].Onset; diff != offset {
		t.Errorf("offset moved the onset by %d frames, want %d", diff, offset)
	}
}

func TestDetectorCarriesCentsAndConfidence(t *testing.T) {
	d := NewDetector(testSampleRate)
	feed(d, perform(testSampleRate, []voice{{at: 0, hz: 196}}))

	got := d.Poll(practice.Frame(testSampleRate))
	if len(got) != 1 {
		t.Fatalf("got %d notes, want 1", len(got))
	}
	if got[0].Confidence <= 0 || got[0].Confidence > 1 {
		t.Errorf("Confidence = %v, want a value in (0, 1]", got[0].Confidence)
	}
	if got[0].CentsError < -50 || got[0].CentsError > 50 {
		t.Errorf("CentsError = %v, want it within half a semitone", got[0].CentsError)
	}
}

func TestDetectorSilenceProducesNothing(t *testing.T) {
	d := NewDetector(testSampleRate)
	feed(d, make([]float32, testSampleRate))
	if got := d.Poll(practice.Frame(testSampleRate)); len(got) != 0 {
		t.Errorf("got %d notes on silence, want 0", len(got))
	}
}

// A Session must score real detections exactly as it scores scripted ones.
// This is the Phase 1 exit criterion in test form: swap the source of
// detections and the scoring side does not change.
func TestDetectorFeedsTheScoringSession(t *testing.T) {
	clock := practice.Clock{SampleRate: testSampleRate}
	song := []practice.Note{
		{Start: 0, Duration: clock.Frames(0.3), MIDI: 45, String: 5, Fret: 0},
		{Start: clock.Frames(0.40), Duration: clock.Frames(0.3), MIDI: 50, String: 4, Fret: 0},
		{Start: clock.Frames(0.80), Duration: clock.Frames(0.3), MIDI: 55, String: 3, Fret: 0},
	}
	session := practice.NewSession(song, practice.SessionConfig{
		Windows:  practice.TimingWindows{Perfect: clock.Frames(0.050), Good: clock.Frames(0.100)},
		MaxCents: 25,
	})

	d := NewDetector(testSampleRate)
	feed(d, perform(int(1.3*testSampleRate), []voice{
		{at: 0, hz: 110},
		{at: int(0.40 * testSampleRate), hz: 146.8324},
		{at: int(0.80 * testSampleRate), hz: 195.9977},
	}))

	end := practice.Frame(1.3 * testSampleRate)
	for _, n := range d.Poll(end) {
		session.Feed(n)
	}
	session.Advance(end)

	st := session.Stats()
	if st.Perfect != 3 {
		t.Errorf("Perfect = %d, want 3 (good %d, miss %d)", st.Perfect, st.Good, st.Miss)
	}
}
