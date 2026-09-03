package practice

import "sort"

// Detector is the boundary the DSP layer will implement in Phase 1. Poll
// returns every note detected up to (and including) the given playhead
// position, each event exactly once.
type Detector interface {
	Poll(upTo Frame) []DetectedNote
}

// ScriptedDetector replays a fixed list of detections. It stands in for real
// pitch detection so Phase 0 stays deterministic and hardware-free.
type ScriptedDetector struct {
	events []DetectedNote
	next   int
}

func NewScriptedDetector(events []DetectedNote) *ScriptedDetector {
	sorted := make([]DetectedNote, len(events))
	copy(sorted, events)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Onset < sorted[j].Onset })
	return &ScriptedDetector{events: sorted}
}

func (d *ScriptedDetector) Poll(upTo Frame) []DetectedNote {
	start := d.next
	for d.next < len(d.events) && d.events[d.next].Onset <= upTo {
		d.next++
	}
	if d.next == start {
		return nil
	}
	return d.events[start:d.next]
}

// Reset rewinds the detector so a run can be replayed.
func (d *ScriptedDetector) Reset() { d.next = 0 }

// Deviation describes how a simulated player departs from one expected note.
type Deviation struct {
	Offset    Frame   // timing error in frames; positive is late
	Cents     float64 // intonation error reported by the detector
	Semitones int     // wrong-note error, in semitones
	Skip      bool    // the player did not play this note at all
}

// Perform turns a score into the detections a simulated player would produce.
// A nil or short plan means the remaining notes are played exactly.
func Perform(notes []Note, plan []Deviation) []DetectedNote {
	out := make([]DetectedNote, 0, len(notes))
	for i, n := range notes {
		var dev Deviation
		if i < len(plan) {
			dev = plan[i]
		}
		if dev.Skip {
			continue
		}
		out = append(out, DetectedNote{
			Onset:      n.Start + dev.Offset,
			MIDI:       shiftMIDI(n.MIDI, dev.Semitones),
			CentsError: dev.Cents,
			Confidence: 1,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Onset < out[j].Onset })
	return out
}

func shiftMIDI(midi uint8, semitones int) uint8 {
	v := int(midi) + semitones
	switch {
	case v < 0:
		return 0
	case v > 127:
		return 127
	default:
		return uint8(v)
	}
}
