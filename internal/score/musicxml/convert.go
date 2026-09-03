package musicxml

import (
	"math"
	"strconv"
	"strings"

	"github.com/FullFran/riffhero/internal/practice"
)

const (
	// defaultDivisions and defaultBPM stand in when a document never states
	// them, which is legal in a single-tempo, unmetered fragment: MusicXML
	// only requires <divisions> before the first note that needs it, and
	// requires no tempo at all.
	defaultDivisions = 1.0
	defaultBPM       = 120.0

	// graceNoteFraction is how much of a quarter note a grace note is given.
	// Grace notes carry no <duration> — they borrow time from the note they
	// decorate rather than occupying their own — so any small nominal length
	// works as long as it does not read as a "real" beat; a 32nd note does.
	graceNoteFraction = 0.125
)

// build turns a parsed document into the domain model. Almost nothing here
// fails: every field it cannot determine falls back to a sane default, so a
// malformed or sparse score degrades gracefully instead of erroring out after
// the XML has already been accepted. The one exception is a repeat structure
// that does not terminate, which cannot be defaulted away — see expandRepeats.
func build(doc scorePartwiseXML, clock practice.Clock) (*practice.Song, error) {
	song := &practice.Song{
		Clock:  clock,
		Source: "MusicXML",
		Title:  firstNonEmpty(doc.Work.Title, doc.MovementTitle),
		Artist: composer(doc.Identification.Creators),
	}

	partMeta := make(map[string]scorePartXML, len(doc.PartList.ScoreParts))
	for _, sp := range doc.PartList.ScoreParts {
		partMeta[sp.ID] = sp
	}

	// The played order is resolved once, before anything is converted: it is a
	// property of the score, not of a part, and the grid and every track have
	// to agree on it or the notes and the bar map describe different songs.
	order, err := expandRepeats(doc.Parts)
	if err != nil {
		return nil, err
	}

	song.Grid = buildGrid(doc.Parts, clock, order)

	for _, part := range doc.Parts {
		track := buildTrack(part, partMeta[part.ID], clock, song.Grid, order)
		if len(track.Notes) == 0 {
			continue
		}
		song.Tracks = append(song.Tracks, track)
	}

	song.Normalize()
	return song, nil
}

// buildGrid derives the tempo/meter timeline every part's notes are placed
// against. MusicXML measures line every part up in lockstep — measure N
// starts at the same instant in every part — so this is one pass over
// measure INDEXES rather than one pass per part: whichever part happens to
// carry a <time> or tempo change at measure i, that change takes effect for
// every part from measure i on.
//
// order is the expanded, linear sequence of measure indices from
// expandRepeats, so a measure inside a repeat contributes one bar per pass.
//
// Meter and tempo are resolved differently under a repeat, on purpose. A meter
// is a property of the written measure — it says how much music the measure
// holds — so it is carried forward in DOCUMENT order and then looked up per
// pass; a measure of four quarter notes does not become a 3/4 bar because the
// music after it changed meter before jumping back. A tempo is a property of
// the moment it is played, so it is carried forward in PLAYED order: a
// repeated measure that restates its own tempo restates it on every pass, and
// one that does not keeps whatever was in force when the repeat jumped back.
// gp/timeline.go makes the same call for the same reason.
//
// Reusing Grid's own bar boundaries later (instead of re-deriving them from
// notes) is what keeps note placement from drifting: BuildGrid computes each
// bar from its section's start rather than accumulating bar by bar, so
// rounding never compounds across a long score.
func buildGrid(parts []partXML, clock practice.Clock, order []int) practice.Grid {
	sigs := signaturesByMeasure(parts)

	bpm := defaultBPM
	sections := make([]practice.Section, 0, len(order))
	for _, measure := range order {
		sig := practice.CommonTime
		if measure < len(sigs) {
			sig = sigs[measure]
		}

		for _, p := range parts {
			if measure >= len(p.Measures) {
				continue
			}
			for _, item := range p.Measures[measure].items {
				if item.kind == itemTempo {
					bpm = item.tempoBPM
				}
			}
		}

		if n := len(sections); n > 0 && sections[n-1].BPM == bpm && sections[n-1].Sig == sig {
			sections[n-1].Bars++
		} else {
			sections = append(sections, practice.Section{BPM: bpm, Sig: sig, Bars: 1})
		}
	}

	return practice.BuildGrid(clock, 0, sections)
}

