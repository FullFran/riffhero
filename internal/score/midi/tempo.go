package midi

import (
	"fmt"
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

// maxGridBars caps the number of bars a file may lay out. The count comes
// straight from tick positions the file is free to invent, and every bar costs
// a practice.Bar plus a slice of its beat frames: a 36-byte file declaring one
// tick per quarter note and a Note Off two million ticks later asked for
// 500,000 bars and 205 MiB of heap, and a single maximal variable-length delta
// reaches ~67 million bars. Refusing with the limit in the message is the only
// honest answer — nothing that large is a score anyone is going to practise.
const maxGridBars = 20000

// ticksPerBar is the length of one bar of sig, measured in the file's own
// ticks.
//
// Under a ticks-per-quarter division this is arithmetic on the meter alone: a
// beat is a 1/Unit note, so a bar is Beats * 4/Unit quarter notes and the
// tempo map has no say in it. Under SMPTE division there is no quarter note to
// count — a tick is a fixed slice of a second — so the bar has to be measured
// in real time at the tempo in force and converted back.
func (c tickClock) ticksPerBar(sig practice.TimeSignature, micros uint32) int64 {
	var ticks int64
	if c.smpte {
		bpm := 60000000.0 / float64(micros)
		barSeconds := float64(sig.Beats) * 60.0 / bpm * 4.0 / float64(sig.Unit)
		ticks = int64(barSeconds * c.ticksPerSecond)
	} else {
		ticks = int64(sig.Beats) * 4 * int64(c.ticksPerQuarter) / int64(sig.Unit)
	}
	if ticks < 1 {
		// An exotic meter against a coarse division (5/64 at 24 ticks per
		// quarter) rounds away to nothing. One tick per bar is musically
		// meaningless, but it terminates, and the alternative is a loop that
		// never advances.
		return 1
	}
	return ticks
}

// buildGrid lays the song's bars out on the frame timeline, walking the song
// one bar at a time in ticks and converting every boundary with the very same
// tickClock that places the notes. That shared conversion is the whole point,
// and practice.GridFrom exists to take bars already placed this way.
//
// The previous version cut the song into sections at every tempo and meter
// change, rounded each section's bar count up, and handed the list to
// practice.BuildGrid, which lays sections end to end from frame 0. The exact
// tick position of each section was computed and then thrown away, so a tempo
// change in the middle of a bar moved the grid without moving the notes. On a
// file that slows down halfway through bar 1, the note sitting on the bar-2
// downbeat landed at 180000 frames (3.750 s) while the grid put bar 2 at
// 96000 (2.000 s) — 1.75 s out, and every later bar inherited the gap. The
// bar/beat readout, A-B loop selection (Grid.Span and Grid.Snap both read off
// this grid, so the loop the player asks for is not the bars they get) and
// Song.End were all wrong together.
//
// Walking in ticks also stops tempo changes from manufacturing bars. An
// accelerando exported as one Set Tempo per beat used to turn a 100-bar song
// into a 400-bar grid, because every breakpoint rounded its own fragment of a
// bar up to a whole one. Bars come from the meter now; tempo only decides how
// long each one lasts.
//
// Only the bar boundaries are placed from the tempo map: GridFrom spreads the
// beats evenly inside each bar, which is exact for every bar at one tempo and
// an approximation for the rare bar a tempo change lands inside. Downbeats —
// the thing loops and the bar readout are anchored to — stay exact either way.
func buildGrid(clock practice.Clock, tc tickClock, tempoChanges []tempoChange, sigChanges []sigChange, songEndTick int64) (practice.Grid, error) {
	var specs []practice.BarSpec

	// The grid must reach the end of the song, so a bar is emitted for every
	// tick position strictly before songEndTick and the last one is allowed to
	// overhang: a grid stopping short of the final note is useless for looping
	// and for the HUD, while a grid running a bar long is unused space. A song
	// with nothing in it still gets one bar rather than an empty grid.
	for tick := int64(0); tick < songEndTick || len(specs) == 0; {
		if len(specs) >= maxGridBars {
			return nil, fmt.Errorf("midi: the file lays out more than %d bars; its tick positions are not a score", maxGridBars)
		}

		micros := tempoAt(tempoChanges, tick)
		sig := sigAt(sigChanges, tick)
		barTicks := tc.ticksPerBar(sig, micros)

		specs = append(specs, practice.BarSpec{
			Start: tc.frame(clock, tick),
			End:   tc.frame(clock, tick+barTicks),
			Sig:   sig,
			// The BPM in force at the downbeat. A bar that accelerates through
			// its own length has no single tempo to report, and the downbeat's
			// is the one a player would name.
			BPM: 60000000.0 / float64(micros),
		})
		tick += barTicks
	}
	return practice.GridFrom(specs), nil
}
