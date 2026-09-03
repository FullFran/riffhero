// Package stretch plays audio slower than it was recorded without moving its
// pitch, which is the whole point of practising a passage at 70%.
//
// The implementation is WSOLA — waveform similarity overlap-add — in pure Go.
// PLAN.md's phase 6 proposed binding Signalsmith Stretch through cgo instead;
// the surface here is small enough that swapping the implementation later
// touches nothing outside this package, and staying in Go keeps the build
// cgo-free apart from the audio device. WSOLA is strongest exactly where
// practice lives, between a quarter speed and unity, and its cost is bounded
// and predictable in a way a phase vocoder's is not.
//
// What a caller has to know about it:
//
//   - Latency depends on the rate. At rate 1 the filter is a copy and the
//     latency is zero; everywhere else it is window plus tolerance. Read it
//     after SetRate, not once at startup.
//   - The similarity search can only match the phase of a partial whose period
//     fits inside twice the tolerance, which puts a floor at about 50 Hz.
//     Below that — sub-bass, a detuned five-string — the phase match is a
//     guess and sustained content there can warble.
//   - Material with no period to match, cymbals and breath and applause, gets
//     smeared rather than stretched. That is WSOLA, not this implementation of
//     it, and it is the reason the tests state their tolerances against the
//     source signal instead of in the abstract.
//   - Drain gives up on the last segment or two rather than searching a window
//     that has run out of input, so a drained stream comes out a couple of
//     percent short. At the end of a passage that is inaudible; it is not a
//     substitute for the caller's own frame accounting.
package stretch

import "math"

// MinRate and MaxRate bound what SetRate accepts. Below a quarter speed WSOLA
// repeats each segment so many times that the result stops sounding like a
// performance and starts sounding like a stutter, and above double speed it
// throws away more input than it keeps. Both ends are far outside any rate a
// practice session has a use for, so clamping is the honest response to a
// caller that asks for one.
const (
	MinRate = 0.25
	MaxRate = 2.0
)

const (
	// windowMillis is the synthesis segment length. It has to hold several
	// periods of the lowest note in a backing track or the overlap-add
	// averages away the bass; 40 ms holds two periods of a 50 Hz kick.
	windowMillis = 40.0

	// toleranceMillis is the half-width of the similarity search. What matters
	// is the *full* width: the search can only match the phase of a partial
	// whose period fits inside 2*tolerance, so 10 ms either side covers
	// everything down to 50 Hz. Widening it past that buys nothing and costs
	// linearly.
	toleranceMillis = 10.0

	// defaultStride is the coarse step of the two-pass similarity search: score
	// every fourth candidate, then sweep the frames around the winner at full
	// resolution. The correlation curve oscillates at the period of the
	// dominant partial, so a step of 4 still lands inside the right hump for
	// anything below about 3 kHz.
	//
	// It does not always choose the same offset as an exhaustive sweep. On
	// dense material the curve has several humps of nearly equal height and the
	// coarse pass can prefer a different one — which is fine, because they are
	// all phase matches and the seam is what matters, not which match won. The
	// claim the tests hold it to is that the output is as seamless either way,
	// at a quarter of the cost; see TestCoarseSearchIsAsCleanAsExhaustive and
	// BenchmarkStretchSecond.
	defaultStride = 4

	// minFrame keeps the geometry sane if someone constructs the filter at an
	// absurd sample rate in a test.
	minFrame = 64
)

