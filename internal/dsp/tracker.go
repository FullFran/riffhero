package dsp

// Note is one stable note the tracker is willing to commit to.
type Note struct {
	At         int64   // absolute sample position of the attack
	MIDI       uint8   // nearest equal-tempered semitone
	Cents      float64 // how far off that semitone the string actually was
	Confidence float64 // 0..1, how much agreement produced this note
}

// Tracker turns a stream of samples into notes.
//
// It is deliberately onset-driven rather than frame-driven. Estimating pitch
// on every window and then smoothing the result is the usual approach and it
// jitters: the estimate wobbles across a semitone boundary and the score sees
// notes the player never played. Here the onset decides *when* a note exists,
// and pitch estimation only has to answer *what* it was — over a few windows
// just after the attack, which then have to agree before anything is emitted.
//
// The cost is latency, and it is a known quantity rather than a hope: see
// LatencyMillis.
type Tracker struct {
	// Expect is how the score conditions detection. Given the stream position
	// of an attack it returns the pitches the score expects there, or nothing
	// when it expects one note or has no opinion.
	//
	// Two or more expected pitches switch the analysis from "what note is
	// this?" to "are these notes present?", which is a question a spectrum can
	// answer and unconstrained polyphonic transcription cannot. The returned
	// slice is read immediately and never retained, so the caller may reuse it.
	Expect func(at int64) []uint8

	sampleRate int

	gate  *Gate
	onset *Onset
	est   *Estimator
	chord *ChordVerifier

	// attackSkip is how much of the attack transient to step over before
	// estimating. The first few milliseconds of a pluck are broadband noise
	// with no usable period in them.
	attackSkip int
	// confirmations is how many staggered windows must be estimated, and
	// quorum how many of them must land on the same semitone.
	confirmations int
	quorum        int

	history      []float32
	historyStart int64 // absolute position of history[0]

	hopBuf  []float32    // partial hop carried between Push calls
	pending []int64      // onsets waiting for enough audio to be analysed
	votes   []windowVote // reused by analyse
}

// NewTracker returns a tracker tuned for guitar at the given sample rate.
func NewTracker(sampleRate int) *Tracker {
	return &Tracker{
		sampleRate:    sampleRate,
		gate:          NewGate(),
		onset:         NewOnset(sampleRate),
		est:           NewEstimator(sampleRate),
		chord:         NewChordVerifier(sampleRate),
		attackSkip:    int(0.012 * float64(sampleRate)),
		confirmations: 3,
		quorum:        2,
	}
}

// span is how many samples after an onset the monophonic path needs before it
// can decide what the note was.
func (t *Tracker) span() int {
	return t.attackSkip + t.est.WindowSize() + (t.confirmations-1)*t.onset.Hop()
}

// chordSpan is the same for the chord path, and it is much longer: telling
// whether a given pitch is present needs frequency resolution, and frequency
// resolution is time.
//
// The two are kept apart on purpose. Making every onset wait for the chord
// window would triple the latency of the single notes that make up most of
// practice, to pay for an analysis they never run.
func (t *Tracker) chordSpan() int {
	if n := t.attackSkip + t.chord.WindowSize(); n > t.span() {
		return n
	}
	return t.span()
}

// LatencyMillis is the delay between a string being struck and the note being
// emitted, ignoring whatever the audio device adds on top. It is a property of
// the analysis, so it is worth stating rather than measuring by surprise.
func (t *Tracker) LatencyMillis() float64 {
	return float64(t.span()) / float64(t.sampleRate) * 1000
}

// ChordLatencyMillis is the same figure for an expected chord.
func (t *Tracker) ChordLatencyMillis() float64 {
	return float64(t.chordSpan()) / float64(t.sampleRate) * 1000
}