// signaturesByMeasure resolves the meter of every WRITTEN measure, in document
// order across all parts: a measure with no <time> keeps the previous one's,
// which is how the format avoids repeating it on every measure.
func signaturesByMeasure(parts []partXML) []practice.TimeSignature {
	count := 0
	for _, p := range parts {
		if len(p.Measures) > count {
			count = len(p.Measures)
		}
	}

	out := make([]practice.TimeSignature, count)
	current := practice.CommonTime
	for i := 0; i < count; i++ {
		for _, p := range parts {
			if i >= len(p.Measures) {
				continue
			}
			for _, item := range p.Measures[i].items {
				if item.kind != itemAttributes || item.attributes.Time == nil {
					continue
				}
				if sig, ok := parseTimeSignature(*item.attributes.Time); ok {
					current = sig
				}
			}
		}
		out[i] = current
	}
	return out
}

// divisionsByMeasure resolves the divisions in force at the START of each
// written measure of one part, in document order.
//
// Divisions is the measure's own tick resolution — how many units of <duration>
// make a quarter note — so it is emphatically NOT carried through the played
// order the way tempo is. If it were, a measure inside a repeat would have its
// notes re-scaled on the second pass by a <divisions> change written after it,
// and the same written half note would come out a different length each time
// round.
func divisionsByMeasure(measures []measureXML) []float64 {
	out := make([]float64, len(measures))
	current := defaultDivisions
	for i, m := range measures {
		out[i] = current
		for _, item := range m.items {
			if item.kind == itemAttributes && item.attributes.Divisions != nil && *item.attributes.Divisions > 0 {
				current = *item.attributes.Divisions
			}
		}
	}
	return out
}

func parseTimeSignature(t timeXML) (practice.TimeSignature, bool) {
	beats, err1 := strconv.Atoi(strings.TrimSpace(t.Beats))
	unit, err2 := strconv.Atoi(strings.TrimSpace(t.BeatType))
	if err1 != nil || err2 != nil || beats <= 0 || unit <= 0 {
		return practice.TimeSignature{}, false
	}
	return practice.TimeSignature{Beats: beats, Unit: unit}, true
}

// tieKey identifies one in-flight tie. Matching by (voice, pitch) rather than
// by pitch alone is what keeps two voices that happen to share a pitch at the
// same moment from being stitched into each other's ties.
type tieKey struct {
	voice string
	midi  uint8
}

// pendingTie is a tied note still waiting for its closing <tie type="stop">.
// Its Duration grows as each tied segment is folded in; its Start never
// changes after the first segment, since a tie chain sounds as one note
// beginning where the first note began.
type pendingTie struct {
	note practice.Note
}

// tieTable holds the ties still waiting for their stop: a map for lookup, plus
// the order they were opened in.
//
// The order is the whole point. Whatever is still open when the part ends gets
// flushed, and ranging over the map to do that made the same bytes produce
// different songs: Go randomises map iteration, practice.Song.Normalize sorts
// stably on (Start, MIDI), so two unterminated ties sharing a start and a
// pitch kept whatever order the map happened to hand over. Two hundred runs of
// one such file split 172/28 between the two orderings.
type tieTable struct {
	byKey map[tieKey]*pendingTie
	order []tieKey
}

func newTieTable() *tieTable {
	return &tieTable{byKey: make(map[tieKey]*pendingTie)}
}

func (t *tieTable) pending(k tieKey) (*pendingTie, bool) {
	p, ok := t.byKey[k]
	return p, ok
}

