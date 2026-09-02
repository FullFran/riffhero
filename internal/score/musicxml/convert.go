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

// build turns a parsed document into the domain model. It never fails on its
// own: every field it cannot determine just falls back to a sane default, so
// a malformed or sparse score degrades gracefully instead of erroring out
// after the XML has already been accepted.
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

	song.Grid = buildGrid(doc.Parts, clock)

	for _, part := range doc.Parts {
		track := buildTrack(part, partMeta[part.ID], clock, song.Grid)
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
// Reusing Grid's own bar boundaries later (instead of re-deriving them from
// notes) is what keeps note placement from drifting: BuildGrid computes each
// bar from its section's start rather than accumulating bar by bar, so
// rounding never compounds across a long score.
func buildGrid(parts []partXML, clock practice.Clock) practice.Grid {
	numMeasures := 0
	for _, p := range parts {
		if len(p.Measures) > numMeasures {
			numMeasures = len(p.Measures)
		}
	}

	bpm := defaultBPM
	sig := practice.CommonTime

	sections := make([]practice.Section, 0, numMeasures)
	for i := 0; i < numMeasures; i++ {
		for _, p := range parts {
			if i >= len(p.Measures) {
				continue
			}
			for _, item := range p.Measures[i].items {
				switch item.kind {
				case itemAttributes:
					if item.attributes.Time != nil {
						if s, ok := parseTimeSignature(*item.attributes.Time); ok {
							sig = s
						}
					}
				case itemTempo:
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

// buildTrack walks one <part>'s measures in document order, converting XML
// notes into domain notes on a single shared timeline.
func buildTrack(part partXML, meta scorePartXML, clock practice.Clock, grid practice.Grid) practice.Track {
	track := practice.Track{
		Name:       meta.Name,
		Instrument: meta.ScoreInstrument.Name,
		Tuning:     practice.StandardTuning,
	}
	fretboard := practice.NewFretboard(track.Tuning)
	tuningFound := false

	divisions := defaultDivisions
	pending := make(map[tieKey]*pendingTie)

	// lastStart is the cursor position (in divisions) of the last note that
	// was not itself a <chord/> member. A chord note starts there instead of
	// at the cursor, because in MusicXML only the first note of a chord
	// advances the cursor — the rest are written as if replayed on top of it.
	var lastStart int64
	haveLastStart := false

	for i, meas := range part.Measures {
		barStart := practice.Frame(0)
		bpm := defaultBPM
		if i < len(grid) {
			barStart = grid[i].Start
			bpm = grid[i].BPM
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
					&cursor, lastStart, haveLastStart, fretboard, pending,
				)
			}
		}
	}

	// A tie that never reached its matching stop — a truncated score, or
	// simply a writer's mistake — should still sound rather than vanish, so
	// whatever duration it accumulated is emitted as-is.
	for _, p := range pending {
		track.Notes = append(track.Notes, p.note)
	}

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
	pending map[tieKey]*pendingTie,
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
	str, fret := stringFretFromTechnical(n.Notations)
	if str == 0 {
		if s, f, ok := fretboard.Place(midi); ok {
			str, fret = s, f
		}
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

	if p, ok := pending[key]; ok {
		// A tied continuation does not start a new note — it only extends
		// the duration of the one already waiting, so a chain of tied notes
		// sounds as a single hit instead of retriggering at every barline.
		p.note.Duration += note.Duration
		if stopped && !started {
			track.Notes = append(track.Notes, p.note)
			delete(pending, key)
		}
		return lastStart, haveLastStart
	}

	if started {
		pending[key] = &pendingTie{note: note}
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
		t.Name = "Tab"
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
			return t, true
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
