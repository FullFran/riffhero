package dsp

import (
	"math/bits"
	"sync/atomic"
)

// Ring is a bounded single-producer/single-consumer sample queue.
//
// The producer is the real-time audio callback, so Write allocates nothing,
// takes no lock and never blocks: when the consumer falls behind, the newest
// samples are dropped and counted instead of stalling the device. Everything
// expensive — gating, onset detection, pitch estimation — runs on the consumer
// side, off the callback thread.
//
// Capacity is rounded up to a power of two so index wrapping is a mask.
type Ring struct {
	buf  []float32
	mask uint64

	// write is only advanced by the producer, read only by the consumer.
	// Each side reads the other's index atomically, which is what makes the
	// queue safe without a mutex.
	write   atomic.Uint64
	read    atomic.Uint64
	written atomic.Uint64
	dropped atomic.Uint64
}

// NewRing returns a ring holding at least capacity samples.
func NewRing(capacity int) *Ring {
	if capacity < 2 {
		capacity = 2
	}
	size := uint64(1) << bits.Len(uint(capacity-1))
	return &Ring{buf: make([]float32, size), mask: size - 1}
}

// Cap returns the real capacity, rounded up to a power of two.
func (r *Ring) Cap() int { return len(r.buf) }

// Written is the number of samples the producer has offered, dropped ones
// included. It is the producer's absolute position in the input stream, which
// is what lets the consumer place a block on the practice timeline.
func (r *Ring) Written() uint64 { return r.written.Load() }

// Dropped is the number of samples lost to a full ring. A non-zero value means
// the consumer is not keeping up.
func (r *Ring) Dropped() uint64 { return r.dropped.Load() }

// Len is the number of samples currently readable.
func (r *Ring) Len() int { return int(r.write.Load() - r.read.Load()) }

// Write copies samples into the ring and returns how many it accepted. Called
// from the audio callback only.
func (r *Ring) Write(samples []float32) int {
	w := r.write.Load()
	free := uint64(len(r.buf)) - (w - r.read.Load())

	n := uint64(len(samples))
	if n > free {
		r.dropped.Add(n - free)
		n = free
	}
	r.written.Add(uint64(len(samples)))
	if n == 0 {
		return 0
	}

	for i := uint64(0); i < n; i++ {
		r.buf[(w+i)&r.mask] = samples[i]
	}
	// Publish the samples only after they are all in place.
	r.write.Store(w + n)
	return int(n)
}

// Drain discards everything currently readable and returns how many samples
// went. Consumer-side only, like Read.
func (r *Ring) Drain() int {
	rd := r.read.Load()
	n := r.write.Load() - rd
	r.read.Store(rd + n)
	return int(n)
}

// Read copies up to len(out) samples out of the ring and returns how many it
// got. Called from the DSP side only.
func (r *Ring) Read(out []float32) int {
	rd := r.read.Load()
	avail := r.write.Load() - rd

	n := uint64(len(out))
	if n > avail {
		n = avail
	}
	if n == 0 {
		return 0
	}

	for i := uint64(0); i < n; i++ {
		out[i] = r.buf[(rd+i)&r.mask]
	}
	// Release the space only after the samples have been copied out.
	r.read.Store(rd + n)
	return int(n)
}
