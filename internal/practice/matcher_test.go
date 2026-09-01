package practice

import "testing"

func TestMatchSingle(t *testing.T) {
	clock := Clock{SampleRate: 48000}
	expected := Note{Start: clock.Frames(1), MIDI: 69}
	windows := TimingWindows{
		Perfect: clock.Frames(0.050),
		Good:    clock.Frames(0.100),
	}

	tests := []struct {
		name string
		got  DetectedNote
		want Rating
	}{
		{"perfect", DetectedNote{Onset: expected.Start + clock.Frames(0.030), MIDI: 69, CentsError: 4}, Perfect},
		{"good", DetectedNote{Onset: expected.Start + clock.Frames(0.080), MIDI: 69, CentsError: 8}, Good},
		{"late", DetectedNote{Onset: expected.Start + clock.Frames(0.150), MIDI: 69, CentsError: 2}, Miss},
		{"wrong pitch", DetectedNote{Onset: expected.Start, MIDI: 70}, Miss},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MatchSingle(expected, tt.got, windows, 25).Rating; got != tt.want {
				t.Fatalf("rating=%v want=%v", got, tt.want)
			}
		})
	}
}
