package practice

import "testing"

func runnerFixture(t *testing.T) (*Transport, []Note, RunnerConfig) {
	t.Helper()
	clock := testClock()
	song := SyntheticSong(clock)
	tr := NewTransport(clock, SongEnd(song)+clock.Frames(1))
	cfg := RunnerConfig{
		Notes: song,
		Session: SessionConfig{
			Windows:  TimingWindows{Perfect: clock.Frames(0.05), Good: clock.Frames(0.1)},
			MaxCents: 25,
		},
	}
	return tr, song, cfg
}

// run drives a runner to the end of the song the way the game loop would.
func run(tr *Transport, r *Runner, seconds float64) []Update {
	var out []Update
	steps := int(seconds * 60)
	for i := 0; i < steps; i++ {
		tr.AdvanceSeconds(1.0 / 60)
		out = append(out, r.Update())
	}
	return out
}

func TestRunnerScoresAPerfectPerformance(t *testing.T) {
	tr, song, cfg := runnerFixture(t)
	det := NewScriptedDetector(Perform(song, nil))
	r := NewRunner(tr, det, cfg)
	tr.Play()

	run(tr, r, 14)

	st := r.Session().Stats()
	if st.Perfect != len(song) {
		t.Fatalf("scored %d perfect out of %d: %+v", st.Perfect, len(song), st)
	}
	if st.Accuracy != 1 {
		t.Fatalf("accuracy %v, want 1", st.Accuracy)
	}
}

func TestRunnerIsDeterministic(t *testing.T) {
	// The same input twice must produce the same scoreboard; the whole point
	// of a frame clock is that a practice run does not depend on how fast the
	// machine happened to be.
	var runs [2]SessionStats
	for i := range runs {
		tr, song, cfg := runnerFixture(t)
		det := NewScriptedDetector(Perform(song, []Deviation{{Offset: 900}, {Skip: true}, {Cents: 40}}))
		r := NewRunner(tr, det, cfg)
		tr.Play()
		run(tr, r, 14)
		runs[i] = r.Session().Stats()
	}
	if runs[0] != runs[1] {
		t.Fatalf("two identical runs scored differently:\n%+v\n%+v", runs[0], runs[1])
	}
}

func TestRunnerScopesTheScoreboardToTheLoop(t *testing.T) {
	// Scoring the whole song while looping four bars would make accuracy
	// meaningless: every note outside the region would expire as a miss.
	tr, song, cfg := runnerFixture(t)
	det := NewScriptedDetector(nil)
	r := NewRunner(tr, det, cfg)

	from, to := song[4].Start, song[8].Start
	r.SetLoop(Loop{A: from, B: to, Enabled: true})

	if got := len(r.Scope()); got != 4 {
		t.Fatalf("the region holds %d notes, want 4", got)
	}
	if got := r.Session().Stats().Total; got != 4 {
		t.Fatalf("the scoreboard counts %d notes, want 4", got)
	}
	if got := len(r.All()); got != len(song) {
		t.Fatalf("the whole part should still be available for drawing, got %d", got)
	}
}

func TestRunnerClearingTheLoopRestoresTheWholeSong(t *testing.T) {
	tr, song, cfg := runnerFixture(t)
	r := NewRunner(tr, NewScriptedDetector(nil), cfg)

	r.SetLoop(Loop{A: song[4].Start, B: song[8].Start, Enabled: true})
	r.SetLoop(Loop{})

	if got := len(r.Scope()); got != len(song) {
		t.Fatalf("scope is %d notes, want the whole song's %d", got, len(song))
	}
}

func TestRunnerReportsALapAndClearsTheScoreboard(t *testing.T) {
	tr, song, cfg := runnerFixture(t)
	det := NewScriptedDetector(Perform(song, nil))
	r := NewRunner(tr, det, cfg)

	from, to := song[0].Start, song[4].Start
	r.SetLoop(Loop{A: from, B: to, Enabled: true})
	tr.Seek(from)
	tr.Play()

	var laps int
	var lapStats SessionStats
	for _, u := range run(tr, r, 6) {
		if u.LapDone {
			laps++
			lapStats = u.LapStats
		}
	}
	if laps == 0 {
		t.Fatal("no lap was reported")
	}
	if lapStats.Total != 4 {
		t.Fatalf("the lap scoreboard counted %d notes, want 4", lapStats.Total)
	}
	// A fresh lap starts from nothing, otherwise accuracy would be an average
	// over every repetition rather than a verdict on the last one.
	if r.Session().Stats().Resolved > 4 {
		t.Fatalf("the scoreboard carried over between laps: %+v", r.Session().Stats())
	}
}

