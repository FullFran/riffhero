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
	strum  Frame
	window Frame
}

func (e *recordingExpecter) Expect(notes []Note, strum, window Frame) {
	e.calls++
	e.notes = notes
	e.strum = strum
	e.window = window
}

func TestRunnerTellsTheDetectorWhatToExpect(t *testing.T) {
	tr, song, cfg := runnerFixture(t)
	exp := &recordingExpecter{}
	cfg.Expecter = exp
	r := NewRunner(tr, NewScriptedDetector(nil), cfg)

	if exp.calls != 1 || len(exp.notes) != len(song) {
		t.Fatalf("after construction: %d calls, %d notes", exp.calls, len(exp.notes))
	}
	// Grouping uses the strum tolerance — at the Good window a sixteenth-note
	// run at 150 BPM would be verified as a series of chords — while matching
	// an attack to a chord uses the player's timing window.
	want := testClock().Frames(DefaultStrumToleranceSeconds)
	if exp.strum != want {
		t.Fatalf("strum %d, want %d", exp.strum, want)
	}
	if exp.window != cfg.Session.Windows.Good {
		t.Fatalf("window %d, want the Good window %d", exp.window, cfg.Session.Windows.Good)
	}
	if exp.strum >= exp.window {
		t.Fatalf("the strum tolerance (%d) must be well under the timing window (%d)",
			exp.strum, exp.window)
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

// loopingDetector replays the same performance every lap, which is what a
// guitarist does and what a ScriptedDetector cannot: it emits each note once
// and then goes silent, so a second lap would score zero for reasons that have
// nothing to do with the code under test.
type loopingDetector struct {
	events []DetectedNote
	loop   Loop
	sent   map[int]bool
	lap    int
}

func newLoopingDetector(events []DetectedNote, loop Loop) *loopingDetector {
	return &loopingDetector{events: events, loop: loop, sent: map[int]bool{}}
}

func (d *loopingDetector) Poll(upTo Frame) []DetectedNote {
	var out []DetectedNote
	for i, e := range d.events {
		if d.sent[i] || e.Onset > upTo {
			continue
		}
		d.sent[i] = true
		out = append(out, e)
	}
	// A wrap is the playhead going backwards, the same signal the audio engine
	// uses; from here it shows up as upTo dropping below what was already sent.
	if d.loop.Active() && upTo < d.loop.A+(d.loop.B-d.loop.A)/4 && len(d.sent) == len(d.events) {
		d.sent = map[int]bool{}
		d.lap++
	}
	return out
}

func TestRunnerEveryLapCanStillScore(t *testing.T) {
	// Multi-lap scoring, which is the whole point of an A-B region. A
	// Transport moves its position and its lap count in the same call, so this
	// cannot reproduce the ordering bug the audio engine had — that one is
	// pinned in internal/audio, where the two came from different threads.
	// What this covers is everything above the playhead: the reset, the
	// rebaseline, and the scoreboard being usable again on the next lap.
	tr, song, cfg := runnerFixture(t)
	loop := Loop{A: song[0].Start, B: song[4].Start, Enabled: true}

	det := newLoopingDetector(Perform(song[:4], nil), loop)
	r := NewRunner(tr, det, cfg)
	r.SetLoop(loop)
	tr.Seek(loop.A)
	tr.Play()

	var laps []SessionStats
	for i := 0; i < 12*60; i++ {
		tr.AdvanceSeconds(1.0 / 60)
		if u := r.Update(); u.LapDone {
			laps = append(laps, u.LapStats)
		}
	}

	if len(laps) < 3 {
		t.Fatalf("only %d laps completed", len(laps))
	}
	for i, st := range laps {
		if st.Accuracy < 1 {
			t.Fatalf("lap %d scored %.0f%% (%+v); a clean performance must score on every lap",
				i+1, st.Accuracy*100, st)
		}
	}
}

func TestRunnerSeekingForwardDoesNotMissTheNotesJumpedOver(t *testing.T) {
	// Right-arrow past an intro, or End, used to resolve every note behind the
	// new position as a miss. The player was never given a chance at them.
	tr, song, cfg := runnerFixture(t)
	r := NewRunner(tr, NewScriptedDetector(nil), cfg)

	r.Seek(song[len(song)-1].Start)
	r.Update()

	if got := r.Session().Stats().Miss; got != 0 {
		t.Fatalf("a seek scored %d misses", got)
	}
	if got := r.Session().Stats().Resolved; got != 0 {
		t.Fatalf("a seek resolved %d notes", got)
	}
}

func TestRunnerStillMissesWhatWasPlayedThrough(t *testing.T) {
	// The other half of the same rule: notes the playhead genuinely passed
	// over, with the player silent, are still misses.
	tr, song, cfg := runnerFixture(t)
	r := NewRunner(tr, NewScriptedDetector(nil), cfg)
	tr.Play()

	run(tr, r, 14)

	st := r.Session().Stats()
	if st.Miss != len(song) {
		t.Fatalf("playing the whole song silently scored %d misses out of %d", st.Miss, len(song))
	}
}

func TestRunnerRestartRebaselines(t *testing.T) {
	// Restart goes back to the start, so nothing behind the playhead should be
	// carried forward as a miss either.
	tr, song, cfg := runnerFixture(t)
	r := NewRunner(tr, NewScriptedDetector(nil), cfg)
	tr.Play()
	run(tr, r, 5)

	r.Restart()
	r.Update()
	if got := r.Session().Stats().Resolved; got != 0 {
		t.Fatalf("%d notes resolved right after a restart", got)
	}
	_ = song
}

func TestSessionResumeFromProtectsOnlyWhatWasSkipped(t *testing.T) {
	clock := testClock()
	notes := []Note{{Start: 1000, Duration: 100, MIDI: 60}, {Start: 100000, Duration: 100, MIDI: 60}}
	s := NewSession(notes, SessionConfig{Windows: TimingWindows{Perfect: 100, Good: clock.Frames(0.1)}})

	// Jumped over: the first note's window closed before the playhead arrived.
	s.ResumeFrom(50000)
	s.Advance(150000)

	st := s.Stats()
	if st.Miss != 1 {
		t.Fatalf("miss %d, want only the note actually played through", st.Miss)
	}
	if s.Since() != 50000 {
		t.Fatalf("since %d", s.Since())
	}
}

func TestFastRunIsNotMistakenForChords(t *testing.T) {
	// Sixteenths at 150 BPM are 100 ms apart, comfortably inside the Good
	// window. Grouping by that window turned the run into a series of
	// two-note chords, each verified over a 171 ms window and stamped at one
	// position.
	clock := testClock()
	step := clock.Frames(0.100)

	notes := make([]Note, 16)
	for i := range notes {
		notes[i] = Note{Start: Frame(i) * step, Duration: step, MIDI: uint8(60 + i%5)}
	}

	tr := NewTransport(clock, SongEnd(notes))
	exp := &recordingExpecter{}
	NewRunner(tr, NewScriptedDetector(nil), RunnerConfig{
		Notes:    notes,
		Session:  SessionConfig{Windows: TimingWindows{Perfect: clock.Frames(0.05), Good: clock.Frames(0.11)}},
		Expecter: exp,
	})

	for _, e := range Events(exp.notes, exp.strum) {
		if len(e.Notes) > 1 {
			t.Fatalf("a run 100 ms apart was grouped into an event of %d notes", len(e.Notes))
		}
	}
}

func TestARealStrumIsStillOneEvent(t *testing.T) {
	// The other side of the same number: a pick crossing six strings spreads
	// them over a few milliseconds and that is one chord, not six notes.
	clock := testClock()
	spread := clock.Frames(0.004)

	notes := make([]Note, 6)
	for i := range notes {
		notes[i] = Note{Start: Frame(i) * spread, Duration: clock.Frames(1), MIDI: uint8(40 + i*5)}
	}

	events := Events(notes, clock.Frames(DefaultStrumToleranceSeconds))
	if len(events) != 1 || len(events[0].Notes) != 6 {
		t.Fatalf("got %d events; a strum is one", len(events))
	}
}
