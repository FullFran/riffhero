package practice

// TimeSignature is a bar's meter. Unit is the note value that gets one beat:
// 4 for a quarter, 8 for an eighth.
type TimeSignature struct {
	Beats int
	Unit  int
}

var CommonTime = TimeSignature{Beats: 4, Unit: 4}

func (s TimeSignature) valid() bool { return s.Beats > 0 && s.Unit > 0 }

// Section is a run of bars sharing a tempo and a meter. A song is described as
// a list of them, which covers a constant tempo and a changing one with the
// same type.
type Section struct {
	BPM  float64
	Sig  TimeSignature
	Bars int
}

// Bar is one measure placed on the timeline, with the frame of every beat in
// it. The UI draws these and A-B loop selection snaps to them.
type Bar struct {
	Number int // 1-based, as a musician counts
	Start  Frame
	End    Frame
	Beats  []Frame
	Sig    TimeSignature
	BPM    float64
}

// Grid is the bar map of a song, ordered and contiguous.
type Grid []Bar

// BuildGrid lays sections out from start. BPM always counts quarter notes, so
// a beat lasts 60/BPM * 4/Unit seconds and 6/8 at 120 gives eighth-note beats.
//
// Bar boundaries are computed from the section start rather than accumulated
// bar by bar, so rounding to whole frames cannot drift over a long song.
func BuildGrid(clock Clock, start Frame, sections []Section) Grid {
	var grid Grid
	number := 1
	at := start

	for _, sec := range sections {
		if sec.Bars <= 0 || sec.BPM <= 0 || !sec.Sig.valid() {
			continue
		}
		beatSeconds := 60.0 / sec.BPM * 4.0 / float64(sec.Sig.Unit)
		sectionStart := at

		for b := 0; b < sec.Bars; b++ {
			beatIndex := b * sec.Sig.Beats
			barStart := sectionStart + clock.Frames(float64(beatIndex)*beatSeconds)
			barEnd := sectionStart + clock.Frames(float64(beatIndex+sec.Sig.Beats)*beatSeconds)

			beats := make([]Frame, sec.Sig.Beats)
			for i := range beats {
				beats[i] = sectionStart + clock.Frames(float64(beatIndex+i)*beatSeconds)
			}

			grid = append(grid, Bar{
				Number: number,
				Start:  barStart,
				End:    barEnd,
				Beats:  beats,
				Sig:    sec.Sig,
				BPM:    sec.BPM,
			})
			number++
			at = barEnd
		}
	}
	return grid
}

// End is the frame the last bar stops at.
func (g Grid) End() Frame {
	if len(g) == 0 {
		return 0
	}
	return g[len(g)-1].End
}

// BarAt returns the index of the bar containing f, or -1 when f falls outside
// the grid. Positions before the first bar belong to no bar: a lead-in is not
// part of the music.
func (g Grid) BarAt(f Frame) int {
	lo, hi := 0, len(g)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		switch {
		case f < g[mid].Start:
			hi = mid - 1
		case f >= g[mid].End:
			lo = mid + 1
		default:
			return mid
		}
	}
	return -1
}

// Span returns the frame range covering bars from..to inclusive, by index.
// Indices are clamped and may be given in either order.
func (g Grid) Span(from, to int) (Frame, Frame) {
	if len(g) == 0 {
		return 0, 0
	}
	if from > to {
		from, to = to, from
	}
	from = clampIndex(from, len(g))
	to = clampIndex(to, len(g))
	return g[from].Start, g[to].End
}

// Snap returns the beat frame nearest to f, or f itself when the grid is empty.
func (g Grid) Snap(f Frame) Frame {
	best, found := Frame(0), false
	for _, bar := range g {
		for _, beat := range bar.Beats {
			if !found || absFrame(beat-f) < absFrame(best-f) {
				best, found = beat, true
			}
		}
		// Bars are ordered, so once a bar starts after f and we already have a
		// candidate, nothing later can be nearer.
		if found && bar.Start > f {
			break
		}
	}
	if !found {
		return f
	}
	if end := g.End(); absFrame(end-f) < absFrame(best-f) {
		return end
	}
	return best
}

// Position describes where a frame sits musically, for the HUD.
type Position struct {
	Bar   int // 1-based, 0 when before the first bar
	Beat  int // 1-based within the bar
	Valid bool
}

// Locate resolves a frame to a bar and beat.
func (g Grid) Locate(f Frame) Position {
	i := g.BarAt(f)
	if i < 0 {
		return Position{}
	}
	bar := g[i]
	beat := 1
	for j, at := range bar.Beats {
		if f >= at {
			beat = j + 1
		}
	}
	return Position{Bar: bar.Number, Beat: beat, Valid: true}
}

func clampIndex(i, n int) int {
	switch {
	case i < 0:
		return 0
	case i >= n:
		return n - 1
	default:
		return i
	}
}
