package gp

import (
	"fmt"
	"strings"

	"github.com/FullFran/riffhero/internal/practice"
)

const (
	// defaultBPM is what Guitar Pro itself assumes when a score carries no
	// tempo automation at all.
	defaultBPM = 120.0

	// maxExpandedBars caps the linear timeline a repeat structure may produce.
	// A practice timeline has to be linear, so repeats are unrolled; a bad
	// count attribute (or a hand-edited file) can therefore ask for millions
	// of bars, and refusing is better than allocating them.
	maxExpandedBars = 20000

	// maxExpandSteps caps the walk itself. Bars skipped by an alternate ending
	// produce no output, so an output cap alone would not stop a structure
	// that jumps backwards without ever playing anything.
	maxExpandSteps = 10 * maxExpandedBars
)

var defaultSignature = practice.TimeSignature{Beats: 4, Unit: 4}

// expandRepeats unrolls the master bars into the order they are actually
// played, returning indices into the original MasterBars slice.
//
// Direction jumps (Da Capo, Dal Segno, Coda, Fine) are deliberately NOT
// followed. Reproducing them faithfully means modelling a small state machine
// whose corner cases differ between engraving programs, and getting it subtly
// wrong silently misaligns every note after the jump. A score that uses them
// is played straight through instead, which is wrong in an obvious, visible
// way that the player can work around by looping the section by hand.
func expandRepeats(bars []masterBarNode) ([]int, error) {
	if len(bars) == 0 {
		return nil, fmt.Errorf("gp: the score has no bars")
	}

	out := make([]int, 0, len(bars))
	index := 0
	repeatStart := 0
	// completed counts how many times the current section has been played to
	// its repeat sign, so the pass currently being played is completed+1.
	completed := 0

	for steps := 0; index < len(bars); steps++ {
		if steps > maxExpandSteps {
			return nil, fmt.Errorf("gp: the repeat structure does not terminate after %d steps", maxExpandSteps)
		}

		mb := &bars[index]

		if mb.Repeat != nil && parseBool(mb.Repeat.Start) && repeatStart != index {
			repeatStart = index
			completed = 0
		}

		// An alternate ending belongs to a set of passes; on any other pass the
		// bar is not played at all.
		if mask := alternateEndingMask(mb.AlternateEndings); mask != 0 && mask&(1<<uint(completed)) == 0 {
			index++
			continue
		}

		out = append(out, index)
		if len(out) > maxExpandedBars {
			return nil, fmt.Errorf("gp: the repeat structure expands past %d bars", maxExpandedBars)
		}

		if mb.Repeat != nil && parseBool(mb.Repeat.End) {
			// count is the total number of plays, and a repeat sign with no
			// usable count still means "play it twice".
			total := intOr(mb.Repeat.Count, 2)
			if total < 2 {
				total = 2
			}
			completed++
			if completed < total {
				index = repeatStart
				continue
			}
			// The section is finished. Anything after it belongs to a new one,
			// so a later repeat sign with no matching start does not rewind
			// into music that has already been played out.
			completed = 0
			repeatStart = index + 1
		}
		index++
	}
	return out, nil
}

// alternateEndingMask turns GPIF's alternate-ending list into a bitmask of
// passes. The element holds ending numbers, 1-based and space separated
// ("1", or "1 2" for a bar shared by the first and second endings); bit n of
// the mask therefore means "played on pass n+1".
func alternateEndingMask(s string) uint32 {
	var mask uint32
	for _, f := range strings.Fields(s) {
		n, ok := parseInt(f)
		if !ok || n < 1 || n > 32 {
			continue
		}
		mask |= 1 << uint(n-1)
	}
	return mask
}

// tempoMap resolves the BPM in force at the start of each master bar, by
// original index.
//
// Two simplifications are made here and both are deliberate. A tempo
// automation with a non-zero Position happens partway through its bar; it is
// applied at the bar start instead, because the grid this feeds is a list of
// bars and cannot hold a tempo change inside one. And where a bar carries more
// than one automation the earliest wins, so a bar that accelerates keeps the
// tempo it was entered at rather than the one it ends on.
func tempoMap(mt masterTrackNode, barCount int) map[int]float64 {
	type entry struct {
		position float64
		bpm      float64
	}
	best := make(map[int]entry)

	for _, a := range mt.Automations {
		if !strings.EqualFold(strings.TrimSpace(a.Type), "Tempo") {
			continue
		}
		bar, ok := parseInt(a.Bar)
		if !ok || bar < 0 || bar >= barCount {
			continue
		}
		bpm, ok := tempoValue(a.Value)
		if !ok {
			continue
		}
		pos, ok := parseFloat(a.Position)
		if !ok {
			pos = 0
		}
		if prev, seen := best[bar]; seen && prev.position <= pos {
			continue
		}
		best[bar] = entry{position: pos, bpm: bpm}
	}

	out := make(map[int]float64, len(best))
	for bar, e := range best {
		out[bar] = e.bpm
	}
	return out
}

