package midi

import (
	"math"
	"sort"

	"github.com/FullFran/riffhero/internal/practice"
)

// tempoChange is a Set Tempo meta event, positioned in ticks from the start
// of the file. It can legally appear on any track, not just track 0.
type tempoChange struct {
	tick   int64
	micros uint32 // microseconds per quarter note
}

// sigChange is a Time Signature meta event, positioned in ticks from the
// start of the file.
type sigChange struct {
	tick int64
	sig  practice.TimeSignature
}

// defaultMicrosPerQuarter is the SMF default tempo (120 BPM) that applies
// until the first Set Tempo event, and applies for the whole file when there
// is none at all.
const defaultMicrosPerQuarter = 500000

var defaultTimeSignature = practice.TimeSignature{Beats: 4, Unit: 4}

// tempoSegment is one run of constant tempo, carrying the real time already
// elapsed by the tick it starts at. Precomputing that running total is what
// lets tick-to-time conversion jump straight to any tick without replaying
// every earlier tempo change, and it is why a long song with many tempo
// changes does not drift: each tick's time is computed once, directly from
// its segment's exact offset, rather than by accumulating per-note roundings
// on top of one another.
type tempoSegment struct {
	tick      int64
	micros    uint32
	cumMicros float64
}

// buildTempoSegments turns a tick-sorted list of tempo changes into the
// lookup table tickClock uses. changes must already be sorted by tick.
func buildTempoSegments(ticksPerQuarter uint16, changes []tempoChange) []tempoSegment {
	segs := []tempoSegment{{tick: 0, micros: defaultMicrosPerQuarter}}
	rest := changes
	if len(rest) > 0 && rest[0].tick == 0 {
		segs[0].micros = rest[0].micros
		rest = rest[1:]
	}
	for _, c := range rest {
		prev := segs[len(segs)-1]
		microsPerTick := float64(prev.micros) / float64(ticksPerQuarter)
		segs = append(segs, tempoSegment{
			tick:      c.tick,
			micros:    c.micros,
			cumMicros: prev.cumMicros + float64(c.tick-prev.tick)*microsPerTick,
		})
	}
	return segs
}

// tickClock converts SMF ticks to seconds. It hides the two division schemes
// a header can declare: ticks-per-quarter-note, where a tick's real duration
// depends on the tempo map, and SMPTE, where it is a fixed fraction of a
// second and Set Tempo events play no part at all.
type tickClock struct {
	smpte           bool
	ticksPerSecond  float64
	ticksPerQuarter uint16
	segments        []tempoSegment
}

// newTickClock builds the conversion for a header's division word. changes
// must already be sorted by tick.
func newTickClock(division uint16, changes []tempoChange) tickClock {
	if division&0x8000 != 0 {
		// SMPTE division: the high byte is the frame rate stored as a
		// negative two's-complement number (0xE8 == -24 for 24 fps), the low
		// byte is ticks per frame. Their product is ticks per second
		// directly, per the SMF spec — there is no quarter-note concept to
		// convert through in this mode.
		fps := -int8(byte(division >> 8))
		ticksPerFrame := byte(division)
		if fps <= 0 || ticksPerFrame == 0 {
			// A malformed division word; fall back to a plausible constant
			// instead of dividing by zero later.
			return tickClock{smpte: true, ticksPerSecond: 24 * 4}
		}
		return tickClock{smpte: true, ticksPerSecond: float64(fps) * float64(ticksPerFrame)}
	}

	tpq := division
	if tpq == 0 {
		tpq = 480 // malformed header; a common default keeps conversion sane
	}
	return tickClock{ticksPerQuarter: tpq, segments: buildTempoSegments(tpq, changes)}
}

func (c tickClock) seconds(tick int64) float64 {
	if c.smpte {
		return float64(tick) / c.ticksPerSecond
	}
	segs := c.segments
	// Last segment whose tick is <= the target: binary search for the first
	// segment starting after it, then step back one.
	i := sort.Search(len(segs), func(i int) bool { return segs[i].tick > tick }) - 1
	if i < 0 {
		i = 0
	}
	seg := segs[i]
	microsPerTick := float64(seg.micros) / float64(c.ticksPerQuarter)
	return (seg.cumMicros + float64(tick-seg.tick)*microsPerTick) / 1e6
}

func (c tickClock) frame(clock practice.Clock, tick int64) practice.Frame {
	return clock.Frames(c.seconds(tick))
}

// tempoAt returns the microseconds-per-quarter-note in effect at tick, from a
// slice already sorted by tick.
func tempoAt(changes []tempoChange, tick int64) uint32 {
	micros := uint32(defaultMicrosPerQuarter)
	for _, c := range changes {
		if c.tick > tick {
			break
		}
		micros = c.micros
	}
	return micros
}

// sigAt returns the time signature in effect at tick, from a slice already
// sorted by tick.
func sigAt(changes []sigChange, tick int64) practice.TimeSignature {
	sig := defaultTimeSignature
	for _, c := range changes {
		if c.tick > tick {
			break
		}
		sig = c.sig
	}
	return sig
}

// buildSections turns the song's tempo and time-signature maps into the runs
// of bars practice.BuildGrid expects. A new section starts at every tick
// where either map changes, and the final one is stretched to songEndTick so
// the grid always covers the last note, rounding its bar count up rather
// than down — a grid that stops one bar short of the last note is useless
// for A-B looping or the HUD position readout, while a grid that runs a bar
// long past it is merely unused space.
func buildSections(clock practice.Clock, tc tickClock, tempoChanges []tempoChange, sigChanges []sigChange, songEndTick int64) []practice.Section {
	breakpoints := []int64{0}
	seen := map[int64]bool{0: true}
	for _, c := range tempoChanges {
		if !seen[c.tick] {
			seen[c.tick] = true
			breakpoints = append(breakpoints, c.tick)
		}
	}
	for _, c := range sigChanges {
		if !seen[c.tick] {
			seen[c.tick] = true
			breakpoints = append(breakpoints, c.tick)
		}
	}
	sort.Slice(breakpoints, func(i, j int) bool { return breakpoints[i] < breakpoints[j] })

	sections := make([]practice.Section, 0, len(breakpoints))
	for i, tick := range breakpoints {
		endTick := songEndTick
		if i+1 < len(breakpoints) {
			endTick = breakpoints[i+1]
		}

		bpm := 60000000.0 / float64(tempoAt(tempoChanges, tick))
		sig := sigAt(sigChanges, tick)

		startFrame := tc.frame(clock, tick)
		endFrame := tc.frame(clock, endTick)
		duration := endFrame - startFrame

		beatSeconds := 60.0 / bpm * 4.0 / float64(sig.Unit)
		barFrames := clock.Frames(beatSeconds * float64(sig.Beats))

		var bars int
		switch {
		case barFrames <= 0:
			// A degenerate clock (SampleRate <= 0): leave bars at 0 so
			// BuildGrid skips the section instead of dividing by zero.
		case duration > 0:
			bars = int(math.Ceil(float64(duration) / float64(barFrames)))
		default:
			bars = 1
		}

		sections = append(sections, practice.Section{BPM: bpm, Sig: sig, Bars: bars})
	}
	return sections
}
