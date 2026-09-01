package practice

import "math"

// SessionConfig holds the scoring rules of a practice run.
type SessionConfig struct {
	Windows  TimingWindows
	MaxCents float64
}

// SessionStats is the scoreboard the UI renders. It is a value type so a
// snapshot can be compared directly in tests.
type SessionStats struct {
	Total    int
	Resolved int
	Perfect  int
	Good     int
	Miss     int
	Extra    int // detections that matched no expected note
	Combo    int
	MaxCombo int
	Accuracy float64 // hits / resolved, in [0,1]
}

// FeedResult reports what a single detection did to the score. Index is the
// expected note it consumed, or -1 when the detection matched nothing.
type FeedResult struct {
	Match
	Index int
}

// VisibleNote is one expected note plus its current scoring state, for rendering.
type VisibleNote struct {
	Index    int
	Note     Note
	Resolved bool
	Rating   Rating
}

type noteState struct {
	resolved bool
	rating   Rating
}

// Session scores a detected note stream against a fixed score. Each expected
// note resolves exactly once, either by a matching detection or by expiring
// past the Good window.
type Session struct {
	notes  []Note
	cfg    SessionConfig
	states []noteState

	stats SessionStats
	last  Rating
	// lastSet distinguishes "no rating yet" from "the last rating was Miss".
	lastSet bool
}

func NewSession(notes []Note, cfg SessionConfig) *Session {
	own := make([]Note, len(notes))
	copy(own, notes)
	return &Session{
		notes:  own,
		cfg:    cfg,
		states: make([]noteState, len(own)),
		stats:  SessionStats{Total: len(own)},
	}
}

func (s *Session) Notes() []Note { return s.notes }

// LastRating returns the most recent resolution, or Miss before anything resolves.
func (s *Session) LastRating() Rating { return s.last }

// HasRating reports whether any note has resolved yet.
func (s *Session) HasRating() bool { return s.lastSet }

func (s *Session) Stats() SessionStats {
	out := s.stats
	if out.Resolved > 0 {
		out.Accuracy = float64(out.Perfect+out.Good) / float64(out.Resolved)
	}
	return out
}

// Feed scores one detection against the nearest unresolved note it can
// legitimately hit. A detection with the wrong pitch or bad intonation
// consumes nothing, so the player can still rescue the note inside its window.
func (s *Session) Feed(d DetectedNote) FeedResult {
	idx := s.candidate(d)
	if idx < 0 {
		s.stats.Extra++
		return FeedResult{Match: Match{Rating: Miss}, Index: -1}
	}

	m := MatchSingle(s.notes[idx], d, s.cfg.Windows, s.cfg.MaxCents)
	s.resolve(idx, m.Rating)
	return FeedResult{Match: m, Index: idx}
}

// candidate finds the closest unresolved note this detection is allowed to
// score: same pitch, in tune, and inside the Good window.
func (s *Session) candidate(d DetectedNote) int {
	best := -1
	var bestDelta Frame

	for i := range s.notes {
		if s.states[i].resolved {
			continue
		}
		m := MatchSingle(s.notes[i], d, s.cfg.Windows, s.cfg.MaxCents)
		if m.Rating == Miss {
			continue
		}
		delta := absFrame(m.TimingError)
		if best < 0 || delta < bestDelta {
			best, bestDelta = i, delta
		}
	}
	return best
}

// Advance expires every unresolved note the playhead has left behind.
func (s *Session) Advance(pos Frame) {
	for i := range s.notes {
		if s.states[i].resolved {
			continue
		}
		if pos > s.notes[i].Start+s.cfg.Windows.Good {
			s.resolve(i, Miss)
		}
	}
}

func (s *Session) resolve(i int, rating Rating) {
	s.states[i] = noteState{resolved: true, rating: rating}
	s.stats.Resolved++
	s.last, s.lastSet = rating, true

	switch rating {
	case Perfect:
		s.stats.Perfect++
	case Good:
		s.stats.Good++
	default:
		s.stats.Miss++
	}

	if rating == Miss {
		s.stats.Combo = 0
		return
	}
	s.stats.Combo++
	if s.stats.Combo > s.stats.MaxCombo {
		s.stats.MaxCombo = s.stats.Combo
	}
}

// Upcoming returns the notes starting inside [from,to] together with their
// scoring state, for the renderer.
func (s *Session) Upcoming(from, to Frame) []VisibleNote {
	var out []VisibleNote
	for i, n := range s.notes {
		if n.Start < from || n.Start > to {
			continue
		}
		out = append(out, VisibleNote{
			Index:    i,
			Note:     n,
			Resolved: s.states[i].resolved,
			Rating:   s.states[i].rating,
		})
	}
	return out
}

func absFrame(f Frame) Frame { return Frame(math.Abs(float64(f))) }