// WSOLA time-stretches interleaved float32 audio without changing its pitch.
//
// It is a push/pull filter: the renderer pushes the input frames it wants
// covered and pulls whatever output that produced, which is what lets the
// caller keep an exact input-frames-to-timeline mapping instead of trusting
// the stretcher's own accounting.
//
// A WSOLA is not safe for concurrent use. It is meant to live on one render
// goroutine, not on the audio callback: Write does real work, and the callback
// must only ever move bytes.
type WSOLA struct {
	channels int
	rate     float64

	frame     int // synthesis segment length, in frames
	hop       int // synthesis hop and overlap length, frame/2
	tolerance int // how far either side of the nominal position the search looks
	stride    int // coarse step of the two-pass search

	// window is a periodic Hann of length frame. Periodic, not symmetric:
	// only the periodic form satisfies w[i]+w[i+hop] == 1 at 50% overlap, and
	// without that identity the overlap-add ripples at the segment rate, which
	// is audible as a 25 Hz tremolo on sustained chords.
	window []float32

	// in holds interleaved input that the search may still need; mono is its
	// channel average. inBase is the absolute frame index of in[0], so
	// positions stay meaningful after the buffer is compacted.
	in     []float32
	mono   []float32
	inBase int64

	// pad is how many frames of silence Drain appended past the real input.
	// A stream ends in silence, so saying so explicitly is both true and the
	// thing that lets every segment be placed at full length; the alternative
	// is truncating the last segments, and a truncated segment leaves the
	// accumulator empty behind it, which cuts the tail off mid-waveform.
	pad int

	// acc is the overlap-add accumulator: frame frames of interleaved audio
	// whose first hop are finished as soon as the next segment lands on them.
	acc    []float32
	primed bool // acc holds a tail that the next segment has to complete

	// nominal is where the next segment would start if there were no search.
	// It advances by rate*hop whatever the search chooses, which is what keeps
	// the average speed exact: advancing from the chosen offset instead lets
	// a run of same-signed matches drift the playhead off the timeline.
	nominal float64

	// cont is the input position the previous segment predicted the output
	// would continue into. It is the template the next search matches against.
	cont int64

	// bypassing is the rate == 1 path: a verbatim copy from copyPos onward.
	bypassing bool
	copyPos   int64

	out     []float32
	outRead int
}

// New returns a stretcher for the given channel count and sample rate.
func New(channels, sampleRate int) *WSOLA {
	if channels < 1 {
		channels = 1
	}
	if sampleRate < 1 {
		sampleRate = 48000
	}

	frame := int(windowMillis / 1000 * float64(sampleRate))
	if frame < minFrame {
		frame = minFrame
	}
	frame &^= 1 // the two halves have to be the same length

	tolerance := int(toleranceMillis / 1000 * float64(sampleRate))
	if tolerance < 1 {
		tolerance = 1
	}

	w := &WSOLA{
		channels:  channels,
		rate:      1,
		frame:     frame,
		hop:       frame / 2,
		tolerance: tolerance,
		stride:    defaultStride,
		window:    make([]float32, frame),
		acc:       make([]float32, frame*channels),
		bypassing: true,
	}
	for i := range w.window {
		w.window[i] = float32(0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(frame)))
	}

	// Preallocate everything the steady state touches. Write and Read run on a
	// render goroutine feeding a real-time callback, and a stop-the-world pause
	// there is a dropout the player hears.
	span := (frame + 2*tolerance) * 4
	w.in = make([]float32, 0, span*channels)
	w.mono = make([]float32, 0, span)
	w.out = make([]float32, 0, span*channels)
	return w
}

// SetRate sets the playback rate. 1 is unchanged, 0.5 plays half as fast and
// therefore produces twice as many output frames per input frame. Rates
// outside a sane practice range are clamped.
//
// The rate may change at any time. Nothing switches here, because leaving the
// stretcher needs input that may not have been written yet; the changeover
// happens on the next Write, so SetRate can never fail or block.
func (w *WSOLA) SetRate(rate float64) {
	switch {
	case math.IsNaN(rate):
		// A NaN compares false against every bound, so it would slip through
		// the clamp below and poison every position in the filter.
		return
	case rate < MinRate:
		rate = MinRate
	case rate > MaxRate:
		rate = MaxRate
	}
	w.rate = rate
}

// Rate is the current playback rate.

// Channels is the interleave width the filter expects and produces.
// Rate reports the clamped playback rate actually in force.
func (w *WSOLA) Rate() float64 { return w.rate }

