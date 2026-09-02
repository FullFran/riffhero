package practice

import "testing"

func stats(resolved, hits int) SessionStats {
	s := SessionStats{Total: resolved, Resolved: resolved, Perfect: hits, Miss: resolved - hits}
	if resolved > 0 {
		s.Accuracy = float64(hits) / float64(resolved)
	}
	return s
}

func TestProgressionRaisesSpeedOnACleanLap(t *testing.T) {
	next, adj := DefaultProgression.Evaluate(0.7, stats(20, 20))
	if adj != SpeedUp {
		t.Fatalf("adjustment %v, want SpeedUp", adj)
	}
	if next <= 0.7 {
		t.Fatalf("speed went from 0.7 to %v", next)
	}
}

func TestProgressionLowersSpeedOnAMessyLap(t *testing.T) {
	next, adj := DefaultProgression.Evaluate(0.7, stats(20, 10))
	if adj != SlowDown {
		t.Fatalf("adjustment %v, want SlowDown", adj)
	}
	if next >= 0.7 {
		t.Fatalf("speed went from 0.7 to %v", next)
	}
}

func TestProgressionRepeatsInTheMiddleBand(t *testing.T) {
	next, adj := DefaultProgression.Evaluate(0.7, stats(20, 17)) // 85%
	if adj != Repeat || next != 0.7 {
		t.Fatalf("got %v at %v, want Repeat at 0.7", adj, next)
	}
}

func TestProgressionTreatsAnEmptyLapAsNoEvidence(t *testing.T) {
	// Nothing resolved means the player paused or the section was empty.
	// Dragging the speed down while nobody was playing would be nonsense.
	next, adj := DefaultProgression.Evaluate(0.5, SessionStats{})
	if adj != Repeat || next != 0.5 {
		t.Fatalf("got %v at %v, want Repeat at 0.5", adj, next)
	}
}

func TestProgressionStopsAtTheCeilingAndTheFloor(t *testing.T) {
	next, adj := DefaultProgression.Evaluate(SpeedMax, stats(10, 10))
	if next != SpeedMax || adj != Repeat {
		t.Fatalf("at full speed got %v %v, want %v Repeat", next, adj, SpeedMax)
	}
	next, adj = DefaultProgression.Evaluate(SpeedMin, stats(10, 0))
	if next != SpeedMin || adj != Repeat {
		t.Fatalf("at the floor got %v %v, want %v Repeat", next, adj, SpeedMin)
	}
}

func TestProgressionClimbsFromTheFloorToTheCeiling(t *testing.T) {
	// The rule has to terminate: a player who nails every lap must actually
	// arrive at full speed rather than asymptote below it.
	speed := SpeedMin
	for i := 0; i < 100; i++ {
		next, _ := DefaultProgression.Evaluate(speed, stats(10, 10))
		if next == speed {
			break
		}
		speed = next
	}
	if speed != SpeedMax {
		t.Fatalf("clean laps stalled at %v, want %v", speed, SpeedMax)
	}
}

func TestClampSpeedStaysInThePracticeRange(t *testing.T) {
	if got := ClampSpeed(5); got != SpeedMax {
		t.Fatalf("ClampSpeed(5) = %v", got)
	}
	if got := ClampSpeed(0); got != SpeedMin {
		t.Fatalf("ClampSpeed(0) = %v", got)
	}
	if got := ClampSpeed(0.6); got != 0.6 {
		t.Fatalf("ClampSpeed(0.6) = %v", got)
	}
}
