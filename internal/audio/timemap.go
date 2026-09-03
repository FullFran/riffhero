package audio

import (
	"math/bits"
	"sync/atomic"

	"github.com/FullFran/riffhero/internal/practice"
)

// TimeMap answers the one question that connects the guitar to the score:
// given a position in the capture stream, where was the song?
//
// The two clocks are not the same clock and cannot be made into one. The
// capture stream counts every sample the device ever handed us, monotonically,
// forever. The song position jumps at a seek, wraps at an A-B loop boundary,
// and moves at practice speed rather than real time. What relates them is the
// audio callback: within one invocation both advance together, linearly. So
// the map is a list of those invocations, and a lookup is a search through it.
//
// It is written by the callback and read by the game loop, which means no
// locks and no allocation on the writing side. Each slot carries a version
// counter: the writer bumps it to odd before touching the slot and to even
// after, and a reader that sees an odd or changed version retries. Every field
// is atomic so the race detector agrees with the design.
type TimeMap struct {
	seg  []timeSegment
	mask uint64
	next atomic.Uint64
}

type timeSegment struct {
	version     atomic.Uint64
	streamStart atomic.Int64
	streamLen   atomic.Int64
	songStart   atomic.Int64
	songEnd     atomic.Int64
	valid       atomic.Bool
}

// NewTimeMap returns a map remembering the last n callback invocations. At a
// typical 10 ms period, 256 of them is two and a half seconds of history —
// far more than the detector's analysis latency needs.
func NewTimeMap(n int) *TimeMap {
	if n < 2 {
		n = 2
	}
	size := 1 << bits.Len(uint(n-1))
	return &TimeMap{seg: make([]timeSegment, size), mask: uint64(size) - 1}
}

// Push records one callback invocation: streamLen frames of input starting at
// streamStart, during which the song moved from songStart to songEnd.
//
// valid is false when the song did not advance at all — the transport was
// paused, or the renderer starved and the device got silence. Input captured
// then belongs to no song position, and saying so is better than inventing one.
func (m *TimeMap) Push(streamStart, streamLen, songStart, songEnd int64, valid bool) {
	if streamLen <= 0 {
		return
	}
	s := &m.seg[m.next.Load()&m.mask]

	v := s.version.Load()
	s.version.Store(v + 1) // odd: readers back off
	s.streamStart.Store(streamStart)
	s.streamLen.Store(streamLen)
	s.songStart.Store(songStart)
	s.songEnd.Store(songEnd)
	s.valid.Store(valid)
	s.version.Store(v + 2)

	m.next.Add(1)
}

// Lookup maps a capture-stream position onto the song timeline. It fails when
// the position is in a stretch of stream that no song position corresponds to,
// or when it has already been overwritten by newer history.
func (m *TimeMap) Lookup(stream int64) (practice.Frame, bool) {
	// Newest first: a detection is nearly always recent, and searching
	// backwards also means the slot most likely to be overwritten mid-read is
	// the one we reach last.
	next := m.next.Load()
	for i := 0; i < len(m.seg); i++ {
		if next == 0 {
			break
		}
		idx := (next - 1 - uint64(i)) & m.mask
		start, length, songStart, songEnd, valid, ok := m.seg[idx].read()
		if !ok || length <= 0 {
			continue
		}
		if stream < start || stream >= start+length {
			continue
		}
		if !valid {
			return 0, false
		}
		// Linear inside the invocation. A loop wrap lands inside one of these
		// blocks a few milliseconds long; the interpolation is wrong for that
		// single block and right for every other, which is a better trade than
		// carrying per-frame stamps across the boundary.
		offset := stream - start
		song := songStart + (songEnd-songStart)*offset/length
		return practice.Frame(song), true
	}
	return 0, false
}

// read returns a consistent snapshot of the slot, or ok=false if the writer
// was in the middle of it.
func (s *timeSegment) read() (start, length, songStart, songEnd int64, valid, ok bool) {
	for attempt := 0; attempt < 4; attempt++ {
		v1 := s.version.Load()
		if v1%2 == 1 {
			continue
		}
		start = s.streamStart.Load()
		length = s.streamLen.Load()
		songStart = s.songStart.Load()
		songEnd = s.songEnd.Load()
		valid = s.valid.Load()
		if s.version.Load() == v1 {
			return start, length, songStart, songEnd, valid, v1 > 0
		}
	}
	return 0, 0, 0, 0, false, false
}