func (w *WSOLA) Channels() int { return w.channels }

// Latency is how many input frames must be written past a point before the
// output covering that point can be read.
//
// It depends on the rate, and a caller mapping output back onto the timeline
// has to read it after SetRate rather than once at startup. At rate 1 the
// filter is a copy with nothing buffered, so the latency really is zero, and
// reporting the stretching figure there would misplace the timeline by the
// whole window.
func (w *WSOLA) Latency() int {
	if w.rate == 1 {
		return 0
	}
	return w.frame + w.tolerance
}

// Write pushes interleaved input frames. A partial frame at the end of in is
// ignored rather than padded, because padding would insert a click the caller
// never wrote.
func (w *WSOLA) Write(in []float32) {
	n := len(in) / w.channels
	if n == 0 {
		return
	}
	in = in[:n*w.channels]

	base := len(w.in)
	w.in = extend(w.in, len(in))
	copy(w.in[base:], in)

	mbase := len(w.mono)
	w.mono = extend(w.mono, n)
	w.downmix(w.mono[mbase:], in)

	w.process(false)
}

// downmix fills dst with the channel average of one interleaved block.
//
// The similarity search runs on this and only this. Searching each channel
// separately would let the channels land on offsets a tolerance apart, and
// two channels of the same instrument shifted by up to 10 ms against each
// other is not a wider image, it is a smeared one: the phantom centre
// collapses and everything sounds like it went through a chorus pedal.
func (w *WSOLA) downmix(dst, in []float32) {
	c := w.channels
	if c == 1 {
		copy(dst, in)
		return
	}
	scale := float32(1) / float32(c)
	for f := range dst {
		var sum float32
		block := in[f*c : f*c+c]
		for _, s := range block {
			sum += s
		}
		dst[f] = sum * scale
	}
}

// Available is how many output FRAMES can be read right now.
func (w *WSOLA) Available() int { return (len(w.out) - w.outRead) / w.channels }

// Read copies up to len(out)/Channels frames into out and returns the number
// of FRAMES written. A buffer too small to hold one frame reads nothing rather
// than returning a torn frame.
func (w *WSOLA) Read(out []float32) int {
	n := len(out) / w.channels
	if avail := w.Available(); n > avail {
		n = avail
	}
	if n <= 0 {
		return 0
	}

	size := n * w.channels
	copy(out, w.out[w.outRead:w.outRead+size])
	w.outRead += size

	switch {
	case w.outRead == len(w.out):
		w.out = w.out[:0]
		w.outRead = 0
	case w.outRead >= len(w.out)/2 && w.outRead >= 4096:
		// Compact rather than reslice, so the backing array is reused for the
		// life of the session instead of being reallocated behind append.
		w.out = append(w.out[:0], w.out[w.outRead:]...)
		w.outRead = 0
	}
	return n
}

// Drain flushes what is buffered at the end of a stream. Input written after a
// Drain is treated as the start of a new stream, so calling it mid-song costs
// a segment of crossfade.
func (w *WSOLA) Drain() {
	// Pad with one segment of silence so the overlap-add can run off the end
	// of the signal instead of stopping short of it. Everything past signalEnd
	// is readable but invisible to the search and to the copy path.
	frames := w.frame
	base := len(w.in)
	w.in = extend(w.in, frames*w.channels)
	clear32(w.in[base:])
	mbase := len(w.mono)
	w.mono = extend(w.mono, frames)
	clear32(w.mono[mbase:])
	w.pad = frames

	w.process(true)

	end := w.signalEnd()
	w.pad = 0
	w.nominal = float64(end)
	w.cont = end
	w.copyPos = end
	w.primed = false
	clear32(w.acc)

	// Everything written has become output, so nothing has to be kept back for
	// a search that will never run.
	w.in = w.in[:0]
	w.mono = w.mono[:0]
	w.inBase = end
}

