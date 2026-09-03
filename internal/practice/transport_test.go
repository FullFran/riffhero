package practice

import "testing"

func TestTransportStartsStoppedAtZero(t *testing.T) {
	tr := NewTransport(Clock{SampleRate: 48000}, 1000)

	if tr.Position() != 0 {
		t.Fatalf("position=%d want=0", tr.Position())
	}
	if tr.Playing() {
		t.Fatal("transport must start paused")
	}

	tr.Advance(500)
	if tr.Position() != 0 {
		t.Fatalf("paused transport advanced to %d", tr.Position())
	}
}

func TestTransportAdvancesOnlyWhilePlaying(t *testing.T) {
	tr := NewTransport(Clock{SampleRate: 48000}, 1000)

	tr.Play()
	tr.Advance(300)
	tr.Advance(200)
	if tr.Position() != 500 {
		t.Fatalf("position=%d want=500", tr.Position())
	}

	tr.Pause()
	tr.Advance(400)
	if tr.Position() != 500 {
		t.Fatalf("position after pause=%d want=500", tr.Position())
	}
}

func TestTransportIgnoresNonPositiveAdvance(t *testing.T) {
	tr := NewTransport(Clock{SampleRate: 48000}, 1000)
	tr.Play()
	tr.Advance(100)

	tr.Advance(-50)
	tr.Advance(0)
	if tr.Position() != 100 {
		t.Fatalf("position=%d want=100", tr.Position())
	}
}

func TestTransportClampsAtEndAndStops(t *testing.T) {
	tr := NewTransport(Clock{SampleRate: 48000}, 1000)
	tr.Play()

	tr.Advance(5000)
	if tr.Position() != 1000 {
		t.Fatalf("position=%d want=1000 (clamped to end)", tr.Position())
	}
	if tr.Playing() {
		t.Fatal("transport must stop at end of song")
	}
	if !tr.Finished() {
		t.Fatal("transport must report finished at end of song")
	}
}

func TestTransportSeekAndRestart(t *testing.T) {
	tr := NewTransport(Clock{SampleRate: 48000}, 1000)

	tr.Seek(700)
	if tr.Position() != 700 {
		t.Fatalf("position=%d want=700", tr.Position())
	}

	tr.Seek(-10)
	if tr.Position() != 0 {
		t.Fatalf("negative seek must clamp to 0, got %d", tr.Position())
	}

	tr.Seek(9999)
	if tr.Position() != 1000 {
		t.Fatalf("overshooting seek must clamp to end, got %d", tr.Position())
	}

	tr.Play()
	tr.Restart()
	if tr.Position() != 0 {
		t.Fatalf("restart position=%d want=0", tr.Position())
	}
	if !tr.Playing() {
		t.Fatal("restart must preserve playing state")
	}
}

func TestTransportAdvanceSeconds(t *testing.T) {
	clock := Clock{SampleRate: 48000}
	tr := NewTransport(clock, clock.Frames(10))
	tr.Play()

	tr.AdvanceSeconds(0.5)
	if got, want := tr.Position(), clock.Frames(0.5); got != want {
		t.Fatalf("position=%d want=%d", got, want)
	}
}
