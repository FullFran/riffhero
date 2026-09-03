package dsp

import (
	"sync"
	"testing"
)

func TestRingRoundTrip(t *testing.T) {
	r := NewRing(8)
	if n := r.Write([]float32{1, 2, 3}); n != 3 {
		t.Fatalf("Write = %d, want 3", n)
	}

	out := make([]float32, 4)
	n := r.Read(out)
	if n != 3 {
		t.Fatalf("Read = %d, want 3", n)
	}
	for i, want := range []float32{1, 2, 3} {
		if out[i] != want {
			t.Errorf("out[%d] = %v, want %v", i, out[i], want)
		}
	}
}

func TestRingCapacityRoundsUpToPowerOfTwo(t *testing.T) {
	if got := NewRing(5).Cap(); got != 8 {
		t.Errorf("Cap = %d, want 8", got)
	}
	if got := NewRing(16).Cap(); got != 16 {
		t.Errorf("Cap = %d, want 16", got)
	}
}

// The audio callback must never block, so a full ring drops the newest samples
// and reports how many it kept.
func TestRingWriteDropsWhenFull(t *testing.T) {
	r := NewRing(4)
	if n := r.Write([]float32{1, 2, 3, 4, 5, 6}); n != 4 {
		t.Fatalf("Write = %d, want 4", n)
	}
	if got, want := r.Dropped(), uint64(2); got != want {
		t.Errorf("Dropped = %d, want %d", got, want)
	}

	out := make([]float32, 4)
	if n := r.Read(out); n != 4 {
		t.Fatalf("Read = %d, want 4", n)
	}
	for i, want := range []float32{1, 2, 3, 4} {
		if out[i] != want {
			t.Errorf("out[%d] = %v, want %v", i, out[i], want)
		}
	}
}

// Written counts every sample the producer offered, so the consumer can map a
// block back to an absolute position on the practice timeline.
func TestRingWrittenTracksProducerPosition(t *testing.T) {
	r := NewRing(4)
	r.Write([]float32{1, 2})
	r.Write([]float32{3, 4})
	if got, want := r.Written(), uint64(4); got != want {
		t.Errorf("Written = %d, want %d", got, want)
	}
}

func TestRingWrapsAround(t *testing.T) {
	r := NewRing(4)
	out := make([]float32, 3)

	for round := 0; round < 4; round++ {
		in := []float32{float32(round), float32(round) + 0.5, float32(round) + 0.25}
		if n := r.Write(in); n != 3 {
			t.Fatalf("round %d: Write = %d, want 3", round, n)
		}
		if n := r.Read(out); n != 3 {
			t.Fatalf("round %d: Read = %d, want 3", round, n)
		}
		for i := range in {
			if out[i] != in[i] {
				t.Fatalf("round %d: out[%d] = %v, want %v", round, i, out[i], in[i])
			}
		}
	}
}

func TestRingEmptyReadReturnsZero(t *testing.T) {
	if n := NewRing(4).Read(make([]float32, 2)); n != 0 {
		t.Errorf("Read on empty ring = %d, want 0", n)
	}
}

// One producer, one consumer, no locks: every sample must arrive exactly once
// and in order. Run with -race.
func TestRingSingleProducerSingleConsumer(t *testing.T) {
	const total = 100000
	r := NewRing(1024)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		block := make([]float32, 64)
		for sent := 0; sent < total; {
			n := 0
			for n < len(block) && sent+n < total {
				block[n] = float32(sent + n)
				n++
			}
			written := r.Write(block[:n])
			sent += written
		}
	}()

	got := 0
	out := make([]float32, 128)
	for got < total {
		n := r.Read(out)
		for i := 0; i < n; i++ {
			if out[i] != float32(got) {
				t.Fatalf("sample %d = %v, want %v", got, out[i], float32(got))
			}
			got++
		}
	}
	wg.Wait()
}