// Reset clears all state, for a seek. The rate survives, because seeking is
// not a reason to start playing at a different speed.
func (w *WSOLA) Reset() {
	w.in = w.in[:0]
	w.mono = w.mono[:0]
	w.out = w.out[:0]
	w.outRead = 0
	w.inBase = 0
	w.nominal = 0
	w.cont = 0
	w.copyPos = 0
	w.primed = false
	w.pad = 0
	w.bypassing = w.rate == 1
	clear32(w.acc)
}

// end is the absolute frame index one past the last frame the buffer holds,
// drain padding included. signalEnd is one past the last frame the caller
// actually wrote: the padding may be read from, never searched in and never
// copied out.
func (w *WSOLA) end() int64 { return w.inBase + int64(len(w.in)/w.channels) }

func (w *WSOLA) signalEnd() int64 { return w.end() - int64(w.pad) }

// process turns as much buffered input into output as the current mode allows.
func (w *WSOLA) process(drain bool) {
	w.switchMode(drain)
	if w.bypassing {
		w.copyThrough()
		w.compact()
		return
	}
	for w.step(drain) {
	}
	if drain {
		w.flush()
	}
	w.compact()
}

// switchMode moves between the stretching path and the rate == 1 copy path.
//
// The handover is the whole reason a rate change does not click. Going into
// the copy, the accumulator is holding w[hop+i]*x[cont+i] and the copy is
// about to play x[cont+i]; adding (1-w[hop+i])*x[cont+i] lands the seam on
// exactly the sample the stretched output was already heading for. Dropping
// the accumulator and starting the copy from wherever the input happens to be
// is the obvious implementation and it steps the waveform.
func (w *WSOLA) switchMode(drain bool) {
	if w.bypassing == (w.rate == 1) {
		return
	}

	if w.rate != 1 {
		// Entering the stretcher. The first segment starts where the copy
		// stopped and is not searched, so there is nothing to match and
		// nothing to fade.
		w.nominal = float64(w.copyPos)
		w.cont = w.copyPos
		w.primed = false
		w.bypassing = false
		return
	}

	if !w.primed {
		w.copyPos = int64(math.Round(w.nominal))
		w.bypassing = true
		return
	}

	end := w.end()
	n := w.hop
	if avail := int(end - w.cont); avail < n {
		if !drain {
			return // the crossfade needs input that has not been written yet
		}
		if avail < 0 {
			avail = 0
		}
		n = avail
	}

	c := w.channels
	src := int(w.cont-w.inBase) * c
	for i := 0; i < n; i++ {
		g := 1 - w.window[w.hop+i]
		o := i * c
		for ch := 0; ch < c; ch++ {
			w.acc[o+ch] += g * w.in[src+o+ch]
		}
	}
	w.emit(w.hop)
	w.copyPos = w.cont + int64(w.hop)
	w.primed = false
	w.bypassing = true
}

// copyThrough is the rate == 1 path: the input is the output, verbatim.
//
// A practice session spends most of its life at full speed, and running the
// overlap-add there would pay a similarity search and a window multiply to
// produce something that is only nearly the original. Nearly is the wrong
// answer when the exact one is a copy.
func (w *WSOLA) copyThrough() {
	end := w.signalEnd()
	n := int(end - w.copyPos)
	if n <= 0 {
		return
	}
	c := w.channels
	src := int(w.copyPos-w.inBase) * c
	base := len(w.out)
	w.out = extend(w.out, n*c)
	copy(w.out[base:], w.in[src:src+n*c])
	w.copyPos = end
}

