package dsp

import (
	"testing"

	"github.com/FullFran/riffhero/internal/practice"
)

// scaled returns a copy of a signal at a different amplitude.
func scaled(signal []float32, gain float64) []float32 {
	out := make([]float32, len(signal))
	for i, s := range signal {
		out[i] = float32(float64(s) * gain)
	}
	return out
}

// A quiet low E must be detected at every amplitude above the floor, not at
// some of them. The low string is where a single hop holds less than one cycle,
// so anything that judges level on one hop turns detection into a coin flip
// decided by where the attack lands in the waveform.
func TestLowNoteDetectionDoesNotDependOnWaveformPhase(t *testing.T) {
	base := perform(testSampleRate, []voice{{at: 0, hz: 82.4069}})

	var lost []float64
	for gain := 0.30; gain <= 1.00; gain += 0.025 {
		tr := NewTracker(testSampleRate)
		got := track(tr, scaled(base, gain), 512)
		if len(got) != 1 || got[0].MIDI != 40 {
			lost = append(lost, gain)
		}
	}
	if len(lost) > 0 {
		t.Errorf("low E lost at %d of the amplitudes swept: %.3f", len(lost), lost)
	}
}

// A dropped ring must not shift every later detection. The tracker counts the
// samples it reads; the ring counts the samples the device offered. When those
// diverge the difference has to be put back, or the timeline is wrong for the
// rest of the run and every remaining note scores as a Miss.
func TestDetectorRealignsTheTimelineAfterDroppedSamples(t *testing.T) {
	d := NewDetector(testSampleRate)

	// Fill the ring past capacity without polling, the way a stalled frame
	// would, then carry on normally.
	stall := perform(2*testSampleRate, []voice{{at: 0, hz: 110}})
	for i := 0; i < len(stall); i += 512 {
		end := i + 512
		if end > len(stall) {
			end = len(stall)
		}
		d.Write(stall[i:end])
	}
	if d.Dropped() == 0 {
		t.Fatal("the stall dropped nothing, so this test proves nothing")
	}

	// One note, well after the stall.
	late := perform(testSampleRate, []voice{{at: int(0.5 * testSampleRate), hz: 196}})
	feed(d, late)

	got := d.Poll(practice.Frame(3 * testSampleRate))
	if len(got) == 0 {
		t.Fatal("no notes after the stall")
	}

	// The last note sits half a second into the post-stall audio, which begins
	// once the whole stall buffer has been offered — dropped samples included,
	// because the player played through them.
	wantAt := int64(len(stall)) + int64(0.5*testSampleRate)
	last := got[len(got)-1]
	if diff := int64(last.Onset) - wantAt; diff < -int64(0.05*testSampleRate) || diff > int64(0.05*testSampleRate) {
		t.Errorf("onset at frame %d, want near %d (off by %d frames)", last.Onset, wantAt, diff)
	}
}

// Restarting a run must not score audio the player produced before the restart
// against the new run's opening bars.
func TestDetectorResetDiscardsQueuedAudio(t *testing.T) {
	d := NewDetector(testSampleRate)
	d.Write(perform(testSampleRate, []voice{{at: 0, hz: 110}}))

	d.Reset()

	if got := d.Poll(practice.Frame(testSampleRate)); len(got) != 0 {
		t.Errorf("Poll after Reset returned %d notes from the previous run: %+v", len(got), got)
	}
	if d.Dropped() != 0 {
		t.Errorf("Dropped = %d after Reset, want 0", d.Dropped())
	}
}

// When the two estimators disagree the tracker is supposed to need more
// evidence, not the same amount. Without this the cross-check changes a number
// nobody reads and nothing else.
func TestTrackerNeedsUnanimityWhenEstimatorsDisagree(t *testing.T) {
	tr := NewTracker(testSampleRate)
	if tr.quorum >= tr.confirmations {
		t.Skip("quorum already equals confirmations; disagreement cannot be distinguished")
	}

	agreed := []windowVote{
		{midi: 55, clarity: 0.9, agreed: true},
		{midi: 55, clarity: 0.9, agreed: true},
		{midi: 67, clarity: 0.7, agreed: false},
	}
	if _, ok := tr.decide(0, agreed); !ok {
		t.Error("two agreeing windows were rejected, want accepted")
	}

	split := []windowVote{
		{midi: 55, clarity: 0.4, agreed: false},
		{midi: 55, clarity: 0.4, agreed: false},
		{midi: 67, clarity: 0.4, agreed: false},
	}
	if _, ok := tr.decide(0, split); ok {
		t.Error("two disagreeing windows were accepted, want rejected without unanimity")
	}

	unanimous := []windowVote{
		{midi: 55, clarity: 0.4, agreed: false},
		{midi: 55, clarity: 0.4, agreed: false},
		{midi: 55, clarity: 0.4, agreed: false},
	}
	if _, ok := tr.decide(0, unanimous); !ok {
		t.Error("three disagreeing-but-consistent windows were rejected, want accepted")
	}
}

// A tie between two semitones must resolve the same way every run, or a test
// passes locally and flakes in CI.
func TestTrackerTieBreakIsDeterministic(t *testing.T) {
	tr := NewTracker(testSampleRate)
	tr.confirmations = 4
	tr.quorum = 2

	votes := []windowVote{
		{midi: 55, clarity: 0.70, agreed: true},
		{midi: 67, clarity: 0.90, agreed: true},
		{midi: 55, clarity: 0.70, agreed: true},
		{midi: 67, clarity: 0.90, agreed: true},
	}

	first, ok := tr.decide(0, votes)
	if !ok {
		t.Fatal("a tie was rejected outright")
	}
	for i := 0; i < 200; i++ {
		got, ok := tr.decide(0, votes)
		if !ok || got.MIDI != first.MIDI {
			t.Fatalf("tie resolved to MIDI %d on run %d, want %d every time", got.MIDI, i, first.MIDI)
		}
	}
}
