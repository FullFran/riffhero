package practice

import "testing"

func TestSyntheticSongIsDeterministic(t *testing.T) {
	clock := Clock{SampleRate: 48000}

	a := SyntheticSong(clock)
	b := SyntheticSong(clock)

	if len(a) != SyntheticSongNoteCount {
		t.Fatalf("len=%d want=%d", len(a), SyntheticSongNoteCount)
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("note %d differs between calls: %+v vs %+v", i, a[i], b[i])
		}
	}
}

func TestSyntheticSongIsOrderedAndPlayable(t *testing.T) {
	clock := Clock{SampleRate: 48000}
	notes := SyntheticSong(clock)

	for i, n := range notes {
		if n.String < 1 || n.String > 6 {
			t.Fatalf("note %d has string=%d outside 1..6", i, n.String)
		}
		if n.Duration <= 0 {
			t.Fatalf("note %d has non-positive duration %d", i, n.Duration)
		}
		want := MIDIForStringFret(n.String, n.Fret)
		if n.MIDI != want {
			t.Fatalf("note %d midi=%d want=%d for string %d fret %d", i, n.MIDI, want, n.String, n.Fret)
		}
		if i > 0 && n.Start <= notes[i-1].Start {
			t.Fatalf("note %d start=%d is not after previous start=%d", i, n.Start, notes[i-1].Start)
		}
	}

	if got := notes[0].Start; got != clock.Frames(SyntheticSongLeadInSeconds) {
		t.Fatalf("lead-in start=%d want=%d", got, clock.Frames(SyntheticSongLeadInSeconds))
	}
}

func TestMIDIForStringFret(t *testing.T) {
	tests := []struct {
		str, fret uint8
		want      uint8
	}{
		{1, 0, 64}, // high E4
		{2, 0, 59}, // B3
		{3, 0, 55}, // G3
		{4, 0, 50}, // D3
		{5, 0, 45}, // A2
		{6, 0, 40}, // low E2
		{6, 5, 45}, // A2 at 5th fret
		{1, 12, 76},
	}
	for _, tt := range tests {
		if got := MIDIForStringFret(tt.str, tt.fret); got != tt.want {
			t.Fatalf("string %d fret %d = %d want %d", tt.str, tt.fret, got, tt.want)
		}
	}
}

func TestSongDuration(t *testing.T) {
	clock := Clock{SampleRate: 48000}
	notes := SyntheticSong(clock)

	last := notes[len(notes)-1]
	if got := SongEnd(notes); got != last.Start+last.Duration {
		t.Fatalf("SongEnd=%d want=%d", got, last.Start+last.Duration)
	}
	if SongEnd(nil) != 0 {
		t.Fatal("SongEnd(nil) must be 0")
	}
}