func TestRunnerAdaptiveRuleSpeedsUpAfterACleanLap(t *testing.T) {
	tr, song, cfg := runnerFixture(t)
	cfg.Adaptive = true
	det := NewScriptedDetector(Perform(song, nil))
	r := NewRunner(tr, det, cfg)

	r.SetLoop(Loop{A: song[0].Start, B: song[4].Start, Enabled: true})
	tr.SetSpeed(0.6)
	tr.Seek(song[0].Start)
	tr.Play()

	// Only the first lap is examined. The scripted detector plays each note
	// once, so a second lap sees silence and the rule correctly slows back
	// down — true of the fixture, not of a guitarist.
	var lap Update
	for i := 0; i < 6*60 && !lap.LapDone; i++ {
		tr.AdvanceSeconds(1.0 / 60)
		lap = r.Update()
	}

	if !lap.LapDone {
		t.Fatal("no lap completed")
	}
	if lap.Adjustment != SpeedUp {
		t.Fatalf("a clean lap (%.0f%% accuracy) decided %v", lap.LapStats.Accuracy*100, lap.Adjustment)
	}
	if tr.Speed() <= 0.6 {
		t.Fatalf("speed is still %v", tr.Speed())
	}
}

func TestRunnerAdaptiveRuleSlowsDownAfterAMessyLap(t *testing.T) {
	tr, song, cfg := runnerFixture(t)
	cfg.Adaptive = true
	// A player who hits nothing at all.
	r := NewRunner(tr, NewScriptedDetector(nil), cfg)

	r.SetLoop(Loop{A: song[0].Start, B: song[4].Start, Enabled: true})
	tr.SetSpeed(0.8)
	tr.Seek(song[0].Start)
	tr.Play()

	run(tr, r, 6)
	if tr.Speed() >= 0.8 {
		t.Fatalf("speed is still %v after missing everything", tr.Speed())
	}
}

func TestRunnerLeavesSpeedAloneWhenNotAdaptive(t *testing.T) {
	tr, song, cfg := runnerFixture(t)
	r := NewRunner(tr, NewScriptedDetector(nil), cfg)

	r.SetLoop(Loop{A: song[0].Start, B: song[4].Start, Enabled: true})
	tr.SetSpeed(0.8)
	tr.Seek(song[0].Start)
	tr.Play()

	run(tr, r, 6)
	if tr.Speed() != 0.8 {
		t.Fatalf("speed changed to %v with the rule switched off", tr.Speed())
	}
}

// recordingExpecter notes what the runner told it to expect.
type recordingExpecter struct {
	calls  int
	notes  []Note
	window Frame
}

func (e *recordingExpecter) Expect(notes []Note, tolerance Frame) {
	e.calls++
	e.notes = notes
	e.window = tolerance
}

func TestRunnerTellsTheDetectorWhatToExpect(t *testing.T) {
	tr, song, cfg := runnerFixture(t)
	exp := &recordingExpecter{}
	cfg.Expecter = exp
	r := NewRunner(tr, NewScriptedDetector(nil), cfg)

	if exp.calls != 1 || len(exp.notes) != len(song) {
		t.Fatalf("after construction: %d calls, %d notes", exp.calls, len(exp.notes))
	}
	if exp.window != cfg.Session.Windows.Good {
		t.Fatalf("tolerance %d, want the Good window %d", exp.window, cfg.Session.Windows.Good)
	}

	r.SetLoop(Loop{A: song[4].Start, B: song[8].Start, Enabled: true})
	if exp.calls != 2 || len(exp.notes) != 4 {
		t.Fatalf("after scoping: %d calls, %d notes", exp.calls, len(exp.notes))
	}
}

func TestRunnerRestartClearsTheScoreboard(t *testing.T) {
	tr, song, cfg := runnerFixture(t)
	r := NewRunner(tr, NewScriptedDetector(Perform(song, nil)), cfg)
	tr.Play()
	run(tr, r, 5)

	if r.Session().Stats().Resolved == 0 {
		t.Fatal("nothing was scored, so the test proves nothing")
	}
	r.Restart()
	if got := r.Session().Stats().Resolved; got != 0 {
		t.Fatalf("%d notes still resolved after a restart", got)
	}
	if tr.Position() != 0 {
		t.Fatalf("the playhead is at %d after a restart", tr.Position())
	}
}
