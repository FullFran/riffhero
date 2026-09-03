// Package midi imports Standard MIDI Files into RiffHero's normalized score
// model. It reads formats 0, 1 and 2, both common MIDI-file division schemes,
// running status, and the handful of meta events that matter for a practice
// score: tempo, time signature, and naming.
package midi

import (
	"fmt"
	"os"
	"sort"

	"github.com/FullFran/riffhero/internal/practice"
)

// Parse reads a Standard MIDI File held in memory.
func Parse(data []byte, clock practice.Clock) (*practice.Song, error) {
	r := &reader{data: data}
	_, division, err := parseHeader(r)
	if err != nil {
		return nil, err
	}

	var tracks []parsedTrack
	var tempoChanges []tempoChange
	var sigChanges []sigChange
	tempoTrackIndex := -1

	for r.remaining() >= 8 {
		id, err := r.readBytes(4)
		if err != nil {
			return nil, fmt.Errorf("midi: chunk header: %w", err)
		}
		length, err := r.readUint32()
		if err != nil {
			return nil, fmt.Errorf("midi: chunk length: %w", err)
		}
		body, err := r.readBytes(int(length))
		if err != nil {
			return nil, fmt.Errorf("midi: truncated %q chunk: %w", id, err)
		}

		if string(id) != "MTrk" {
			// The spec reserves unknown chunk types for future extensions;
			// skipping them by their declared length is the correct thing
			// to do, not an error.
			continue
		}

		before := len(tempoChanges)
		pt, err := parseTrack(body, &tempoChanges, &sigChanges)
		if err != nil {
			return nil, fmt.Errorf("midi: track %d: %w", len(tracks), err)
		}
		if tempoTrackIndex < 0 && len(tempoChanges) > before {
			tempoTrackIndex = len(tracks)
		}
		tracks = append(tracks, pt)
	}

	sort.SliceStable(tempoChanges, func(i, j int) bool { return tempoChanges[i].tick < tempoChanges[j].tick })
	sort.SliceStable(sigChanges, func(i, j int) bool { return sigChanges[i].tick < sigChanges[j].tick })

	tc := newTickClock(division, tempoChanges)
	songEndTick := lastTick(tracks, tempoChanges, sigChanges)

	grid, err := buildGrid(clock, tc, tempoChanges, sigChanges, songEndTick)
	if err != nil {
		return nil, err
	}

	song := &practice.Song{Clock: clock}
	song.Grid = grid
	song.Title = titleFrom(tracks, tempoTrackIndex)
	song.Tracks = buildTracks(tracks, tc, clock)
	song.Normalize()
	return song, nil
}

// ParseFile reads a Standard MIDI File from disk.
func ParseFile(path string, clock practice.Clock) (*practice.Song, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("midi: %w", err)
	}
	song, err := Parse(data, clock)
	if err != nil {
		return nil, err
	}
	song.Source = path
	return song, nil
}

// lastTick is the latest tick anything in the file touches: the last tempo
// or time-signature change, or the end of the last note, whichever comes
// last. It anchors the grid's final section so the grid always reaches the
// end of the song even when the file's last MTrk chunk ends earlier (a
// common shape: a track's last event is its own End of Track, with no note
// or meta event anywhere near the true musical end).
func lastTick(tracks []parsedTrack, tempoChanges []tempoChange, sigChanges []sigChange) int64 {
	var last int64
	for _, c := range tempoChanges {
		if c.tick > last {
			last = c.tick
		}
	}
	for _, c := range sigChanges {
		if c.tick > last {
			last = c.tick
		}
	}
	for _, t := range tracks {
		for _, n := range t.notes {
			if n.endTick > last {
				last = n.endTick
			}
		}
	}
	return last
}

// titleFrom picks the song title from the tempo track: the track that
// carried the first Set Tempo event, or track 0 when none did. Format 1
// files conventionally put both the tempo map and the song's own name on
// that track even when it has no notes of its own.
func titleFrom(tracks []parsedTrack, tempoTrackIndex int) string {
	if tempoTrackIndex < 0 && len(tracks) > 0 {
		tempoTrackIndex = 0
	}
	if tempoTrackIndex < 0 || tempoTrackIndex >= len(tracks) {
		return ""
	}
	t := tracks[tempoTrackIndex]
	if t.name != "" {
		return t.name
	}
	return t.text
}

// buildTracks turns every MIDI track that ended up with at least one note
// into a practice.Track. A track that had notes only on the percussion
// channel, or none at all, contributes nothing — an empty Track would just
// be dead weight in the UI's track picker.
func buildTracks(tracks []parsedTrack, tc tickClock, clock practice.Clock) []practice.Track {
	var out []practice.Track
	for i, pt := range tracks {
		if len(pt.notes) == 0 {
			continue
		}

		name := pt.name
		if name == "" {
			name = fmt.Sprintf("Track %d", i+1)
		}
		instrument := pt.instrument
		if instrument == "" && pt.hasProgram {
			instrument = gmProgramName(pt.firstProgram)
		}

		fb := practice.NewFretboard(practice.StandardTuning)
		notes := make([]practice.Note, 0, len(pt.notes))
		for _, rn := range pt.notes {
			midi, str, fret, ok := fb.PlaceOrTranspose(rn.midi)
			if !ok {
				continue // genuinely outside the guitar's range; nothing to place it at
			}
			start := tc.frame(clock, rn.startTick)
			notes = append(notes, practice.Note{
				Start:    start,
				Duration: tc.frame(clock, rn.endTick) - start,
				MIDI:     midi,
				String:   str,
				Fret:     fret,
			})
		}

		out = append(out, practice.Track{
			Name:       name,
			Instrument: instrument,
			Tuning:     practice.StandardTuning,
			Notes:      notes,
		})
	}
	return out
}