// Push feeds a block of samples and returns any notes that became certain
// because of it. Blocks may be any size; the tracker does its own hop
// alignment, since an audio device picks its period size, not us.
func (t *Tracker) Push(block []float32) []Note {
	t.history = append(t.history, block...)
	t.hopBuf = append(t.hopBuf, block...)

	hop := t.onset.Hop()
	consumed := 0
	for len(t.hopBuf)-consumed >= hop {
		block := t.hopBuf[consumed : consumed+hop]
		at, fired := t.onset.Push(block)

		// The gate tracks the signal level for the UI and for note-off later.
		// It deliberately does not veto an onset: it would be judging the same
		// envelope the onset already checked against its floor, and a veto here
		// would still consume the onset's refractory window, silently losing
		// the attack instead of merely ignoring it.
		t.gate.UpdateLevel(t.onset.Level())

		if fired {
			t.pending = append(t.pending, at)
		}
		consumed += hop
	}
	// Compact rather than reslice, so the backing array is reused instead of
	// being reallocated every few blocks for the life of the session.
	t.hopBuf = append(t.hopBuf[:0], t.hopBuf[consumed:]...)

	notes := t.resolve()
	t.trim()
	return notes
}

// resolve analyses every pending onset that now has enough audio behind it.
func (t *Tracker) resolve() []Note {
	var out []Note
	available := t.historyStart + int64(len(t.history))

	kept := t.pending[:0]
	for _, at := range t.pending {
		var expected []uint8
		if t.Expect != nil {
			expected = t.Expect(at)
		}

		need := t.span()
		if len(expected) >= 2 {
			need = t.chordSpan()
		}
		if at+int64(need) > available {
			kept = append(kept, at) // not enough audio yet
			continue
		}

		if len(expected) >= 2 {
			if notes := t.analyseChord(at, expected); len(notes) > 0 {
				out = append(out, notes...)
				continue
			}
			// Nothing of the expected chord is there. The player may have hit
			// one string of it, so fall through rather than emitting silence:
			// a single wrong note is information, and the score can use it.
		}
		if note, ok := t.analyse(at); ok {
			out = append(out, note)
		}
	}
	t.pending = kept
	return out
}

// analyseChord tests the expected pitches against the spectrum just after the
// attack and emits one note per pitch it finds.
//
// Emitting a separate note per pitch rather than one chord event is what keeps
// the domain simple: the scoring session already resolves notes one at a time,
// so a strummed chord scores as the notes it is made of and nothing above the
// DSP has to learn what a chord is.
func (t *Tracker) analyseChord(at int64, expected []uint8) []Note {
	start := at + int64(t.attackSkip) - t.historyStart
	window := t.chord.WindowSize()
	if start < 0 || start+int64(window) > int64(len(t.history)) {
		return nil
	}

	res := t.chord.Verify(t.history[start:start+int64(window)], expected)
	if !res.Voiced || res.Found == 0 {
		return nil
	}

	out := make([]Note, 0, res.Found)
	for _, e := range res.Notes {
		if !e.Present {
			continue
		}
		out = append(out, Note{
			At:    at,
			MIDI:  e.MIDI,
			Cents: e.Cents,
			// The whole event's score matters as much as the single pitch:
			// finding an E while the rest of the chord is missing is weaker
			// evidence than finding the same E inside a clean strum.
			Confidence: e.Score * res.Score,
		})
	}
	return out
}

// windowVote is one window's opinion about the note that just started.
type windowVote struct {
	midi    uint8
	cents   float64
	clarity float64
	agreed  bool // whether MPM and YIN concurred on this window
}

// analyse estimates the pitch of the note starting at an onset over several
// staggered windows, then asks decide whether they add up to a note.
func (t *Tracker) analyse(at int64) (Note, bool) {
	window := t.est.WindowSize()
	hop := t.onset.Hop()

	votes := t.votes[:0]
	for i := 0; i < t.confirmations; i++ {
		start := at + int64(t.attackSkip+i*hop) - t.historyStart
		if start < 0 || start+int64(window) > int64(len(t.history)) {
			continue
		}
		p := t.est.Detect(t.history[start : start+int64(window)])
		if !p.Voiced {
			continue
		}
		midi, cents := NearestNote(p.Hz)
		votes = append(votes, windowVote{midi: midi, cents: cents, clarity: p.Clarity, agreed: p.Agreed})
	}
	t.votes = votes

	return t.decide(at, votes)
}

