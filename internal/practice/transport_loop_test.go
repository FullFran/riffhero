package practice

import "testing"

func TestTransportDefaultsToFullSpeed(t *testing.T) {
	tr := NewTransport(testClock(), 48000)
	if got := tr.Speed(); got != 1 {
		t.Fatalf("speed %v, want 1", got)
	}
}

func TestTransportSpeedScalesRealTime(t *testing.T) {
	clock := testClock()
	tr := NewTransport(clock, clock.Frames(60))
	tr.SetSpeed(0.5)
	tr.Play()

	tr.AdvanceSeconds(1)
	if want := clock.Frames(0.5); tr.Position() != want {
		t.Fatalf("half speed moved %d frames in a second, want %d", tr.Position(), want)
	}
}

func TestTransportSpeedIsClamped(t *testing.T) {
	tr := NewTransport(testClock(), 48000)
	tr.SetSpeed(10)
	if got := tr.Speed(); got != SpeedMax {
		t.Fatalf("speed %v, want %v", got, SpeedMax)
	}
	tr.SetSpeed(-1)
	if got := tr.Speed(); got != SpeedMin {
		t.Fatalf("speed %v, want %v", got, SpeedMin)
	}
}

func TestTransportLoopsInsteadOfFinishing(t *testing.T) {
	tr := NewTransport(testClock(), 10000)
	tr.SetLoop(Loop{A: 1000, B: 2000, Enabled: true})
	tr.Seek(1900)
	tr.Play()

	tr.Advance(200)
	if got := tr.Position(); got != 1100 {
		t.Fatalf("position %d, want 1100", got)
	}
	if tr.Laps() != 1 {
		t.Fatalf("laps %d, want 1", tr.Laps())
	}
	if tr.Finished() {
		t.Fatal("a looping transport must never report finished")
	}
	if !tr.Playing() {
		t.Fatal("wrapping must not stop playback")
	}
}

func TestTransportSetLoopPullsAStrandedPlayheadIn(t *testing.T) {
	// Selecting a region that ends before the playhead should start looping
	// now, not after the song runs out.
	tr := NewTransport(testClock(), 10000)
	tr.Seek(5000)
	tr.SetLoop(Loop{A: 1000, B: 2000, Enabled: true})

	if got := tr.Position(); got != 1000 {
		t.Fatalf("position %d, want to be pulled back to 1000", got)
	}
}

func TestTransportRestartGoesToTheLoopStart(t *testing.T) {
	tr := NewTransport(testClock(), 10000)
	tr.Seek(1500)
	tr.SetLoop(Loop{A: 1000, B: 2000, Enabled: true})
	tr.Seek(1800)
	tr.Restart()

	if got := tr.Position(); got != 1000 {
		t.Fatalf("restart went to %d, want the loop start 1000", got)
	}
}

func TestTransportRestartGoesToZeroWithoutALoop(t *testing.T) {
	tr := NewTransport(testClock(), 10000)
	tr.Seek(5000)
	tr.Restart()
	if got := tr.Position(); got != 0 {
		t.Fatalf("restart went to %d, want 0", got)
	}
}

func TestTransportStillStopsAtTheEndWithAnInactiveLoop(t *testing.T) {
	tr := NewTransport(testClock(), 1000)
	tr.SetLoop(Loop{A: 100, B: 200}) // enabled is false
	tr.Play()
	tr.Advance(5000)

	if got := tr.Position(); got != 1000 {
		t.Fatalf("position %d, want to be clamped at the end", got)
	}
	if tr.Playing() {
		t.Fatal("the transport must stop at the end of the song")
	}
	if !tr.Finished() {
		t.Fatal("Finished() should be true at the end")
	}
}

func TestTransportSatisfiesPlayhead(t *testing.T) {
	// The whole point of the interface is that a test's Transport and the
	// audio engine's Player are interchangeable above this line.
	var _ Playhead = NewTransport(testClock(), 1000)
}
