package practice

import "testing"

func testSession(notes []Note) (*Session, Clock) {
	clock := Clock{SampleRate: 48000}
	return NewSession(notes, SessionConfig{
		Windows: TimingWindows{
			Perfect: clock.Frames(0.050),
			Good:    clock.Frames(0.100),
		},
		MaxCents: 25,
	}), clock
}

func twoNotes(clock Clock) []Note {
	return []Note{
		{Start: clock.Frames(1), Duration: clock.Frames(0.3), MIDI: 60, String: 3, Fret: 5},
		{Start: clock.Frames(2), Duration: clock.Frames(0.3), MIDI: 62, String: 3, Fret: 7},
	}
}

func TestSessionScoresPerfectAndGood(t *testing.T) {
	s, clock := testSession(twoNotes(clock48()))
	notes := twoNotes(clock)

	first := s.Feed(DetectedNote{Onset: notes[0].Start + clock.Frames(0.010), MIDI: 60, CentsError: 5})
	if first.Rating != Perfect || first.Index != 0 {
		t.Fatalf("first=%+v want Perfect on index 0", first)
	}

	second := s.Feed(DetectedNote{Onset: notes[1].Start + clock.Frames(0.080), MIDI: 62})
	if second.Rating != Good || second.Index != 1 {
		t.Fatalf("second=%+v want Good on index 1", second)
	}

	st := s.Stats()
	if st.Perfect != 1 || st.Good != 1 || st.Miss != 0 || st.Resolved != 2 {
		t.Fatalf("stats=%+v", st)
	}
	if st.Combo != 2 || st.MaxCombo != 2 {
		t.Fatalf("combo=%d max=%d want 2/2", st.Combo, st.MaxCombo)
	}
	if st.Accuracy != 1 {
		t.Fatalf("accuracy=%v want 1", st.Accuracy)
	}
}

func TestSessionNeverScoresTheSameNoteTwice(t *testing.T) {
	clock := clock48()
	notes := twoNotes(clock)
	s, _ := testSession(notes)

	hit := DetectedNote{Onset: notes[0].Start, MIDI: 60}
	if got := s.Feed(hit); got.Index != 0 {
		t.Fatalf("first feed did not consume note 0: %+v", got)
	}

	dup := s.Feed(hit)
	if dup.Index != -1 {
		t.Fatalf("duplicate detection consumed note %d", dup.Index)
	}

	st := s.Stats()
	if st.Resolved != 1 || st.Perfect != 1 {
		t.Fatalf("duplicate detection double-scored: %+v", st)
	}
	if st.Extra != 1 {
		t.Fatalf("extra=%d want 1 unmatched detection", st.Extra)
	}
}

func TestSessionWrongPitchDoesNotConsumeTheNote(t *testing.T) {
	clock := clock48()
	notes := twoNotes(clock)
	s, _ := testSession(notes)

	wrong := s.Feed(DetectedNote{Onset: notes[0].Start, MIDI: 61})
	if wrong.Index != -1 || wrong.Rating != Miss {
		t.Fatalf("wrong pitch=%+v want unmatched Miss", wrong)
	}

	retry := s.Feed(DetectedNote{Onset: notes[0].Start + clock.Frames(0.020), MIDI: 60})
	if retry.Index != 0 || retry.Rating != Perfect {
		t.Fatalf("retry=%+v want Perfect on index 0", retry)
	}
	if st := s.Stats(); st.Resolved != 1 || st.Miss != 0 {
		t.Fatalf("stats=%+v want the retry to rescue the note", st)
	}
}

func TestSessionOutOfTuneDetectionIsRejected(t *testing.T) {
	clock := clock48()
	notes := twoNotes(clock)
	s, _ := testSession(notes)

	got := s.Feed(DetectedNote{Onset: notes[0].Start, MIDI: 60, CentsError: 40})
	if got.Index != -1 {
		t.Fatalf("detection 40 cents off consumed note %d", got.Index)
	}
}

func TestSessionAdvanceExpiresLateNotesAsMiss(t *testing.T) {
	clock := clock48()
	notes := twoNotes(clock)
	s, _ := testSession(notes)

	s.Advance(notes[0].Start + clock.Frames(0.100))
	if st := s.Stats(); st.Miss != 0 {
		t.Fatalf("note expired while still inside the Good window: %+v", st)
	}

	s.Advance(notes[0].Start + clock.Frames(0.101))
	st := s.Stats()
	if st.Miss != 1 || st.Resolved != 1 {
		t.Fatalf("stats=%+v want 1 miss", st)
	}
	if st.Combo != 0 {
		t.Fatalf("combo=%d want 0 after a miss", st.Combo)
	}
	if s.LastRating() != Miss {
		t.Fatalf("last rating=%v want Miss", s.LastRating())
	}
}

func TestSessionLateDetectionAfterExpiryIsNotScored(t *testing.T) {
	clock := clock48()
	notes := twoNotes(clock)
	s, _ := testSession(notes)

	s.Advance(notes[0].Start + clock.Frames(0.200))
	got := s.Feed(DetectedNote{Onset: notes[0].Start + clock.Frames(0.200), MIDI: 60})
	if got.Index != -1 {
		t.Fatalf("expired note was re-scored: %+v", got)
	}
	if st := s.Stats(); st.Miss != 1 || st.Perfect != 0 {
		t.Fatalf("stats=%+v", st)
	}
}

