package gp

import (
	"math"
	"strings"

	"github.com/FullFran/riffhero/internal/practice"
)

// maxImportFret bounds what is accepted as a fret number.
//
// Two reasons for the bound and one for its value. Fret is a uint8 in the
// domain model, so an out-of-range value in a corrupt file would wrap into a
// plausible-looking note instead of being noticed; and a note past the end of
// the neck is not playable, so keeping it would put a position in the
// tablature that no hand can reach. The value is the domain's own MaxFret,
// which is also what every other importer places notes within — a score whose
// tab and whose pitches disagree is worse than one missing a note.
const maxImportFret = practice.MaxFret

// builder holds everything the traversal needs: the parsed document, the id
// tables, the expanded timeline, and the clock the frames are measured in.
type builder struct {
	doc   *gpif
	tab   tables
	tl    *timeline
	clock practice.Clock
}

// frames converts a duration in quarter notes at a given tempo into frames.
//
// It rounds to the nearest frame rather than truncating the way Clock.Frames
// does. Offsets inside a bar are routinely not whole frames — an eighth-note
// triplet at 130 BPM lands on 7384.6 — and truncating pushes every one of them
// early by up to a full frame, always in the same direction. Rounding halves
// the worst case and leaves no bias for the scorer's timing windows to inherit.
func (b *builder) frames(quarters, bpm float64) practice.Frame {
	if bpm <= 0 || b.clock.SampleRate <= 0 || quarters <= 0 {
		return 0
	}
	seconds := quarters * 60.0 / bpm
	return practice.Frame(math.Round(seconds * float64(b.clock.SampleRate)))
}

// barQuarters is the written length of a played bar, in quarter notes.
func (b *builder) barQuarters(bar int) float64 {
	sig := b.tl.sig[bar]
	return float64(sig.Beats) * 4.0 / float64(sig.Unit)
}

// frameAt resolves a position given as "quarters into played bar N" to an
// absolute frame.
//
// The offset is allowed to run past the end of its bar, which is how a note
// tied across a barline is measured: the carry walks forward bar by bar so the
// tail of the note is converted at the tempo of the bar it actually lands in,
// not the one it started in.
func (b *builder) frameAt(bar int, quarters float64) practice.Frame {
	if len(b.tl.grid) == 0 {
		return 0
	}
	if bar < 0 {
		bar = 0
	}
	if bar >= len(b.tl.grid) {
		bar = len(b.tl.grid) - 1
	}
	for bar < len(b.tl.grid)-1 && quarters >= b.barQuarters(bar) {
		quarters -= b.barQuarters(bar)
		bar++
	}
	return b.tl.grid[bar].Start + b.frames(quarters, b.tl.grid[bar].BPM)
}

// tieKey identifies the note a tie can continue. A tie joins the same string
// within the same voice, so both are needed: two voices may be holding notes
// on different strings at once, and keying on the string alone would let one
// voice's tie extend the other's note.
type tieKey struct {
	voice  int
	string int // as GPIF numbers it, which is what the file's Tie refers to
}

// trackWalk is the state carried through one track's traversal: the notes
// produced so far, and which of them a tie is still allowed to lengthen.
type trackWalk struct {
	index       int // position of the track in <Tracks>, which is how bars are addressed
	stringCount int
	tuning      practice.Tuning
	notes       []practice.Note
	held        map[tieKey]int
}

// build walks every track and produces the song.
func (b *builder) build() *practice.Song {
	tracks := make([]practice.Track, 0, len(b.doc.Tracks))
	for i := range b.doc.Tracks {
		tr := &b.doc.Tracks[i]

		tuning, stringCount, ok := trackTuning(tr)
		if !ok {
			// No usable string tuning: a piano, a drum kit, or an instrument
			// with more strings than the domain model has. Either way there is
			// no tablature to practise.
			continue
		}

		notes := b.trackNotes(i, stringCount, tuning)
		if len(notes) == 0 {
			continue
		}
		tracks = append(tracks, practice.Track{
			Name:       strings.TrimSpace(tr.Name),
			Instrument: trackInstrument(tr),
			Tuning:     tuning,
			Notes:      notes,
		})
	}

	return &practice.Song{
		Title:  strings.TrimSpace(b.doc.Score.Title),
		Artist: scoreArtist(b.doc.Score),
		Source: sourceName,
		Clock:  b.clock,
		Grid:   b.tl.grid,
		Tracks: tracks,
	}
}

