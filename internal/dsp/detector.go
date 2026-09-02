package dsp

import (
	"sort"

	"github.com/FullFran/riffhero/internal/practice"
)

// defaultRingSeconds is how far behind the DSP side may fall before samples
// start being dropped. Half a second is far more than a healthy frame ever
// needs, and small enough that a stall is noticed rather than hidden.
const defaultRingSeconds = 0.5

// Detector is the Phase 1 implementation of practice.Detector: real audio in,
// scored notes out.
//
// It is the seam between two worlds with very different rules. Write is called
// from the audio callback and does nothing but copy into a lock-free ring —
// no analysis, no allocation, no blocking, per the real-time constraint. Poll
// is called from the game loop and does all the actual work: drain the ring,
// run the tracker, hand back whatever notes the playhead has reached.
//
// Because it satisfies the same interface as ScriptedDetector, the whole of
// Phase 0's scoring stays untouched and keeps working as its regression net.
type Detector struct {
	// LatencyOffset is the round-trip delay of the audio path, in frames.
	// Detections are shifted back by it so a note lands on the timeline where
	// the player heard themselves play it, not where the machine finished
	// hearing it. Phase 2 measures this; until then it is zero.
	LatencyOffset practice.Frame

	ring    *Ring
	tracker *Tracker

	scratch []float32
	ready   []practice.DetectedNote // detected, not yet past the playhead

	// Samples the ring dropped that have already been accounted for on the
	// tracker's timeline, and the count Dropped() reports from.
	appliedDrops uint64
	dropBase     uint64
}

// NewDetector returns a detector for a stream at the given sample rate.
func NewDetector(sampleRate int) *Detector {
	return &Detector{
		ring:    NewRing(int(defaultRingSeconds * float64(sampleRate))),
		tracker: NewTracker(sampleRate),
		scratch: make([]float32, 4096),
	}
}

// Write hands captured samples to the detector. Safe to call from the audio
// callback: it copies and returns, and drops rather than blocks if the DSP
// side has fallen behind.
func (d *Detector) Write(samples []float32) int {
	return d.ring.Write(samples)
}

// Dropped is the number of input samples lost since the last Reset because the
// DSP side could not keep up. Anything other than zero means detections are
// being missed.
func (d *Detector) Dropped() uint64 { return d.ring.Dropped() - d.dropBase }

// LatencyMillis is the analysis delay between a string being struck and the
// note becoming available to Poll.
func (d *Detector) LatencyMillis() float64 { return d.tracker.LatencyMillis() }

// Poll drains whatever audio has arrived, analyses it, and returns the notes
// whose onset the playhead has reached. Each note is returned exactly once.
func (d *Detector) Poll(upTo practice.Frame) []practice.DetectedNote {
	// Snapshot before draining. A full ring drops the newest samples and keeps
	// the oldest, so everything buffered right now precedes the gap.
	dropped := d.ring.Dropped()

	for {
		n := d.ring.Read(d.scratch)
		if n == 0 {
			break
		}
		d.collect(d.tracker.Push(d.scratch[:n]))
	}

	// Put the gap back on the tracker's clock. Without this the tracker counts
	// only the samples it managed to read while the game counts every frame
	// that elapsed, so a single overflow shifts every later detection earlier
	// for the rest of the run and the Session scores all of them as misses.
	//
	// The gap is attributed to the end of what was buffered when it was
	// noticed, so its placement can be out by at most the ring's length.
	if gap := dropped - d.appliedDrops; gap > 0 {
		d.tracker.Skip(int64(gap))
		d.appliedDrops = dropped
	}

	sort.SliceStable(d.ready, func(i, j int) bool { return d.ready[i].Onset < d.ready[j].Onset })

	due := 0
	for due < len(d.ready) && d.ready[due].Onset <= upTo {
		due++
	}
	if due == 0 {
		return nil
	}

	out := make([]practice.DetectedNote, due)
	copy(out, d.ready[:due])
	d.ready = append(d.ready[:0], d.ready[due:]...)
	return out
}

// collect turns tracker notes into timeline detections.
func (d *Detector) collect(notes []Note) {
	for _, note := range notes {
		d.ready = append(d.ready, practice.DetectedNote{
			Onset:      practice.Frame(note.At) - d.LatencyOffset,
			MIDI:       note.MIDI,
			CentsError: note.Cents,
			Confidence: note.Confidence,
		})
	}
}

// Reset clears all analysis state so a run can start over.
//
// The queued audio goes with it. The ring can hold most of a second, and that
// second was played against the previous run — analysing it after a restart
// would score notes from before the restart against the new run's opening bars.
func (d *Detector) Reset() {
	d.ring.Drain()
	d.tracker.Reset()
	d.ready = nil
	d.dropBase = d.ring.Dropped()
	d.appliedDrops = d.ring.Dropped()
}
