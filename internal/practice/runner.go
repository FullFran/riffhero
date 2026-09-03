package practice

// Runner is one practice run: a playhead, a detector, a scoreboard and the
// progressive rule, wired together.
//
// It exists so that the loop at the centre of this app — advance, detect,
// score, notice a lap, adjust the speed — is written once, in the domain,
// where it can be driven by a test's Transport and a scripted detector rather
// than by a guitar and a sound card. The Ebiten layer above it only draws.
type Runner struct {
	head Playhead
	det  Detector
	cfg  RunnerConfig

	all     []Note // the whole part
	scoped  []Note // what is being practised: the loop region, or all of it
	session *Session

	laps     int
	lastLap  SessionStats
	adaptive bool
}

// RunnerConfig is everything a run needs that is not the playhead or the
// detector.
type RunnerConfig struct {
	Notes       []Note
	Session     SessionConfig
	Progression Progression

	// Adaptive turns the progressive practice rule on: each lap of the A-B
	// region adjusts the speed for the next one.
	Adaptive bool

	// Expecter, when set, is told which notes are in scope so it can condition
	// detection on them. The DSP detector uses this to verify chords
	// spectrally instead of putting them to a monophonic estimator.
	Expecter Expecter

	// Strum is how far apart two notes may be written and still count as
	// struck together. Zero means DefaultStrumTolerance.
	Strum Frame
}

// DefaultStrumToleranceSeconds is how long a pick takes to cross the strings.
//
// It is deliberately not the Good scoring window. Those are two different
// questions — "how late may this be and still count" and "were these struck
// together" — and using one number for both put sixteenth notes at 150 BPM,
// which are 100 ms apart, through the chord verifier as if they were a chord.
// That path has a 171 ms window and stamps every pitch it finds at the same
// position, so a fast single-note run came back as pairs, all late.
const DefaultStrumToleranceSeconds = 0.030

// Expecter is the part of the detector that can be told what to expect.
type Expecter interface {
	Expect(notes []Note, tolerance Frame)
}

// Update is what one turn of the loop did, for the UI to render.
type Update struct {
	Position Frame
	Fed      []FeedResult

	// LapDone is set on the turn a lap of the A-B region completed, with the
	// scoreboard it finished on and what the progressive rule decided.
	LapDone    bool
	LapStats   SessionStats
	Adjustment Adjustment
	Speed      float64
}

func NewRunner(head Playhead, det Detector, cfg RunnerConfig) *Runner {
	if cfg.Progression == (Progression{}) {
		cfg.Progression = DefaultProgression
	}
	if cfg.Strum <= 0 {
		cfg.Strum = head.Clock().Frames(DefaultStrumToleranceSeconds)
	}
	r := &Runner{
		head:     head,
		det:      det,
		cfg:      cfg,
		all:      cfg.Notes,
		adaptive: cfg.Adaptive,
	}
	r.laps = head.Laps()
	r.rescope()
	return r
}

// Session is the live scoreboard.
func (r *Runner) Session() *Session { return r.session }

// Scope is the set of notes currently being practised.
func (r *Runner) Scope() []Note { return r.scoped }

// All is the whole part, which the view still draws outside the loop region.
func (r *Runner) All() []Note { return r.all }

// LastLap is the scoreboard the previous lap finished on.
func (r *Runner) LastLap() SessionStats { return r.lastLap }

// Adaptive reports whether the progressive rule is running.
func (r *Runner) Adaptive() bool { return r.adaptive }

// SetAdaptive turns the progressive rule on or off.
func (r *Runner) SetAdaptive(on bool) { r.adaptive = on }

// SetLoop installs an A-B region and rescopes the scoreboard to it.
//
// Scoring the whole song while looping four bars would make accuracy
// meaningless — the notes outside the region can never be played, so they
// would all expire as misses and drag the number to nothing.
func (r *Runner) SetLoop(l Loop) {
	r.head.SetLoop(l)
	r.rescope()
}

// Restart returns to the start of the run and clears the scoreboard.
func (r *Runner) Restart() {
	r.head.Restart()
	r.reset()
	r.session.ResumeFrom(r.head.Position())
}

// Seek moves the playhead and tells the scoreboard a jump happened.
//
// The scoreboard is deliberately not cleared: seeking back a bar to have
// another go at it should not throw away the rest of the run. What it must not
// do is score the notes that were jumped over, which is what happened while
// the session was only ever told a position.
func (r *Runner) Seek(to Frame) {
	r.head.Seek(to)
	r.session.ResumeFrom(r.head.Position())
}

// Update advances the run by one turn of the game loop.
func (r *Runner) Update() Update {
	out := Update{Speed: r.head.Speed()}

	// The lap count is read before the position, and the playhead publishes
	// them in that order, so seeing a new lap guarantees the position has
	// already wrapped. Reading them the other way round would hand ResumeFrom
	// a position at the end of the region and stop the new lap ever expiring
	// anything.
	laps := r.head.Laps()
	pos := r.head.Position()
	out.Position = pos

	// A completed lap is handled before anything from the new one is scored,
	// so the scoreboard the rule reads is the lap that actually finished.
	if laps != r.laps {
		r.laps = laps
		out.LapDone = true
		out.LapStats = r.session.Stats()
		r.lastLap = out.LapStats

		if r.adaptive {
			next, adj := r.cfg.Progression.Evaluate(r.head.Speed(), out.LapStats)
			r.head.SetSpeed(next)
			out.Adjustment, out.Speed = adj, next
		}
		r.session.Reset()
		r.session.ResumeFrom(pos)
	}

	for _, d := range r.det.Poll(pos) {
		out.Fed = append(out.Fed, r.session.Feed(d))
	}
	r.session.Advance(pos)
	return out
}

// rescope rebuilds the scoreboard for whatever region is being practised.
func (r *Runner) rescope() {
	if l := r.head.Loop(); l.Active() {
		r.scoped = NotesIn(r.all, l.A, l.B)
	} else {
		r.scoped = r.all
	}
	r.session = NewSession(r.scoped, r.cfg.Session)
	r.session.ResumeFrom(r.head.Position())
	r.laps = r.head.Laps()

	if r.cfg.Expecter != nil {
		r.cfg.Expecter.Expect(r.scoped, r.cfg.Strum)
	}
}

func (r *Runner) reset() {
	r.session.Reset()
	r.laps = r.head.Laps()
	r.lastLap = SessionStats{}
}