// trackNotes walks the played bars of one track.
//
// The nesting here is the format: a master bar lists one bar id per track,
// positionally; a bar lists up to four voice ids; a voice lists beat ids; a
// beat points at a rhythm and lists note ids. Every hop is an id lookup that
// can miss, and a miss skips the smallest thing it can rather than the song.
func (b *builder) trackNotes(trackIndex, stringCount int, tuning practice.Tuning) []practice.Note {
	w := &trackWalk{
		index:       trackIndex,
		stringCount: stringCount,
		tuning:      tuning,
		held:        make(map[tieKey]int),
	}

	for barPos, master := range b.tl.order {
		barIDs := parseIntList(b.doc.MasterBars[master].Bars)
		if w.index >= len(barIDs) {
			continue // this track has no bar here
		}
		bar, ok := b.tab.bars[barIDs[w.index]]
		if !ok {
			continue
		}

		for slot, voiceID := range parseIntList(bar.Voices) {
			// -1 is GPIF's "this voice is empty in this bar"; the slot still
			// counts, because the slot number is the voice's identity.
			if voiceID < 0 {
				continue
			}
			voice, ok := b.tab.voices[voiceID]
			if !ok {
				continue
			}
			b.walkVoice(w, voice, slot, barPos)
		}
	}
	return w.notes
}

// walkVoice plays one voice of one bar, advancing a cursor measured in quarter
// notes from the barline.
func (b *builder) walkVoice(w *trackWalk, voice *voiceNode, voiceSlot, barPos int) {
	// Every voice starts at the start of the bar. Voices are simultaneous
	// layers, not consecutive phrases, so the cursor is local to this call
	// rather than carried across the voices of the bar.
	cursor := 0.0

	for _, beatID := range parseIntList(voice.Beats) {
		beat, ok := b.tab.beats[beatID]
		if !ok {
			continue
		}
		// A grace beat is an ornament borrowed from a neighbouring beat; it
		// owns none of the bar's time. Playing it would push everything after
		// it out of place, so it is dropped whole, before the cursor moves.
		if strings.TrimSpace(beat.GraceNotes) != "" {
			continue
		}
		rhythm, ok := b.tab.rhythms[intOr(beat.Rhythm.Ref, -1)]
		if !ok {
			continue
		}
		duration, ok := rhythm.quarters()
		if !ok {
			continue
		}

		// A beat with no notes is a rest. It emits nothing and still advances
		// the cursor, which is the entire point of it.
		for _, noteID := range parseIntList(beat.Notes) {
			note, ok := b.tab.notes[noteID]
			if !ok {
				continue
			}
			b.emitNote(w, note, voiceSlot, barPos, cursor, duration)
		}
		cursor += duration
	}
}

// emitNote turns one GPIF note into a domain note, or extends the note it is
// tied to.
func (b *builder) emitNote(w *trackWalk, note *noteNode, voiceSlot, barPos int, cursor, duration float64) {
	gpifString, fret, ok := note.fretPosition()
	if !ok || fret < 0 || fret > maxImportFret {
		return
	}
	str, ok := practiceString(gpifString, w.stringCount)
	if !ok {
		return
	}

	key := tieKey{voice: voiceSlot, string: gpifString}
	end := b.frameAt(barPos, cursor+duration)

	if note.tieDestination() {
		if i, found := w.held[key]; found {
			// The tie continues a note already on the timeline: lengthen it
			// instead of striking the string again. The map keeps pointing at
			// the head of the chain, so a note tied over three bars grows
			// twice rather than splitting into two notes.
			if d := end - w.notes[i].Start; d > w.notes[i].Duration {
				w.notes[i].Duration = d
			}
			return
		}
		// A destination with no origin is a broken file. Playing it as a fresh
		// note loses the tie but keeps the music; dropping it would leave a
		// hole the player would be scored against.
	}

	start := b.frameAt(barPos, cursor)
	w.notes = append(w.notes, practice.Note{
		Start:    start,
		Duration: end - start,
		MIDI:     w.tuning.MIDI(str, uint8(fret)),
		String:   str,
		Fret:     uint8(fret),
	})
	w.held[key] = len(w.notes) - 1
}

