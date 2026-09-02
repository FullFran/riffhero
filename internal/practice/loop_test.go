package practice

import "testing"

func TestLoopActiveNeedsARealRegion(t *testing.T) {
	cases := []struct {
		name string
		loop Loop
		want bool
	}{
		{"disabled", Loop{A: 0, B: 100}, false},
		{"empty", Loop{A: 100, B: 100, Enabled: true}, false},
		{"reversed", Loop{A: 200, B: 100, Enabled: true}, false},
		{"negative", Loop{A: -1, B: 100, Enabled: true}, false},
		{"real", Loop{A: 0, B: 100, Enabled: true}, true},
	}
	for _, c := range cases {
		if got := c.loop.Active(); got != c.want {
			t.Fatalf("%s: Active() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestLoopStepIsPlainAdditionWhenInactive(t *testing.T) {
	var l Loop
	next, laps := l.Step(1000, 500)
	if next != 1500 || laps != 0 {
		t.Fatalf("got %d after %d laps, want 1500 after 0", next, laps)
	}
}

func TestLoopStepPlaysIntoTheRegionBeforeWrapping(t *testing.T) {
	// A run that starts before A must play through into the region rather than
	// being yanked forward; only reaching B pulls it back.
	l := Loop{A: 1000, B: 2000, Enabled: true}

	next, laps := l.Step(500, 300)
	if next != 800 || laps != 0 {
		t.Fatalf("before the region: got %d laps %d, want 800 laps 0", next, laps)
	}
	next, laps = l.Step(900, 300)
	if next != 1200 || laps != 0 {
		t.Fatalf("crossing into the region: got %d laps %d, want 1200 laps 0", next, laps)
	}
}

func TestLoopStepWrapsAtB(t *testing.T) {
	l := Loop{A: 1000, B: 2000, Enabled: true}

	next, laps := l.Step(1900, 200)
	if next != 1100 || laps != 1 {
		t.Fatalf("got %d laps %d, want 1100 laps 1", next, laps)
	}
	// Landing exactly on B is a wrap, not a note that plays twice.
	next, laps = l.Step(1900, 100)
	if next != 1000 || laps != 1 {
		t.Fatalf("exactly at B: got %d laps %d, want 1000 laps 1", next, laps)
	}
}

func TestLoopStepSurvivesAJumpBiggerThanTheRegion(t *testing.T) {
	// A stalled render thread can hand the callback more frames than one lap
	// holds. The position must still land inside the region.
	l := Loop{A: 100, B: 200, Enabled: true}

	next, laps := l.Step(150, 1000)
	if next < l.A || next >= l.B {
		t.Fatalf("landed at %d, outside [%d,%d)", next, l.A, l.B)
	}
	// A thousand frames through a hundred-frame region is ten full laps.
	if laps != 10 {
		t.Fatalf("counted %d laps, want 10", laps)
	}
}

func TestLoopContains(t *testing.T) {
	l := Loop{A: 100, B: 200, Enabled: true}
	if !l.Contains(100) || !l.Contains(199) {
		t.Fatal("the region must include A and everything up to B")
	}
	if l.Contains(99) || l.Contains(200) {
		t.Fatal("the region must exclude B and anything before A")
	}
}

func TestLoopLength(t *testing.T) {
	if got := (Loop{A: 100, B: 350, Enabled: true}).Length(); got != 250 {
		t.Fatalf("length %d, want 250", got)
	}
	if got := (Loop{A: 100, B: 350}).Length(); got != 0 {
		t.Fatalf("an inactive loop has no length, got %d", got)
	}
}
