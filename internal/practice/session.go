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
//
// The detection itself comes back too. The view needs to show what was heard,
// not only what it was worth, and a wrong note that scored nothing is exactly
// the case where the player most needs to see the pitch.
type FeedResult struct {
	Match
	Index    int
	Detected DetectedNote
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

	// since is the earliest position the playhead is known to have played
	// through continuously. Notes whose window closed before it were never put
	// in front of the player, so they are not misses.
	since Frame
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
		return FeedResult{Match: Match{Rating: Miss}, Index: -1, Detected: d}
	}

	m := MatchSingle(s.notes[idx], d, s.cfg.Windows, s.cfg.MaxCents)
	s.resolve(idx, m.Rating)
	return FeedResult{Match: m, Index: idx, Detected: d}
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

// Advance expires every unresolved note the playhead has played through.
//
// "Played through" and "is now behind" are not the same thing, and treating
// them as one was a real bug: seeking past an intro, pressing End, or changing
// track mid-song resolved every note jumped over as a miss, destroying the
// scoreboard for something the player was never given a chance to play. The
// caller says where a jump landed with ResumeFrom, and anything whose window
// closed before that is left alone.
func (s *Session) Advance(pos Frame) {
	for i := range s.notes {
		if s.states[i].resolved {
			continue
		}
		deadline := s.notes[i].Start + s.cfg.Windows.Good
		if deadline < s.since {
			continue
		}
		if pos > deadline {
			s.resolve(i, Miss)
		}
	}
}

// ResumeFrom tells the session the playhead arrived at pos without playing the
// music in between — a seek, or a lap of the A-B region.
func (s *Session) ResumeFrom(pos Frame) {
	if pos < 0 {
		pos = 0
	}
	s.since = pos
}

// Since is the position the current stretch of playing began at.
func (s *Session) Since() Frame { return s.since }

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

// Reset clears every resolution so the same notes can be practised again. It
// is what a lap of an A-B loop does: same section, fresh scoreboard.
func (s *Session) Reset() {
	for i := range s.states {
		s.states[i] = noteState{}
	}
	s.stats = SessionStats{Total: len(s.notes)}
	s.last, s.lastSet = Miss, false
	s.since = 0
}

// NextExpected returns the first unresolved note starting at or after from,
// which is the one the UI highlights as what to play now.
func (s *Session) NextExpected(from Frame) (VisibleNote, bool) {
	for i, n := range s.notes {
		if s.states[i].resolved || n.Start < from {
			continue
		}
		return VisibleNote{Index: i, Note: n}, true
	}
	return VisibleNote{}, false
}

// Pending reports how many notes have not resolved yet.
func (s *Session) Pending() int { return s.stats.Total - s.stats.Resolved }

// NotesIn returns the notes starting inside [from,to). Building a Session from
// it is how an A-B loop is scored on its own section rather than on the whole
// song.
func NotesIn(notes []Note, from, to Frame) []Note {
	out := make([]Note, 0, len(notes))
	for _, n := range notes {
		if n.Start >= from && n.Start < to {
			out = append(out, n)
		}
	}
	return out
}