// decide turns a set of window opinions into a note, or into nothing.
//
// Two rules matter here. The first is the quorum: a majority of windows must
// land on the same semitone, which is what stops a single bad window from
// inventing a note. The second is that windows where MPM and YIN disagreed do
// not count as ordinary evidence — a disagreement is nearly always one of them
// taking an octave, so a semitone supported only by disputed windows has to be
// unanimous before it is believed. Without that second rule the cross-check
// would only ever change a number nobody reads.
func (t *Tracker) decide(at int64, votes []windowVote) (Note, bool) {
	type tally struct {
		count   int
		agreed  int
		cents   float64
		clarity float64
	}

	counts := make(map[uint8]*tally, len(votes))
	for _, v := range votes {
		e, ok := counts[v.midi]
		if !ok {
			e = &tally{}
			counts[v.midi] = e
		}
		e.count++
		e.cents += v.cents
		e.clarity += v.clarity
		if v.agreed {
			e.agreed++
		}
	}

	// Ranked by support, then by total clarity, then by pitch. Map iteration
	// order is random, so without an explicit tie-break a split vote would
	// resolve differently from run to run.
	var best uint8
	var bestT *tally
	for midi, e := range counts {
		switch {
		case bestT == nil,
			e.count > bestT.count,
			e.count == bestT.count && e.clarity > bestT.clarity,
			e.count == bestT.count && e.clarity == bestT.clarity && midi < best:
			best, bestT = midi, e
		}
	}
	if bestT == nil || bestT.count < t.quorum {
		return Note{}, false
	}
	if bestT.agreed == 0 && bestT.count < t.confirmations {
		return Note{}, false
	}

	n := float64(bestT.count)
	return Note{
		At:         at,
		MIDI:       best,
		Cents:      bestT.cents / n,
		Confidence: bestT.clarity / n,
	}, true
}

// trim drops history that no pending onset can still need, so a long session
// does not grow the buffer without bound.
func (t *Tracker) trim() {
	// Sized on the chord path: it is the longer of the two, and history thrown
	// away early is a chord that can never be verified.
	keepFrom := t.historyStart + int64(len(t.history)) - int64(t.chordSpan()+t.onset.Hop())
	for _, at := range t.pending {
		if at < keepFrom {
			keepFrom = at
		}
	}
	if drop := keepFrom - t.historyStart; drop > 0 {
		if drop > int64(len(t.history)) {
			drop = int64(len(t.history))
		}
		t.history = append(t.history[:0], t.history[drop:]...)
		t.historyStart += drop
	}
}

// Skip advances the tracker past samples that never arrived, so a gap in the
// input shifts nothing that comes after it. Pending onsets are abandoned: the
// audio they needed is exactly what went missing.
func (t *Tracker) Skip(n int64) {
	if n <= 0 {
		return
	}
	t.onset.Skip(n)
	t.gate.Reset()
	t.history = t.history[:0]
	t.historyStart = t.onset.Position()
	t.hopBuf = t.hopBuf[:0]
	t.pending = nil
}

// Level is the current signal level in dBFS, and Present reports whether the
// gate considers a string to be sounding. Both are for display and for note-off
// later; neither takes part in deciding that a note happened.
func (t *Tracker) Level() float64 { return t.gate.LevelDB() }

// Present reports whether the gate is currently open.
func (t *Tracker) Present() bool { return t.gate.Open() }

// Reset clears every bit of state so a run can start over.
func (t *Tracker) Reset() {
	t.gate.Reset()
	t.onset.Reset()
	t.history = t.history[:0]
	t.historyStart = 0
	t.hopBuf = t.hopBuf[:0]
	t.pending = nil
}
