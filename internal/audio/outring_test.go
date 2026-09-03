package audio

import "testing"

func stereo(frames int, value float32) []float32 {
	out := make([]float32, frames*2)
	for i := range out {
		out[i] = value
	}
	return out
}

func stampsFrom(start int64, n int, gen uint32) []stamp {
	out := make([]stamp, n)
	for i := range out {
		out[i] = stamp{pos: start + int64(i), gen: gen}
	}
	return out
}

func TestOutRingRoundsCapacityUp(t *testing.T) {
	if got := newOutRing(1000).Cap(); got != 1024 {
		t.Fatalf("capacity %d, want 1024", got)
	}
}

func TestOutRingReportsBothEndsOfTheBlock(t *testing.T) {
	// The callback has to know how far the song moved during it, not only
	// where it ended up, so both stamps come back.
	r := newOutRing(64)
	r.Push(stereo(10, 0.5), stampsFrom(1000, 10, 7))

	dst := make([]float32, 20)
	got, first, last, ok := r.Pop(dst, 10)
	if !ok || got != 10 {
		t.Fatalf("popped %d ok=%v, want 10 true", got, ok)
	}
	if first.pos != 1000 || last.pos != 1009 {
		t.Fatalf("stamps %d..%d, want 1000..1009", first.pos, last.pos)
	}
	if first.gen != 7 || last.gen != 7 {
		t.Fatalf("generations %d/%d, want 7", first.gen, last.gen)
	}
	for i, v := range dst {
		if v != 0.5 {
			t.Fatalf("sample %d = %v, want 0.5", i, v)
		}
	}
}

func TestOutRingSilencesWhatItCannotSupply(t *testing.T) {
	// An underrun must hand the device silence, not stale audio: the buffer it
	// gives us is reused and would otherwise repeat the last block.
	r := newOutRing(64)
	r.Push(stereo(4, 1), stampsFrom(0, 4, 0))

	dst := stereo(10, 9) // pre-filled with garbage
	got, _, _, ok := r.Pop(dst, 10)
	if got != 4 || !ok {
		t.Fatalf("popped %d ok=%v, want 4 true", got, ok)
	}
	for i := 8; i < 20; i++ {
		if dst[i] != 0 {
			t.Fatalf("sample %d = %v, want silence", i, dst[i])
		}
	}
	if r.Underruns() != 1 {
		t.Fatalf("underruns %d, want 1", r.Underruns())
	}
}

func TestOutRingEmptyPopIsAllSilence(t *testing.T) {
	r := newOutRing(64)
	dst := stereo(8, 3)
	got, _, _, ok := r.Pop(dst, 8)
	if got != 0 || ok {
		t.Fatalf("popped %d ok=%v, want 0 false", got, ok)
	}
	for i, v := range dst {
		if v != 0 {
			t.Fatalf("sample %d = %v, want silence", i, v)
		}
	}
}

func TestOutRingRefusesToOverwriteUnreadFrames(t *testing.T) {
	r := newOutRing(8)
	if n := r.Push(stereo(20, 1), stampsFrom(0, 20, 0)); n != 8 {
		t.Fatalf("accepted %d frames into a ring of 8", n)
	}
	if got := r.Writable(); got != 0 {
		t.Fatalf("writable %d, want 0", got)
	}
	if n := r.Push(stereo(1, 1), stampsFrom(99, 1, 0)); n != 0 {
		t.Fatalf("accepted %d frames into a full ring", n)
	}
}

func TestOutRingWrapsAroundTheBackingArray(t *testing.T) {
	r := newOutRing(8)
	dst := make([]float32, 16)

	// Three passes over an eight-frame ring: indices wrap and the data must
	// still come back in order.
	next := int64(0)
	for pass := 0; pass < 3; pass++ {
		r.Push(stereo(6, float32(pass)), stampsFrom(next, 6, 0))
		got, first, last, _ := r.Pop(dst, 6)
		if got != 6 {
			t.Fatalf("pass %d popped %d", pass, got)
		}
		if first.pos != next || last.pos != next+5 {
			t.Fatalf("pass %d stamps %d..%d, want %d..%d", pass, first.pos, last.pos, next, next+5)
		}
		for i := 0; i < 12; i++ {
			if dst[i] != float32(pass) {
				t.Fatalf("pass %d sample %d = %v", pass, i, dst[i])
			}
		}
		next += 6
	}
}

func TestOutRingPushRespectsTheShorterOfAudioAndStamps(t *testing.T) {
	r := newOutRing(64)
	if n := r.Push(stereo(4, 1), stampsFrom(0, 10, 0)); n != 4 {
		t.Fatalf("pushed %d, want 4", n)
	}
	if n := r.Push(stereo(10, 1), stampsFrom(0, 2, 0)); n != 2 {
		t.Fatalf("pushed %d, want 2", n)
	}
}

func TestOutRingPopClampsToTheDestination(t *testing.T) {
	r := newOutRing(64)
	r.Push(stereo(10, 1), stampsFrom(0, 10, 0))

	dst := make([]float32, 6) // room for three frames
	got, _, _, _ := r.Pop(dst, 10)
	if got != 3 {
		t.Fatalf("popped %d frames into a three-frame buffer", got)
	}
}
