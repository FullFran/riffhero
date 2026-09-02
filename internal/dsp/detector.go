package dsp

import (
	"sort"

	"github.com/FullFran/riffhero/internal/practice"
)

// TimeMapper turns a position in the capture stream into a position on the
// practice timeline.
//
// The two are the same number only in the simplest case. The capture stream
// counts every sample the device ever delivered and never goes back; the song
// position jumps at a seek, wraps at an A-B loop boundary, and moves at
// practice speed rather than real time. The audio layer knows how they relate
// because it owns both counters, so it supplies the mapping and the detector
// stays ignorant of loops and seeks.
type TimeMapper interface {
	Lookup(stream int64) (practice.Frame, bool)
}

// staleAfterPolls is how many polls a detected note may wait for the playhead
// before it is thrown away. A note only waits at all when the playhead moved
// backwards under it — a loop wrap or a seek — and replaying it a lap later
// would score the wrong lap.
const staleAfterPolls = 240

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

	// Timeline maps capture-stream positions onto the practice timeline. Left
	// nil the two are taken to be the same, which is what the hardware-free
	// tests rely on.
	Timeline TimeMapper

	ring    *Ring
	tracker *Tracker

	scratch []float32
	ready   []pendingNote // detected, not yet past the playhead

	// Samples the ring dropped that have already been accounted for on the
	// tracker's timeline, and the count Dropped() reports from.
	appliedDrops uint64
	dropBase     uint64
}

// pendingNote is a detection waiting for the playhead to reach it.
type pendingNote struct {
	note practice.DetectedNote
	age  int
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

	sort.SliceStable(d.ready, func(i, j int) bool { return d.ready[i].note.Onset < d.ready[j].note.Onset })

	var out []practice.DetectedNote
	kept := d.ready[:0]
	for _, p := range d.ready {
		switch {
		case p.note.Onset <= upTo:
			out = append(out, p.note)
		case p.age >= staleAfterPolls:
			// The playhead has moved away from this note rather than towards
			// it. Dropping it is the honest outcome: it was played against a
			// part of the song that is no longer under the cursor.
		default:
			p.age++
			kept = append(kept, p)
		}
	}
	d.ready = kept
	return out
}

// collect turns tracker notes into timeline detections.
//
// The latency offset is subtracted on the capture stream, before the mapping,
// because that is where it belongs: it is a property of the audio path, not of
// the song, and applying it afterwards would push a note across a loop
// boundary into the wrong lap.
func (d *Detector) collect(notes []Note) {
	for _, note := range notes {
		onset, ok := d.songPosition(note.At)
		if !ok {
			// Captured while the transport was paused or the renderer was
			// starved: there is no song position to score it against.
			continue
		}

		d.ready = append(d.ready, pendingNote{note: practice.DetectedNote{
			Onset:      onset,
			MIDI:       note.MIDI,
			CentsError: note.Cents,
			Confidence: note.Confidence,
		}})
	}
}

// Level is the current input level in dBFS and Present reports whether the
// gate hears a string sounding. Both are for the input meter: a silent guitar
// and a misrouted input look identical on a scoreboard, and this is what tells
// them apart.
func (d *Detector) Level() float64 { return d.tracker.Level() }

// Present reports whether the gate is open.
func (d *Detector) Present() bool { return d.tracker.Present() }

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

// Expect conditions detection on the score.
//
// Without it every onset is put to a monophonic estimator, which on a strummed
// chord is a coin toss between six strings — the quorum rule makes it fail
// silently rather than guess, so chords simply do not score. With it, an onset
// where the score expects several notes at once is verified spectrally
// instead: the question stops being "what note is this?" and becomes "are
// these notes present?", which a magnitude spectrum can actually answer.
//
// tolerance is how far from a written event an attack may land and still be
// treated as that event, and should be the same window the scoring session
// uses to accept a note. Notes must already be on the practice timeline.
func (d *Detector) Expect(notes []practice.Note, tolerance practice.Frame) {
	events := practice.Events(notes, tolerance)
	if len(events) == 0 {
		d.ClearExpect()
		return
	}

	// One scratch slice, refilled per call. The tracker documents that it reads
	// the result immediately and never retains it, which is what makes reusing
	// this safe and keeps the analysis path allocation-free.
	scratch := make([]uint8, 0, 8)

	d.tracker.Expect = func(at int64) []uint8 {
		song, ok := d.songPosition(at)
		if !ok {
			return nil
		}

		i := sort.Search(len(events), func(i int) bool { return events[i].Start >= song })
		best := -1
		var bestDelta practice.Frame
		for _, c := range [2]int{i - 1, i} {
			if c < 0 || c >= len(events) {
				continue
			}
			delta := events[c].Start - song
			if delta < 0 {
				delta = -delta
			}
			if delta <= tolerance && (best < 0 || delta < bestDelta) {
				best, bestDelta = c, delta
			}
		}
		if best < 0 || len(events[best].Notes) < 2 {
			return nil
		}

		scratch = scratch[:0]
		for _, n := range events[best].Notes {
			scratch = append(scratch, n.MIDI)
		}
		return scratch
	}
}

// ClearExpect returns the detector to unconditioned monophonic detection.
func (d *Detector) ClearExpect() { d.tracker.Expect = nil }

// songPosition maps a capture-stream position onto the practice timeline,
// latency offset included, the same way collect does.
func (d *Detector) songPosition(at int64) (practice.Frame, bool) {
	at -= int64(d.LatencyOffset)
	if at < 0 {
		at = 0
	}
	if d.Timeline == nil {
		return practice.Frame(at), true
	}
	return d.Timeline.Lookup(at)
}