// step places one synthesis segment and emits one hop of output. It reports
// whether it ran, so process can loop until the input runs out.
func (w *WSOLA) step(drain bool) bool {
	end := w.end()
	signal := w.signalEnd()
	nom := int64(math.Round(w.nominal))

	if drain {
		if nom >= signal {
			return false
		}
		if w.primed && !w.searchable(nom, signal) && w.cont >= signal {
			return false
		}
	} else if nom+int64(w.tolerance+w.frame) > end {
		// Every candidate the search may pick has to be written in full before
		// the search runs. Placing a segment on input that is merely present
		// would make the offset depend on the caller's block size, and the same
		// song would then stretch differently every time it was played.
		return false
	}
	if nom < w.inBase {
		nom = w.inBase
	}

	b := nom
	if w.primed {
		if w.searchable(nom, signal) {
			b = w.searchOffset(nom, signal)
		} else {
			// The drain tail, where there is no longer a full overlap to match
			// against. Continue exactly where the last segment pointed instead
			// of searching: acc is holding w[hop+i]*x[cont+i] and this adds
			// (1-w[hop+i])*x[cont+i], so the seam is exact. The price is that
			// the last few frames of a stream play at speed rather than at the
			// chosen rate, which nobody has ever heard and everybody would hear
			// the alternative.
			b = w.cont
		}
	}
	if b < w.inBase {
		b = w.inBase
	}

	n := w.frame
	if over := int(b + int64(n) - end); over > 0 {
		n -= over // unreachable while the drain pad is in place; belt and braces
	}
	if n <= 0 {
		return false
	}

	c := w.channels
	src := int(b-w.inBase) * c
	if w.primed {
		for i := 0; i < n; i++ {
			g := w.window[i]
			o := i * c
			for ch := 0; ch < c; ch++ {
				w.acc[o+ch] += g * w.in[src+o+ch]
			}
		}
	} else {
		// The first segment after a reset, a seek or a rate change has nothing
		// underneath its rising half, so windowing it would fade the passage in
		// every single time. Copy that half flat and window only the tail the
		// next segment will complete.
		head := w.hop
		if head > n {
			head = n
		}
		for i := 0; i < head; i++ {
			o := i * c
			for ch := 0; ch < c; ch++ {
				w.acc[o+ch] = w.in[src+o+ch]
			}
		}
		for i := head; i < n; i++ {
			g := w.window[i]
			o := i * c
			for ch := 0; ch < c; ch++ {
				w.acc[o+ch] = g * w.in[src+o+ch]
			}
		}
	}

	w.emit(w.hop)
	w.cont = b + int64(w.hop)
	w.nominal += w.rate * float64(w.hop)
	w.primed = true
	return true
}

// searchable reports whether a full-length overlap and at least one whole
// candidate are still available. Only a drain can make it false.
func (w *WSOLA) searchable(nom, end int64) bool {
	hop := int64(w.hop)
	return w.cont+hop <= end && nom-int64(w.tolerance) <= end-hop
}

// searchOffset picks where the next synthesis segment should start.
//
// The candidates sit within a tolerance either side of the nominal analysis
// position and are scored by normalized cross-correlation against the input
// the previous segment predicted the output would continue into. Normalizing
// by the candidate's own energy is the part that is easy to leave out and
// wrong to: a bare dot product prefers whichever candidate is loudest, so the
// search walks onto every attack instead of onto the phase match it was asked
// for, and the result is a stutter on exactly the percussive material the
// player is trying to lock to.
func (w *WSOLA) searchOffset(nom, end int64) int64 {
	// end here is signalEnd: the search never looks at the drain padding,
	// because correlating against manufactured silence scores nothing and
	// picks an offset at random.
	lo := nom - int64(w.tolerance)
	hi := nom + int64(w.tolerance)
	if lo < w.inBase {
		lo = w.inBase
	}
	if hi > end-1 {
		hi = end - 1
	}
	if hi < lo {
		return lo
	}

	// The overlap is always matched at full length; step calls searchable
	// first so this clamp can never empty the range. Shortening the overlap
	// near the end of a stream, to squeeze one more segment out of it, is
	// where this first went wrong: a correlation over the last few frames
	// scores noise, the offset it returns is arbitrary, and the final segment
	// of every drained stream lands on the wrong phase. On a single tone that
	// is inaudible; on a chord it is a click at full level.
	if hi > end-int64(w.hop) {
		hi = end - int64(w.hop)
	}
	if hi < lo {
		return clampInt64(nom, lo, end-1)
	}

	ti := int(w.cont - w.inBase)
	template := w.mono[ti : ti+w.hop]

	stride := int64(w.stride)
	best, bestScore := lo, math.Inf(-1)
	for b := lo; b <= hi; b += stride {
		if s := similarity(template, w.mono[int(b-w.inBase):]); s > bestScore {
			best, bestScore = b, s
		}
	}

	// Second pass at full resolution around the coarse winner. Without it the
	// offset is quantized to the stride, and a quantized offset is a phase
	// error of up to half a stride on every segment.
	rlo, rhi := best-stride+1, best+stride-1
	if rlo < lo {
		rlo = lo
	}
	if rhi > hi {
		rhi = hi
	}
	for b := rlo; b <= rhi; b++ {
		if (b-lo)%stride == 0 {
			continue // already scored by the coarse pass
		}
		if s := similarity(template, w.mono[int(b-w.inBase):]); s > bestScore {
			best, bestScore = b, s
		}
	}
	return best
}

