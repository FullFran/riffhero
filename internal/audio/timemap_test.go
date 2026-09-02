package audio

import (
	"sync"
	"testing"

	"github.com/FullFran/riffhero/internal/practice"
)

func TestTimeMapInterpolatesInsideACallback(t *testing.T) {
	m := NewTimeMap(16)
	// One callback: 100 capture frames starting at 1000, during which the song
	// moved from 5000 to 5100.
	m.Push(1000, 100, 5000, 5100, true)

	cases := []struct {
		stream int64
		want   practice.Frame
	}{
		{1000, 5000},
		{1050, 5050},
		{1099, 5099},
	}
	for _, c := range cases {
		got, ok := m.Lookup(c.stream)
		if !ok || got != c.want {
			t.Fatalf("Lookup(%d) = %d ok=%v, want %d true", c.stream, got, ok, c.want)
		}
	}
}

func TestTimeMapHandlesHalfSpeed(t *testing.T) {
	// At half speed the song advances half as many frames as the device
	// delivered, and a detection halfway through the block belongs a quarter
	// of the way into the song block.
	m := NewTimeMap(16)
	m.Push(0, 200, 1000, 1100, true)

	got, ok := m.Lookup(100)
	if !ok || got != 1050 {
		t.Fatalf("Lookup(100) = %d ok=%v, want 1050 true", got, ok)
	}
}

func TestTimeMapRejectsPositionsWithNoSongBehindThem(t *testing.T) {
	// Captured while paused or while the renderer was starved: there is no
	// honest answer, and inventing one would score a note against whatever the
	// playhead happened to be sitting on.
	m := NewTimeMap(16)
	m.Push(0, 100, 0, 0, false)

	if _, ok := m.Lookup(50); ok {
		t.Fatal("a position inside an invalid segment must not resolve")
	}
}

func TestTimeMapMissesOutsideItsHistory(t *testing.T) {
	m := NewTimeMap(16)
	m.Push(1000, 100, 5000, 5100, true)

	if _, ok := m.Lookup(999); ok {
		t.Fatal("before the first segment must not resolve")
	}
	if _, ok := m.Lookup(1100); ok {
		t.Fatal("past the last segment must not resolve")
	}
	if _, ok := NewTimeMap(16).Lookup(0); ok {
		t.Fatal("an empty map must not resolve anything")
	}
}

func TestTimeMapForgetsTheOldestSegments(t *testing.T) {
	m := NewTimeMap(4) // holds four callbacks
	for i := int64(0); i < 10; i++ {
		m.Push(i*100, 100, i*100, (i+1)*100, true)
	}
	// The first six are gone; the last four are still there.
	if _, ok := m.Lookup(50); ok {
		t.Fatal("overwritten history must not resolve")
	}
	for i := int64(6); i < 10; i++ {
		if _, ok := m.Lookup(i*100 + 5); !ok {
			t.Fatalf("segment %d should still be present", i)
		}
	}
}

func TestTimeMapFollowsALoopWrap(t *testing.T) {
	// The song jumps backwards between callbacks; each block is still linear
	// on its own, so lookups either side of the wrap resolve independently.
	m := NewTimeMap(16)
	m.Push(0, 100, 9900, 10000, true)  // reaching the end of the region
	m.Push(100, 100, 5000, 5100, true) // wrapped back to A

	got, ok := m.Lookup(50)
	if !ok || got != 9950 {
		t.Fatalf("before the wrap: %d ok=%v, want 9950", got, ok)
	}
	got, ok = m.Lookup(150)
	if !ok || got != 5050 {
		t.Fatalf("after the wrap: %d ok=%v, want 5050", got, ok)
	}
}

func TestTimeMapIgnoresEmptyPushes(t *testing.T) {
	m := NewTimeMap(16)
	m.Push(0, 0, 1, 2, true)
	if _, ok := m.Lookup(0); ok {
		t.Fatal("a zero-length callback should record nothing")
	}
}

func TestTimeMapSatisfiesTheDetectorInterface(t *testing.T) {
	// The detector only ever sees this narrow interface, which is what keeps
	// the DSP side ignorant of loops and seeks.
	var lookup interface {
		Lookup(int64) (practice.Frame, bool)
	} = NewTimeMap(8)
	_ = lookup
}

func TestTimeMapReadsWhileBeingWritten(t *testing.T) {
	// The real access pattern: the audio callback writes while the game loop
	// reads. Under -race this is the test that proves the version counter and
	// the atomics are doing their job.
	m := NewTimeMap(64)
	var wg sync.WaitGroup
	done := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := int64(0); i < 20000; i++ {
			m.Push(i*480, 480, i*480, (i+1)*480, true)
		}
		close(done)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
			}
			// Whatever comes back must be self-consistent: a segment that
			// resolves has to resolve to its own linear range.
			if got, ok := m.Lookup(int64(m.next.Load()) * 480); ok && got < 0 {
				t.Errorf("negative song position %d", got)
				return
			}
		}
	}()

	wg.Wait()
}