// tempoValue reads an automation value of the form "<bpm> <unit>".
//
// The unit says which note value the BPM counts: 2 is a quarter note, and
// Guitar Pro also writes 1 (eighth), 3 (dotted quarter) and 5 (half). Only the
// quarter reading is used. Treating the others as quarters is an approximation
// that misplaces a 6/8 score written with dotted-quarter beats, which is worth
// less than the risk of guessing the mapping wrong for every other file.
func tempoValue(s string) (float64, bool) {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return 0, false
	}
	bpm, ok := parseFloat(fields[0])
	if !ok || bpm <= 0 {
		return 0, false
	}
	return bpm, true
}

// signatureMap resolves the meter of every master bar. A bar with no <Time>
// keeps the previous one's, which is how the format avoids repeating it.
func signatureMap(bars []masterBarNode) []practice.TimeSignature {
	out := make([]practice.TimeSignature, len(bars))
	current := defaultSignature
	for i := range bars {
		if sig, ok := parseSignature(bars[i].Time); ok {
			current = sig
		}
		out[i] = current
	}
	return out
}

func parseSignature(s string) (practice.TimeSignature, bool) {
	beats, unit, found := strings.Cut(strings.TrimSpace(s), "/")
	if !found {
		return practice.TimeSignature{}, false
	}
	b, okB := parseInt(beats)
	u, okU := parseInt(unit)
	if !okB || !okU || b <= 0 || u <= 0 {
		return practice.TimeSignature{}, false
	}
	return practice.TimeSignature{Beats: b, Unit: u}, true
}

// timeline is the expanded, linear description of the song: one entry per bar
// actually played, plus the grid those entries produced.
type timeline struct {
	order []int                    // index into MasterBars, per played bar
	bpm   []float64                // tempo of each played bar
	sig   []practice.TimeSignature // meter of each played bar
	grid  practice.Grid
}

// buildTimeline expands the repeats, resolves tempo and meter per played bar,
// and lays the result out on the frame timeline.
func buildTimeline(doc *gpif, clock practice.Clock) (*timeline, error) {
	order, err := expandRepeats(doc.MasterBars)
	if err != nil {
		return nil, err
	}

	tempos := tempoMap(doc.MasterTrack, len(doc.MasterBars))
	sigs := signatureMap(doc.MasterBars)

	tl := &timeline{
		order: order,
		bpm:   make([]float64, len(order)),
		sig:   make([]practice.TimeSignature, len(order)),
	}

	// A repeated bar re-applies its own tempo automation, which is what a
	// player does: the second pass through a rallentando starts at the same
	// speed the first one did.
	current := defaultBPM
	for i, master := range order {
		if bpm, ok := tempos[master]; ok {
			current = bpm
		}
		tl.bpm[i] = current
		tl.sig[i] = sigs[master]
	}

	// BuildGrid takes runs of bars sharing a tempo and a meter, so consecutive
	// identical bars are collapsed into one section.
	var sections []practice.Section
	for i := range order {
		if n := len(sections); n > 0 && sections[n-1].BPM == tl.bpm[i] && sections[n-1].Sig == tl.sig[i] {
			sections[n-1].Bars++
			continue
		}
		sections = append(sections, practice.Section{BPM: tl.bpm[i], Sig: tl.sig[i], Bars: 1})
	}

	tl.grid = practice.BuildGrid(clock, 0, sections)

	// BuildGrid silently drops a section it considers invalid. Every bar index
	// used below indexes the grid directly, so a shorter grid would shift the
	// whole song rather than lose one bar; refuse instead.
	if len(tl.grid) != len(order) {
		return nil, fmt.Errorf("gp: could not lay out %d bars on the timeline (got %d)", len(order), len(tl.grid))
	}
	return tl, nil
}
