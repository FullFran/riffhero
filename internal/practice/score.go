package practice

import "sort"

// Track is one playable part of a song, already normalized into the domain's
// note model. Importers differ wildly in what they carry; by the time anything
// reaches here it is notes on a timeline with a tuning behind them.
type Track struct {
	Name       string
	Instrument string
	Tuning     Tuning
	Notes      []Note
}

// End is the frame the last note of the track stops sounding.
func (t Track) End() Frame { return SongEnd(t.Notes) }

// Song is what every importer produces and the whole app consumes.
type Song struct {
	Title  string
	Artist string
	Source string // where it came from, for the HUD
	Clock  Clock
	Grid   Grid
	Tracks []Track
}

// End is the later of the last note and the last bar, so a song with a written
// rest at the end still plays that rest.
func (s *Song) End() Frame {
	end := s.Grid.End()
	for _, t := range s.Tracks {
		if e := t.End(); e > end {
			end = e
		}
	}
	return end
}

// Normalize sorts every track's notes and drops the unplayable ones, so
// importers can append in whatever order their format hands them over.
func (s *Song) Normalize() {
	for i := range s.Tracks {
		notes := s.Tracks[i].Notes[:0]
		for _, n := range s.Tracks[i].Notes {
			if n.Start < 0 || n.Duration <= 0 || n.MIDI == 0 {
				continue
			}
			notes = append(notes, n)
		}
		sort.SliceStable(notes, func(a, b int) bool {
			if notes[a].Start != notes[b].Start {
				return notes[a].Start < notes[b].Start
			}
			return notes[a].MIDI < notes[b].MIDI
		})
		s.Tracks[i].Notes = notes
	}
}

// GuitarTrack picks the track most likely to be the one the player wants: the
// first whose name or instrument looks like a guitar, else the first with any
// notes at all. It returns -1 for an empty song.
func (s *Song) GuitarTrack() int {
	fallback := -1
	for i, t := range s.Tracks {
		if len(t.Notes) == 0 {
			continue
		}
		if fallback < 0 {
			fallback = i
		}
		if looksLikeGuitar(t.Name) || looksLikeGuitar(t.Instrument) {
			return i
		}
	}
	return fallback
}

func looksLikeGuitar(s string) bool {
	for _, want := range []string{"guitar", "guitarra", "gtr", "chitarra", "gitarre"} {
		if containsFold(s, want) {
			return true
		}
	}
	return false
}

func containsFold(haystack, needle string) bool {
	if len(needle) > len(haystack) {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if equalFold(haystack[i:i+len(needle)], needle) {
			return true
		}
	}
	return false
}

func equalFold(a, b string) bool {
	for i := range a {
		x, y := a[i], b[i]
		if 'A' <= x && x <= 'Z' {
			x += 'a' - 'A'
		}
		if 'A' <= y && y <= 'Z' {
			y += 'a' - 'A'
		}
		if x != y {
			return false
		}
	}
	return true
}

// Events groups simultaneous notes, which is what the chord verifier needs:
// one note at a time is a pitch problem, several at once is a spectral one.
// Notes closer together than tolerance count as struck together.
func Events(notes []Note, tolerance Frame) []Event {
	if len(notes) == 0 {
		return nil
	}
	sorted := make([]Note, len(notes))
	copy(sorted, notes)
	sort.SliceStable(sorted, func(a, b int) bool { return sorted[a].Start < sorted[b].Start })

	var out []Event
	cur := Event{Start: sorted[0].Start, Notes: []Note{sorted[0]}}
	for _, n := range sorted[1:] {
		if n.Start-cur.Start <= tolerance {
			cur.Notes = append(cur.Notes, n)
			continue
		}
		out = append(out, cur)
		cur = Event{Start: n.Start, Notes: []Note{n}}
	}
	return append(out, cur)
}
