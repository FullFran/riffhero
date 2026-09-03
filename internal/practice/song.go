package practice

// Standard tuning, indexed by tab string number: 1 is the high E, 6 the low E.
var standardTuning = [6]uint8{64, 59, 55, 50, 45, 40}

// MIDIForStringFret resolves a tab position into a MIDI note number.
// The string number is clamped into the 1..6 tab range.
func MIDIForStringFret(str, fret uint8) uint8 {
	return standardTuning[clampString(str)-1] + fret
}

func clampString(str uint8) uint8 {
	switch {
	case str < 1:
		return 1
	case str > 6:
		return 6
	default:
		return str
	}
}

// Synthetic song constants. Phase 0 has no importer yet, so the score is
// generated in code and must stay byte-for-byte reproducible.
const (
	SyntheticSongNoteCount     = 20
	SyntheticSongBPM           = 100
	SyntheticSongLeadInSeconds = 1.0
)

// A minor pentatonic box at the 5th fret, ascending then descending.
var syntheticSongPositions = [SyntheticSongNoteCount][2]uint8{
	{6, 5}, {6, 8}, {5, 5}, {5, 7}, {4, 5}, {4, 7},
	{3, 5}, {3, 7}, {2, 5}, {2, 8}, {1, 5}, {1, 8},
	{1, 5}, {2, 8}, {2, 5}, {3, 7}, {3, 5}, {4, 7}, {4, 5}, {5, 7},
}

// SyntheticSong builds the Phase 0 practice phrase: 20 eighth notes at
// SyntheticSongBPM, starting after a lead-in. It is a pure function of the
// clock, so the same clock always yields the same score.
func SyntheticSong(clock Clock) []Note {
	step := clock.Frames(60.0 / SyntheticSongBPM / 2)
	start := clock.Frames(SyntheticSongLeadInSeconds)

	notes := make([]Note, 0, len(syntheticSongPositions))
	for i, pos := range syntheticSongPositions {
		str, fret := pos[0], pos[1]
		notes = append(notes, Note{
			Start:    start + Frame(i)*step,
			Duration: step,
			MIDI:     MIDIForStringFret(str, fret),
			String:   str,
			Fret:     fret,
		})
	}
	return notes
}

// SongEnd returns the frame at which the last note stops sounding.
func SongEnd(notes []Note) Frame {
	var end Frame
	for _, n := range notes {
		if stop := n.Start + n.Duration; stop > end {
			end = stop
		}
	}
	return end
}

// SyntheticScore wraps the Phase 0 phrase in the full song model, so the app
// has something real to run against with no file at all. It is the demo, the
// smoke test and the thing to check a new guitar input against, and it must
// stay a pure function of the clock.
func SyntheticScore(clock Clock) *Song {
	notes := SyntheticSong(clock)

	// Twenty eighth notes is ten quarters: two and a half bars of 4/4, rounded
	// up so the last note is inside the grid rather than hanging off the end.
	song := &Song{
		Title:  "Pentatonic warm-up",
		Artist: "RiffHero",
		Source: "built in",
		Clock:  clock,
		Grid: BuildGrid(clock, clock.Frames(SyntheticSongLeadInSeconds), []Section{
			{BPM: SyntheticSongBPM, Sig: CommonTime, Bars: 3},
		}),
		Tracks: []Track{{
			Name:       "Guitar",
			Instrument: "Clean guitar",
			Tuning:     StandardTuning,
			Notes:      notes,
		}},
	}
	song.Normalize()
	return song
}