// practiceString converts GPIF's string number to RiffHero's.
//
// This is the single most dangerous conversion in the importer, because both
// conventions are plausible and getting it backwards produces a song that
// parses cleanly and is wrong on every note. GPIF numbers strings from 0
// counting UP from the lowest-sounding string, matching the order of
// <Pitches>. RiffHero numbers them from 1 counting DOWN from the
// highest-sounding string, the way tablature is drawn, so string 1 is the high
// E. The two run in opposite directions, hence the subtraction.
//
// The string count comes from the instrument's own <Pitches> rather than a
// constant 6, so a bass or a five-string instrument maps correctly too.
func practiceString(gpifString, stringCount int) (uint8, bool) {
	if stringCount <= 0 || stringCount > 6 {
		return 0, false
	}
	if gpifString < 0 || gpifString >= stringCount {
		return 0, false
	}
	return uint8(stringCount - gpifString), true
}

// trackTuning recovers the open pitches and capo of a track.
func trackTuning(tr *trackNode) (practice.Tuning, int, bool) {
	props := make([]propertyNode, 0, len(tr.Properties))
	props = append(props, tr.Properties...)
	for i := range tr.Staves {
		props = append(props, tr.Staves[i].Properties...)
	}

	var pitches []int
	capo := 0
	for i := range props {
		p := &props[i]
		switch p.Name {
		case "Tuning":
			if list := parseIntList(p.Pitches); len(list) > 0 {
				pitches = list
			}
		case "CapoFret":
			if p.Fret != nil {
				capo = *p.Fret
			}
		}
	}

	// More than six strings has nowhere to go: practice.Tuning is a fixed
	// [6]uint8 and squeezing a seven-string guitar into it would either drop a
	// string silently or renumber the other six. Skipping the track is the
	// honest failure.
	if len(pitches) == 0 || len(pitches) > 6 {
		return practice.Tuning{}, 0, false
	}
	if capo < 0 || capo > practice.MaxFret {
		capo = 0
	}

	var t practice.Tuning
	t.Capo = uint8(capo)
	for i, pitch := range pitches {
		if pitch < 0 || pitch > 127 {
			return practice.Tuning{}, 0, false
		}
		// <Pitches> is written lowest string first; Tuning.Strings is indexed
		// by string number minus one, and string 1 is the highest. The two
		// orders are reverses of each other.
		t.Strings[len(pitches)-1-i] = uint8(pitch)
	}
	// A track with fewer than six strings keeps the domain's six-slot array,
	// so it can never match a known six-string tuning; Named spells it out
	// instead, which is the right answer for a bass anyway.
	t = t.Named()
	return t, len(pitches), true
}

// trackInstrument picks the most specific instrument label the file offers.
// The sound name is the closest thing to what the part actually is; the track
// name is the fallback because a track called "Rhythm" says nothing.
func trackInstrument(tr *trackNode) string {
	for i := range tr.Sounds {
		if name := strings.TrimSpace(tr.Sounds[i].Name); name != "" {
			return name
		}
	}
	if name := strings.TrimSpace(tr.InstrumentSet.Name); name != "" {
		return name
	}
	if name := strings.TrimSpace(tr.InstrumentSet.Type); name != "" {
		return name
	}
	if name := strings.TrimSpace(tr.ShortName); name != "" {
		return name
	}
	return strings.TrimSpace(tr.Name)
}

// scoreArtist falls back to the composer, because plenty of transcriptions
// fill in <Music> and leave <Artist> empty.
func scoreArtist(s scoreNode) string {
	if a := strings.TrimSpace(s.Artist); a != "" {
		return a
	}
	return strings.TrimSpace(s.Music)
}