// similarity is the cross-correlation of template against the head of cand,
// normalized by cand's energy. The template's own energy is the same for every
// candidate, so leaving it out changes no comparison and saves a pass.
func similarity(template, cand []float32) float64 {
	cand = cand[:len(template)]
	var dot, energy float64
	for i, t := range template {
		c := float64(cand[i])
		dot += float64(t) * c
		energy += c * c
	}
	if energy <= 0 {
		return 0
	}
	return dot / math.Sqrt(energy)
}

// emit moves the finished head of the accumulator to the output and slides the
// tail down, so acc[0] is always the next output frame.
func (w *WSOLA) emit(frames int) {
	size := frames * w.channels
	base := len(w.out)
	w.out = extend(w.out, size)
	copy(w.out[base:], w.acc[:size])

	copy(w.acc, w.acc[size:])
	clear32(w.acc[len(w.acc)-size:])
}

// flush empties the accumulator at the end of a stream: the falling half of
// the last segment, with nothing left to complete it.
//
// It is skipped when that half covers nothing but the drain padding, which is
// the difference between ending where the caller's audio ends and appending
// forty milliseconds of manufactured silence to every drained stream.
func (w *WSOLA) flush() {
	if !w.primed {
		return
	}
	if w.cont < w.signalEnd() {
		w.emit(w.hop)
	}
	w.primed = false
}

// compact drops input that no template and no candidate can still reach.
//
// It waits until a whole segment's worth is droppable. Compacting on every
// Write would copy the entire live window for each block the caller pushes,
// which turns a 64-frame write into a few thousand frames of memmove.
func (w *WSOLA) compact() {
	keep := w.end()
	if w.bypassing {
		keep = w.copyPos
	} else {
		if n := int64(math.Round(w.nominal)) - int64(w.tolerance); n < keep {
			keep = n
		}
		if w.cont < keep {
			keep = w.cont
		}
	}
	if keep > w.end() {
		keep = w.end()
	}

	drop := keep - w.inBase
	if drop < int64(w.frame) {
		return
	}
	c := int64(w.channels)
	w.in = append(w.in[:0], w.in[drop*c:]...)
	w.mono = append(w.mono[:0], w.mono[drop:]...)
	w.inBase = keep
}

// extend lengthens buf by n samples, growing its backing array geometrically
// so a steady stream stops allocating once it has seen its largest block.
func extend(buf []float32, n int) []float32 {
	need := len(buf) + n
	if need <= cap(buf) {
		return buf[:need]
	}
	grown := make([]float32, need, 2*need)
	copy(grown, buf)
	return grown
}

func clear32(buf []float32) {
	for i := range buf {
		buf[i] = 0
	}
}

func clampInt64(v, lo, hi int64) int64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