// open starts a tie on k. Callers always close whatever was already there
// first; the guard is only so a re-open cannot list the same key twice.
func (t *tieTable) open(k tieKey, n practice.Note) {
	if _, ok := t.byKey[k]; !ok {
		t.order = append(t.order, k)
	}
	t.byKey[k] = &pendingTie{note: n}
}

func (t *tieTable) close(k tieKey) {
	if _, ok := t.byKey[k]; !ok {
		return
	}
	delete(t.byKey, k)
	for i, key := range t.order {
		if key == k {
			t.order = append(t.order[:i], t.order[i+1:]...)
			break
		}
	}
}

// drain returns every tie still open, oldest first, and empties the table.
func (t *tieTable) drain() []practice.Note {
	notes := make([]practice.Note, 0, len(t.order))
	for _, k := range t.order {
		if p, ok := t.byKey[k]; ok {
			notes = append(notes, p.note)
		}
	}
	t.byKey = make(map[tieKey]*pendingTie)
	t.order = nil
	return notes
}

// buildTrack walks one <part>'s measures in PLAYED order — the expanded
// sequence from expandRepeats, so a measure inside a repeat is converted once
// per pass — turning XML notes into domain notes on a single shared timeline.
//
// Everything scoped to a measure has to behave under a repeat, and each piece
// behaves differently. Divisions are resolved per WRITTEN measure in document
// order (see divisionsByMeasure), so a note keeps its written length on every
// pass. The cursor resets per measure, as it must. And the tie table is
// carried across the whole part, which handles the case that actually bites: a
// tie opened in the last measure before a backward repeat never meets its stop
// on that pass, because the music jumps back instead. When that measure comes
// round again the note it opened is flushed at its written length and a fresh
// tie starts — which is exactly what the first pass sounds like.
func buildTrack(part partXML, meta scorePartXML, clock practice.Clock, grid practice.Grid, order []int) practice.Track {
	track := practice.Track{
		Name:       meta.Name,
		Instrument: meta.ScoreInstrument.Name,
		Tuning:     practice.StandardTuning,
	}
	fretboard := practice.NewFretboard(track.Tuning)
	tuningFound := false

	divisionsAt := divisionsByMeasure(part.Measures)
	ties := newTieTable()

	// lastStart is the cursor position (in divisions) of the last note that
	// was not itself a <chord/> member. A chord note starts there instead of
	// at the cursor, because in MusicXML only the first note of a chord
	// advances the cursor — the rest are written as if replayed on top of it.
	var lastStart int64
	haveLastStart := false

	for played, measure := range order {
		if measure >= len(part.Measures) {
			// A part shorter than the longest one: it simply has no music
			// where the others do. The bar still exists on the grid.
			continue
		}
		meas := part.Measures[measure]
		divisions := divisionsAt[measure]

		barStart := practice.Frame(0)
		bpm := defaultBPM
		if played < len(grid) {
			barStart = grid[played].Start
			bpm = grid[played].BPM
		}

		var cursor int64
		for _, item := range meas.items {
			switch item.kind {
			case itemAttributes:
				if item.attributes.Divisions != nil && *item.attributes.Divisions > 0 {
					divisions = *item.attributes.Divisions
				}
				if !tuningFound {
					if t, ok := tuningFromStaffDetails(item.attributes.StaffDetails); ok {
						track.Tuning = t
						fretboard = practice.NewFretboard(t)
						tuningFound = true
					}
				}
			case itemCursorMove:
				cursor += item.cursorMove
				if cursor < 0 {
					// A backup past the start of the measure is malformed;
					// clamping instead of going negative keeps the frame math
					// below from silently collapsing to zero (Clock.Frames
					// treats a non-positive duration as zero).
					cursor = 0
				}
			case itemNote:
				lastStart, haveLastStart = placeNote(
					&track, item.note, clock, divisions, bpm, barStart,
					&cursor, lastStart, haveLastStart, fretboard, ties,
				)
			}
		}
	}

	// A tie that never reached its matching stop — a truncated score, or
	// simply a writer's mistake — should still sound rather than vanish, so
	// whatever duration it accumulated is emitted as-is, oldest tie first.
	track.Notes = append(track.Notes, ties.drain()...)

	return track
}

