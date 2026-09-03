package practice

import "testing"

func TestSongNormalizeSortsAndDropsRubbish(t *testing.T) {
	song := &Song{
		Tracks: []Track{{Notes: []Note{
			{Start: 300, Duration: 100, MIDI: 60},
			{Start: 100, Duration: 100, MIDI: 64},
			{Start: 100, Duration: 100, MIDI: 60}, // same start, lower pitch first
			{Start: 200, Duration: 0, MIDI: 60},   // zero length
			{Start: -5, Duration: 100, MIDI: 60},  // before the timeline
			{Start: 400, Duration: 100, MIDI: 0},  // no pitch
		}}},
	}
	song.Normalize()

	got := song.Tracks[0].Notes
	if len(got) != 3 {
		t.Fatalf("kept %d notes, want 3: %+v", len(got), got)
	}
	want := []Note{
		{Start: 100, Duration: 100, MIDI: 60},
		{Start: 100, Duration: 100, MIDI: 64},
		{Start: 300, Duration: 100, MIDI: 60},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("note %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestSongEndTakesTheLaterOfNotesAndBars(t *testing.T) {
	clock := testClock()
	song := &Song{
		Clock:  clock,
		Grid:   BuildGrid(clock, 0, []Section{{BPM: 120, Sig: CommonTime, Bars: 1}}),
		Tracks: []Track{{Notes: []Note{{Start: 100, Duration: 200, MIDI: 60}}}},
	}
	// A written rest at the end is still part of the song, so the bar wins.
	if got, want := song.End(), clock.Frames(2); got != want {
		t.Fatalf("end %d, want the bar end %d", got, want)
	}

	song.Tracks[0].Notes[0] = Note{Start: clock.Frames(5), Duration: 100, MIDI: 60}
	if got, want := song.End(), clock.Frames(5)+100; got != want {
		t.Fatalf("end %d, want the note end %d", got, want)
	}
}

func TestGuitarTrackPrefersAGuitar(t *testing.T) {
	note := []Note{{Start: 0, Duration: 10, MIDI: 60}}
	song := &Song{Tracks: []Track{
		{Name: "Drums", Notes: note},
		{Name: "Bass", Notes: note},
		{Name: "Rhythm Guitar", Notes: note},
	}}
	if got := song.GuitarTrack(); got != 2 {
		t.Fatalf("picked track %d, want 2", got)
	}
}

func TestGuitarTrackMatchesTheInstrumentToo(t *testing.T) {
	note := []Note{{Start: 0, Duration: 10, MIDI: 60}}
	song := &Song{Tracks: []Track{
		{Name: "Track 1", Notes: note},
		{Name: "Track 2", Instrument: "Overdriven GUITAR", Notes: note},
	}}
	if got := song.GuitarTrack(); got != 1 {
		t.Fatalf("picked track %d, want 1", got)
	}
}

func TestGuitarTrackFallsBackToTheFirstTrackWithNotes(t *testing.T) {
	note := []Note{{Start: 0, Duration: 10, MIDI: 60}}
	song := &Song{Tracks: []Track{
		{Name: "Empty"},
		{Name: "Piano", Notes: note},
	}}
	if got := song.GuitarTrack(); got != 1 {
		t.Fatalf("picked track %d, want 1", got)
	}
	if got := (&Song{}).GuitarTrack(); got != -1 {
		t.Fatalf("an empty song returned %d, want -1", got)
	}
}

func TestEventsGroupsSimultaneousNotes(t *testing.T) {
	notes := []Note{
		{Start: 0, MIDI: 40},
		{Start: 5, MIDI: 47}, // struck with the first
		{Start: 8, MIDI: 52}, // still the same strum
		{Start: 500, MIDI: 60},
	}
	events := Events(notes, 10)

	if len(events) != 2 {
		t.Fatalf("got %d events, want 2: %+v", len(events), events)
	}
	if len(events[0].Notes) != 3 || events[0].Start != 0 {
		t.Fatalf("first event = %+v, want three notes at 0", events[0])
	}
	if len(events[1].Notes) != 1 || events[1].Start != 500 {
		t.Fatalf("second event = %+v, want one note at 500", events[1])
	}
}

func TestEventsUsesTheEventStartAsTheReference(t *testing.T) {
	// Every note is compared to the event's own start. Chaining note-to-note
	// instead would let a fast run slide into one giant chord, because each
	// note is always within tolerance of the one before it.
	notes := []Note{{Start: 0}, {Start: 8}, {Start: 16}, {Start: 24}}
	events := Events(notes, 10)
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2 (a chained rule would give 1)", len(events))
	}
}

func TestEventsOnNothing(t *testing.T) {
	if got := Events(nil, 10); got != nil {
		t.Fatalf("got %+v, want nil", got)
	}
}

func TestNotesInSelectsTheRegion(t *testing.T) {
	notes := []Note{{Start: 0}, {Start: 100}, {Start: 200}, {Start: 300}}
	got := NotesIn(notes, 100, 300)
	if len(got) != 2 || got[0].Start != 100 || got[1].Start != 200 {
		t.Fatalf("got %+v, want the notes at 100 and 200", got)
	}
}
