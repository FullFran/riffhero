package audio

import (
	"math/bits"
	"sync/atomic"
)

// stamp is what one output frame knows about itself: the song frame it plays
// at, and which seek generation produced it.
//
// The generation is the whole reason the playhead can be trusted across a
// seek. Audio for the old position is already sitting in the ring when the
// user jumps somewhere else; without a generation the callback would keep
// reporting those stale positions for as long as they take to drain, and the
// scrolling view would slide backwards after every seek.
type stamp struct {
	pos int64
	gen uint32
}

// outRing is a bounded single-producer/single-consumer queue of stereo output
// frames, each carrying the song position it belongs to.
//
// The producer is the render goroutine, which may take as long as it likes.
// The consumer is the audio callback, which may not: pop copies and returns,
// and an empty ring yields silence rather than a wait. That silence is also
// what keeps the clock honest — the song position only moves for frames that
// were actually handed to the device, so a starved renderer stalls the score
// instead of letting it race ahead of what the player can hear.
type outRing struct {
	audio  []float32 // interleaved stereo, 2 per frame
	stamps []stamp

	mask  uint64
	write atomic.Uint64
	read  atomic.Uint64

	underruns atomic.Uint64
}

func newOutRing(frames int) *outRing {
	if frames < 2 {
		frames = 2
	}
	size := 1 << bits.Len(uint(frames-1))
	return &outRing{
		audio:  make([]float32, size*2),
		stamps: make([]stamp, size),
		mask:   uint64(size) - 1,
	}
}

// Cap is the ring's real capacity in frames, rounded up to a power of two.
func (r *outRing) Cap() int { return len(r.stamps) }

// Len is how many frames are queued.
func (r *outRing) Len() int { return int(r.write.Load() - r.read.Load()) }

// Writable is how many frames the producer may push right now.
func (r *outRing) Writable() int { return r.Cap() - r.Len() }

// Underruns counts the callback invocations the ring could not fill.
func (r *outRing) Underruns() uint64 { return r.underruns.Load() }

// Push copies frames of interleaved stereo audio and their stamps. It writes
// whole frames only and returns how many it took.
func (r *outRing) Push(audio []float32, stamps []stamp) int {
	n := len(stamps)
	if got := len(audio) / 2; got < n {
		n = got
	}
	if free := r.Writable(); n > free {
		n = free
	}
	if n <= 0 {
		return 0
	}

	w := r.write.Load()
	for i := 0; i < n; i++ {
		idx := (w + uint64(i)) & r.mask
		r.audio[idx*2] = audio[i*2]
		r.audio[idx*2+1] = audio[i*2+1]
		r.stamps[idx] = stamps[i]
	}
	r.write.Store(w + uint64(n))
	return n
}

// Pop fills dst with up to n frames of interleaved stereo and reports the
// stamps of the first and last frame it wrote. Frames the ring could not
// supply are silenced, so the caller can hand dst straight to the device.
//
// Both ends of the block are reported because the caller has to record how far
// the song moved during this callback, not just where it ended up.
//
// Called from the audio callback: it allocates nothing and never waits.
func (r *outRing) Pop(dst []float32, n int) (got int, first, last stamp, ok bool) {
	if n*2 > len(dst) {
		n = len(dst) / 2
	}
	if n <= 0 {
		return 0, stamp{}, stamp{}, false
	}

	rd := r.read.Load()
	avail := int(r.write.Load() - rd)
	got = n
	if avail < got {
		got = avail
	}

	for i := 0; i < got; i++ {
		idx := (rd + uint64(i)) & r.mask
		dst[i*2] = r.audio[idx*2]
		dst[i*2+1] = r.audio[idx*2+1]
	}
	if got > 0 {
		first = r.stamps[rd&r.mask]
		last = r.stamps[(rd+uint64(got-1))&r.mask]
		ok = true
		r.read.Store(rd + uint64(got))
	}

	for i := got * 2; i < n*2; i++ {
		dst[i] = 0
	}
	if got < n {
		r.underruns.Add(1)
	}
	return got, first, last, ok
}