// placeNote converts one <note> into (at most) one domain note, folding it
// into an in-flight tie when it continues one. It returns the updated
// lastStart/haveLastStart pair the caller carries into the next note.
func placeNote(
	track *practice.Track,
	n noteXML,
	clock practice.Clock,
	divisions, bpm float64,
	barStart practice.Frame,
	cursor *int64,
	lastStart int64,
	haveLastStart bool,
	fretboard *practice.Fretboard,
	ties *tieTable,
) (newLastStart int64, newHaveLastStart bool) {
	grace := n.Grace != nil
	chord := n.Chord != nil && !grace

	startDivisions := *cursor
	if chord && haveLastStart {
		startDivisions = lastStart
	}

	var duration practice.Frame
	switch {
	case grace:
		// Grace notes have no <duration> and do not move the cursor: they
		// are played "on top of" the beat they decorate, not inside it.
		duration = clock.Frames(graceNoteFraction * 60.0 / bpm)
	default:
		var raw int64
		if n.Duration != nil {
			raw = *n.Duration
		}
		duration = framesForDivisions(clock, raw, divisions, bpm)
		if !chord {
			*cursor += raw
		}
	}

	if !chord {
		lastStart, haveLastStart = startDivisions, true
	}

	if n.Rest != nil || n.Pitch == nil {
		// A rest still occupies time (the cursor already moved above) but
		// produces no playable note.
		return lastStart, haveLastStart
	}

	midi := midiFromPitch(*n.Pitch)

	// The file's own tablature is trusted only when it agrees with the pitch
	// beside it. A position that sounds something else would show the player
	// one thing and score another, and there is no way for them to satisfy
	// both. Anything else — no tablature, or tablature that contradicts
	// itself — is placed from the pitch, moving by octaves if the part was
	// written for an instrument with a wider range.
	str, fret := stringFretFromTechnical(n.Notations)
	if !fretboard.Tuning.Sounds(midi, str, fret) {
		placed, s, f, ok := fretboard.PlaceOrTranspose(midi)
		if !ok {
			return lastStart, haveLastStart
		}
		midi, str, fret = placed, s, f
	}

	note := practice.Note{
		Start:    barStart + framesForDivisions(clock, startDivisions, divisions, bpm),
		Duration: duration,
		MIDI:     midi,
		String:   str,
		Fret:     fret,
	}

	key := tieKey{voice: n.Voice, midi: midi}
	started, stopped := tieFlags(n.Ties)

	if p, ok := ties.pending(key); ok {
		if stopped {
			// A tied continuation does not start a new note — it only extends
			// the duration of the one already waiting, so a chain of tied
			// notes sounds as a single hit instead of retriggering at every
			// barline. A note that both stops and starts is the middle of a
			// longer chain, so the tie stays open.
			p.note.Duration += note.Duration
			if !started {
				track.Notes = append(track.Notes, p.note)
				ties.close(key)
			}
			return lastStart, haveLastStart
		}

		// No stop on this note, so it does not continue the pending tie —
		// something on the same (voice, pitch) key is just being played again.
		// Folding it in regardless is how one stray <tie type="start"/> used
		// to swallow the rest of the part: three written whole notes, only the
		// first tied and nothing ever stopping it, came out as a single note
		// of triple length. Flush the tie as the note it actually was and let
		// this one stand on its own.
		track.Notes = append(track.Notes, p.note)
		ties.close(key)
	}

	if started {
		ties.open(key, note)
		return lastStart, haveLastStart
	}

	track.Notes = append(track.Notes, note)
	return lastStart, haveLastStart
}

func tieFlags(ties []tieXML) (started, stopped bool) {
	for _, t := range ties {
		switch t.Type {
		case "start":
			started = true
		case "stop":
			stopped = true
		}
	}
	return started, stopped
}

