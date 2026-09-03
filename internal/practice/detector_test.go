package practice

import "testing"

func TestScriptedDetectorEmitsEachEventOnceInOrder(t *testing.T) {
	det := NewScriptedDetector([]DetectedNote{
		{Onset: 100, MIDI: 60},
		{Onset: 300, MIDI: 62},
		{Onset: 900, MIDI: 64},
	})

	if got := det.Poll(50); len(got) != 0 {
		t.Fatalf("early poll returned %d events", len(got))
	}

	got := det.Poll(300)
	if len(got) != 2 {
		t.Fatalf("poll(300) returned %d events want 2", len(got))
	}
	if got[0].Onset != 100 || got[1].Onset != 300 {
		t.Fatalf("events out of order: %+v", got)
	}

	if again := det.Poll(300); len(again) != 0 {
		t.Fatalf("re-poll replayed %d events", len(again))
	}

	if last := det.Poll(1000); len(last) != 1 || last[0].Onset != 900 {
		t.Fatalf("final poll=%+v", last)
	}
}

func TestScriptedDetectorSortsInput(t *testing.T) {
	det := NewScriptedDetector([]DetectedNote{
		{Onset: 900, MIDI: 64},
		{Onset: 100, MIDI: 60},
	})

	got := det.Poll(1000)
	if len(got) != 2 || got[0].Onset != 100 || got[1].Onset != 900 {
		t.Fatalf("unsorted output: %+v", got)
	}
}

func TestPerformApplysDeviations(t *testing.T) {
	notes := []Note{
		{Start: 1000, MIDI: 60},
		{Start: 2000, MIDI: 62},
		{Start: 3000, MIDI: 64},
	}

	got := Perform(notes, []Deviation{
		{Offset: 50, Cents: 3},
		{Skip: true},
		{Offset: -20, Semitones: 1, Cents: -4},
	})

	if len(got) != 2 {
		t.Fatalf("len=%d want 2 (one note skipped)", len(got))
	}
	if got[0] != (DetectedNote{Onset: 1050, MIDI: 60, CentsError: 3, Confidence: 1}) {
		t.Fatalf("first=%+v", got[0])
	}
	if got[1] != (DetectedNote{Onset: 2980, MIDI: 65, CentsError: -4, Confidence: 1}) {
		t.Fatalf("second=%+v", got[1])
	}
}

func TestPerformWithoutPlanIsExact(t *testing.T) {
	notes := []Note{{Start: 1000, MIDI: 60}, {Start: 2000, MIDI: 62}}

	got := Perform(notes, nil)
	if len(got) != 2 {
		t.Fatalf("len=%d want 2", len(got))
	}
	for i, d := range got {
		if d.Onset != notes[i].Start || d.MIDI != notes[i].MIDI || d.CentsError != 0 {
			t.Fatalf("event %d = %+v, want an exact performance of %+v", i, d, notes[i])
		}
	}
}
