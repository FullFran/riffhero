package musicxml

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	// maxExpandedMeasures caps the linear timeline a repeat structure may
	// produce. A practice timeline is linear, so repeats are unrolled; a bad
	// times attribute (or a hand-edited file) can therefore ask for millions
	// of measures, and refusing is better than allocating them.
	maxExpandedMeasures = 20000

	// maxExpandSteps caps the walk itself. Measures skipped by an alternate
	// ending produce no output, so an output cap alone cannot be trusted to
	// stop a structure that jumps backwards without ever playing anything.
	maxExpandSteps = 10 * maxExpandedMeasures
)

// measureRepeat is what one measure's barlines say about the order the score
// is played in. endingMask is a bitmask of passes: bit n means "played on pass
// n+1", and zero means the measure is unconditional.
type measureRepeat struct {
	forward    bool
	backward   bool
	times      int
	endingMask uint32
}

// expandRepeats unrolls the measures into the order they are actually played,
// returning indices into each part's Measures slice.
//
// docs/architecture.md is explicit that repeats are expanded at import and
// that a practice timeline is linear; gp/timeline.go already does this for
// Guitar Pro. Until this existed, MusicXML did not, so a chart with a repeat
// sign played half of what the player was reading.
//
// The walk is deliberately the same one gp/timeline.go uses, down to the two
// caps, because the two formats describe the same hazard: nothing stops a file
// from writing a repeat structure that never terminates.
//
// Direction jumps — <sound dacapo="yes">, dalsegno, tocoda, fine — are NOT
// followed, exactly as in gp. Reproducing them faithfully means modelling a
// small state machine whose corner cases differ between engraving programs,
// and getting it subtly wrong silently misaligns every note after the jump. A
// score that uses them is played straight through, which is wrong in an
// obvious, visible way the player can work around by looping the section by
// hand.
func expandRepeats(parts []partXML) ([]int, error) {
	info := repeatStructure(parts)
	if len(info) == 0 {
		return nil, nil
	}

	out := make([]int, 0, len(info))
	index := 0
	repeatStart := 0
	// completed counts how many times the current section has been played to
	// its repeat sign, so the pass currently being played is completed+1.
	completed := 0

	for steps := 0; index < len(info); steps++ {
		if steps > maxExpandSteps {
			return nil, fmt.Errorf("musicxml: the repeat structure does not terminate after %d steps", maxExpandSteps)
		}

		m := info[index]

		if m.forward && repeatStart != index {
			repeatStart = index
			completed = 0
		}

		// An alternate ending belongs to a set of passes; on any other pass
		// the measure is not played at all.
		if m.endingMask != 0 && m.endingMask&(1<<uint(completed)) == 0 {
			index++
			continue
		}

		out = append(out, index)
		if len(out) > maxExpandedMeasures {
			return nil, fmt.Errorf("musicxml: the repeat structure expands past %d measures", maxExpandedMeasures)
		}

		if m.backward {
			// times is the total number of plays, and a repeat sign with no
			// usable times attribute still means "play it twice".
			total := m.times
			if total < 2 {
				total = 2
			}
			completed++
			if completed < total {
				index = repeatStart
				continue
			}
			// The section is finished. Anything after it belongs to a new one,
			// so a later backward repeat with no matching forward sign does
			// not rewind into music that has already been played out.
			completed = 0
			repeatStart = index + 1
		}
		index++
	}
	return out, nil
}

// repeatStructure reduces every part's barlines to one repeat description per
// measure index.
//
// Merging the parts rather than trusting one of them is the safe reading: the
// spec says a repeat has to appear in every part, and exporters that get that
// wrong drop it from the parts nobody looked at rather than from all of them.
// Parts that do agree just set the same flags twice.
func repeatStructure(parts []partXML) []measureRepeat {
	count := 0
	for _, p := range parts {
		if len(p.Measures) > count {
			count = len(p.Measures)
		}
	}
	if count == 0 {
		return nil
	}
	info := make([]measureRepeat, count)

	// active carries a numbered ending forward across the measures it spans.
	// This is the one real difference from GPIF, where every bar carries its
	// own alternate-ending list: MusicXML writes the number only on the
	// bracket's start and stop barlines, so a three-measure first ending has
	// nothing at all on its middle measure.
	var active uint32

	for i := 0; i < count; i++ {
		mask := active
		for _, part := range parts {
			if i >= len(part.Measures) {
				continue
			}
			for _, bl := range part.Measures[i].barlines {
				if r := bl.Repeat; r != nil {
					switch strings.ToLower(strings.TrimSpace(r.Direction)) {
					case "forward":
						info[i].forward = true
					case "backward":
						info[i].backward = true
						if t, err := strconv.Atoi(strings.TrimSpace(r.Times)); err == nil && t > info[i].times {
							info[i].times = t
						}
					}
				}
				if e := bl.Ending; e != nil {
					switch strings.ToLower(strings.TrimSpace(e.Type)) {
					case "start":
						m := endingMask(e.Number)
						mask, active = m, m
					case "stop", "discontinue":
						// The measure carrying the closing bracket is still
						// inside the ending; only what follows it is outside.
						if mask == 0 {
							mask = endingMask(e.Number)
						}
						active = 0
					}
				}
			}
		}
		info[i].endingMask = mask
	}
	return info
}

// endingMask turns an <ending number> attribute into a bitmask of passes.
//
// The attribute is a comma-separated list of positive integers, but engraving
// programs write it with spaces, with trailing periods ("1.") and occasionally
// with a range, so the digits are picked out rather than the string being
// split on one separator. A number nothing can be made of leaves the mask
// empty, which makes the measure unconditional: playing a bracket on every
// pass is a visible mistake, silently dropping it is not.
func endingMask(s string) uint32 {
	var mask uint32
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r < '0' || r > '9' }) {
		n, err := strconv.Atoi(f)
		if err != nil || n < 1 || n > 32 {
			continue
		}
		mask |= 1 << uint(n-1)
	}
	return mask
}