func stringFretFromTechnical(n *notationsXML) (str, fret uint8) {
	if n == nil || n.Technical == nil {
		return 0, 0
	}
	t := n.Technical
	// Both must be present: a lone <string> or <fret> is not enough to place
	// a note, and guessing the other half would fabricate tablature the
	// score never specified.
	if t.StringNum == nil || t.FretNum == nil {
		return 0, 0
	}
	return uint8(*t.StringNum), uint8(*t.FretNum)
}

// framesForDivisions converts a duration expressed in a part's own divisions
// (its "ticks per quarter note", which different notation software sets
// differently and which can itself change mid-score) into sample frames at
// the tempo in force. Dividing by divisions first is what makes the result
// independent of whatever tick resolution the exporter chose.
func framesForDivisions(clock practice.Clock, dur int64, divisions, bpm float64) practice.Frame {
	if divisions <= 0 {
		divisions = defaultDivisions
	}
	if bpm <= 0 {
		bpm = defaultBPM
	}
	quarters := float64(dur) / divisions
	seconds := quarters * 60.0 / bpm
	return clock.Frames(seconds)
}

// tuningFromStaffDetails recovers a real guitar tuning from a tab staff's
// <staff-tuning> entries, or reports false when the part carries none.
func tuningFromStaffDetails(details []staffDetailsXML) (practice.Tuning, bool) {
	for _, d := range details {
		if len(d.StaffTunings) == 0 {
			continue
		}
		lines := 6
		if d.StaffLines != nil && *d.StaffLines > 0 {
			lines = *d.StaffLines
		}

		t := practice.StandardTuning
		found := false
		for _, st := range d.StaffTunings {
			// <staff-tuning> numbers its "line" from the BOTTOM of the staff
			// (the lowest-pitched string), the opposite of RiffHero's
			// convention where string 1 is the highest-sounding string. The
			// line number has to be flipped into a tab string number.
			strNum := lines - st.Line + 1
			if strNum < 1 || strNum > 6 {
				continue
			}
			alter := 0.0
			if st.TuningAlter != nil {
				alter = *st.TuningAlter
			}
			t.Strings[strNum-1] = midiFromStepOctaveAlter(st.TuningStep, alter, st.TuningOctave)
			found = true
		}
		if found {
			return t.Named(), true
		}
	}
	return practice.Tuning{}, false
}

var stepPitchClass = map[string]int{
	"C": 0, "D": 2, "E": 4, "F": 5, "G": 7, "A": 9, "B": 11,
}

func midiFromPitch(p pitchXML) uint8 {
	alter := 0.0
	if p.Alter != nil {
		alter = *p.Alter
	}
	return midiFromStepOctaveAlter(p.Step, alter, p.Octave)
}

// midiFromStepOctaveAlter converts a MusicXML step/alter/octave triple to a
// MIDI note number, with octave 4 as the octave containing middle C — so
// step C, alter 0, octave 4 must come out to 60.
func midiFromStepOctaveAlter(step string, alter float64, octave int) uint8 {
	pc, ok := stepPitchClass[strings.ToUpper(strings.TrimSpace(step))]
	if !ok {
		pc = 0
	}
	// Alter is nominally an integer number of semitones, but the schema
	// allows a decimal for microtonal notation; RiffHero's model has no room
	// for that, so it rounds to the nearest semitone rather than truncating,
	// which would bias every negative alter flat.
	midi := 12*(octave+1) + pc + int(math.Round(alter))
	return clampMIDI(midi)
}

func clampMIDI(v int) uint8 {
	switch {
	case v < 0:
		return 0
	case v > 127:
		return 127
	default:
		return uint8(v)
	}
}

func composer(creators []creatorXML) string {
	for _, c := range creators {
		if strings.EqualFold(strings.TrimSpace(c.Type), "composer") {
			return strings.TrimSpace(c.Value)
		}
	}
	return ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v := strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}