func TestSessionBoundaryConditions(t *testing.T) {
	clock := clock48()

	tests := []struct {
		name   string
		offset Frame
		want   Rating
	}{
		{"exactly on perfect edge early", -clock.Frames(0.050), Perfect},
		{"one frame past perfect edge", clock.Frames(0.050) + 1, Good},
		{"exactly on good edge late", clock.Frames(0.100), Good},
		{"one frame past good edge", clock.Frames(0.100) + 1, Miss},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			notes := twoNotes(clock)
			s, _ := testSession(notes)

			got := s.Feed(DetectedNote{Onset: notes[0].Start + tt.offset, MIDI: 60})
			if got.Rating != tt.want {
				t.Fatalf("rating=%v want=%v (offset %d frames)", got.Rating, tt.want, tt.offset)
			}
			if tt.want == Miss && got.Index != -1 {
				t.Fatalf("out-of-window detection consumed note %d", got.Index)
			}
		})
	}
}

func TestSessionAssignsDetectionToNearestUnresolvedNote(t *testing.T) {
	clock := clock48()
	notes := []Note{
		{Start: clock.Frames(1.00), MIDI: 60},
		{Start: clock.Frames(1.06), MIDI: 60},
	}
	s, _ := testSession(notes)

	got := s.Feed(DetectedNote{Onset: clock.Frames(1.05), MIDI: 60})
	if got.Index != 1 {
		t.Fatalf("index=%d want 1 (nearest note)", got.Index)
	}
}

func TestSessionAccuracyCountsHitsOverResolvedNotes(t *testing.T) {
	clock := clock48()
	notes := twoNotes(clock)
	s, _ := testSession(notes)

	s.Feed(DetectedNote{Onset: notes[0].Start, MIDI: 60})
	s.Advance(notes[1].Start + clock.Frames(0.200))

	if st := s.Stats(); st.Accuracy != 0.5 {
		t.Fatalf("accuracy=%v want 0.5 (one hit, one miss)", st.Accuracy)
	}
}

func TestSessionAccuracyIsZeroBeforeAnyNoteResolves(t *testing.T) {
	s, _ := testSession(twoNotes(clock48()))
	if st := s.Stats(); st.Accuracy != 0 || st.Resolved != 0 {
		t.Fatalf("stats=%+v want an empty session", st)
	}
}

func TestSessionUpcomingReturnsVisibleNotes(t *testing.T) {
	clock := clock48()
	notes := twoNotes(clock)
	s, _ := testSession(notes)

	got := s.Upcoming(clock.Frames(0.5), clock.Frames(1.0))
	if len(got) != 1 || got[0].Note.Start != notes[0].Start {
		t.Fatalf("upcoming=%+v want only the first note", got)
	}
	if got[0].Resolved {
		t.Fatal("unplayed note reported as resolved")
	}

	s.Feed(DetectedNote{Onset: notes[0].Start, MIDI: 60})
	got = s.Upcoming(clock.Frames(0.5), clock.Frames(1.0))
	if len(got) != 1 || !got[0].Resolved || got[0].Rating != Perfect {
		t.Fatalf("upcoming after hit=%+v", got)
	}
}

// Phase 0 exit criterion: a synthetic note stream scores a synthetic song
// deterministically, end to end, with no audio hardware involved.
func TestPhase0ExitCriterion(t *testing.T) {
	clock := clock48()
	song := SyntheticSong(clock)

	run := func() SessionStats {
		s := NewSession(song, SessionConfig{
			Windows:  TimingWindows{Perfect: clock.Frames(0.050), Good: clock.Frames(0.100)},
			MaxCents: 25,
		})
		plan := make([]Deviation, len(song))
		for i := range plan {
			switch i % 4 {
			case 0:
				plan[i] = Deviation{Offset: clock.Frames(0.010)}
			case 1:
				plan[i] = Deviation{Offset: clock.Frames(0.080)}
			case 2:
				plan[i] = Deviation{Skip: true}
			case 3:
				plan[i] = Deviation{Offset: -clock.Frames(0.030), Cents: 12}
			}
		}
		det := NewScriptedDetector(Perform(song, plan))

		tr := NewTransport(clock, SongEnd(song)+clock.Frames(1))
		tr.Play()
		for !tr.Finished() {
			tr.AdvanceSeconds(1.0 / 60.0)
			for _, d := range det.Poll(tr.Position()) {
				s.Feed(d)
			}
			s.Advance(tr.Position())
		}
		return s.Stats()
	}

	first := run()
	if first.Resolved != len(song) {
		t.Fatalf("resolved=%d want=%d — the song did not finish scoring", first.Resolved, len(song))
	}
	if first.Miss != 5 {
		t.Fatalf("miss=%d want 5 skipped notes", first.Miss)
	}
	if first.Perfect+first.Good != 15 {
		t.Fatalf("perfect=%d good=%d want 15 hits", first.Perfect, first.Good)
	}

	second := run()
	if first != second {
		t.Fatalf("scoring is not deterministic:\n%+v\n%+v", first, second)
	}
}

func clock48() Clock { return Clock{SampleRate: 48000} }
