package audio

import (
	"math"
	"sync/atomic"
	"time"

	"github.com/FullFran/riffhero/internal/practice"
	"github.com/FullFran/riffhero/internal/stretch"
)

// renderChunkFrames is how much of the song the renderer prepares at a time.
// Small enough that a speed or loop change takes effect promptly, large enough
// that the time-stretcher has a real block to work with.
const renderChunkFrames = 512

// loopFadeFrames is the crossfade applied either side of a loop wrap or a
// seek. Cutting a waveform mid-cycle and starting somewhere else is a step
// change, and a step change is a click; a couple of milliseconds of fade costs
// nothing and removes it.
const loopFadeFrames = 128

// renderer produces the backing audio and, in doing so, decides where the song
// is. It runs on its own goroutine precisely so that the audio callback does
// not have to: decoding, stretching and loop bookkeeping all happen here, and
// the callback is left with a memcpy.
type renderer struct {
	player  *Player
	ring    *outRing
	stretch *stretch.WSOLA

	// src is the backing track, interleaved stereo at the device sample rate.
	// A song with no backing track renders silence instead, on the same clock,
	// so practising unaccompanied is the same code path.
	src    []float32
	frames int

	gain *atomic.Uint64 // master volume, math.Float64bits

	scratch []float32 // one chunk of stereo input
	outbuf  []float32 // stretcher output
	stamps  []stamp

	// Output that did not fit in the ring last time. Keeping it rather than
	// dropping it is what stops a momentarily full ring from punching a hole
	// in the backing track.
	pendAudio  []float32
	pendStamps []stamp

	cursor     practice.Frame // song frame of the next input sample
	gen        uint32
	fadeIn     int
	sampleRate int
}

func newRenderer(p *Player, ring *outRing, src []float32, sampleRate int, gain *atomic.Uint64) *renderer {
	worst := int(float64(renderChunkFrames)/practice.SpeedMin) + 4*renderChunkFrames

	r := &renderer{
		player:     p,
		ring:       ring,
		stretch:    stretch.New(2, sampleRate),
		src:        src,
		frames:     len(src) / 2,
		gain:       gain,
		scratch:    make([]float32, renderChunkFrames*2),
		outbuf:     make([]float32, worst*2),
		stamps:     make([]stamp, worst),
		pendAudio:  make([]float32, 0, worst*2),
		pendStamps: make([]stamp, 0, worst),
		sampleRate: sampleRate,
	}
	r.gen = p.SeekGeneration()
	r.cursor = p.SeekTarget()
	return r
}

// run keeps the ring fed until stop closes.
func (r *renderer) run(stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		default:
		}
		if !r.fill() {
			// Either paused or the ring is full. Either way there is nothing
			// useful to do for about one device period.
			select {
			case <-stop:
				return
			case <-time.After(2 * time.Millisecond):
			}
		}
	}
}

// fill pushes as much as it can and reports whether it did any work.
//
// It renders while the transport is paused too, which is what makes pressing
// play instantaneous: the ring is already full of the audio that starts at the
// playhead. The callback is the half that respects the pause — it leaves the
// ring alone while stopped, so nothing rendered ahead is consumed or counted
// as time passing.
func (r *renderer) fill() bool {
	worked := r.flushPending()
	if len(r.pendStamps) > 0 {
		return worked
	}

	r.syncSeek()

	speed := r.player.Speed()
	r.stretch.SetRate(speed)
	loop := r.player.Loop()
	end := r.player.End()

	// Render whenever there is room for a chunk. Anything the stretcher hands
	// back beyond that goes into the pending buffer, which is what lets the
	// ring actually fill: an earlier version demanded room for the worst-case
	// expansion up front, and so kept the ring three quarters empty and
	// underran on every device period that ran a millisecond late.
	for r.ring.Writable() >= renderChunkFrames && len(r.pendStamps) == 0 {
		in := renderChunkFrames
		wrap := false

		switch {
		case loop.Active() && r.cursor < loop.B:
			if r.cursor+practice.Frame(in) >= loop.B {
				in = int(loop.B - r.cursor)
				wrap = true
			}
		case loop.Active():
			// The playhead sits past the region; fold it back rather than
			// running to the end of the song.
			r.jumpTo(loop.A)
			continue
		default:
			if r.cursor+practice.Frame(in) >= end {
				in = int(end - r.cursor)
			}
		}

		if in <= 0 {
			// Everything is rendered. The transport stops only once the device
			// has actually played it, or the last bar would be cut off.
			if r.ring.Len() == 0 {
				r.player.Pause()
			}
			return worked
		}

		r.renderChunk(in, speed, wrap)
		if wrap {
			r.jumpTo(loop.A)
			r.player.countLap()
		}
		worked = true
	}
	return worked
}

// syncSeek notices a jump requested by the UI and restarts from there.
func (r *renderer) syncSeek() {
	if gen := r.player.SeekGeneration(); gen != r.gen {
		r.gen = gen
		r.cursor = r.player.SeekTarget()
		r.stretch.Reset()
		r.pendAudio = r.pendAudio[:0]
		r.pendStamps = r.pendStamps[:0]
		r.fadeIn = loopFadeFrames
	}
}

// jumpTo moves the render cursor without touching the seek generation: a loop
// wrap is not a user jump, and the audio already queued for the end of the
// region is still valid and still due to be heard.
func (r *renderer) jumpTo(to practice.Frame) {
	r.cursor = to
	r.fadeIn = loopFadeFrames
}

// renderChunk turns `in` song frames into output frames and queues them.
func (r *renderer) renderChunk(in int, speed float64, fadeOut bool) {
	r.readSource(in, fadeOut)

	var got int
	if r.src == nil {
		// No backing track: the timeline still has to advance, so emit the
		// silence it would have played. Running silence through the stretcher
		// would burn CPU to produce the same zeros.
		got = int(math.Round(float64(in) / speed))
		if got > len(r.stamps) {
			got = len(r.stamps)
		}
		for i := 0; i < got*2; i++ {
			r.outbuf[i] = 0
		}
	} else {
		r.stretch.Write(r.scratch[:in*2])
		got = r.stretch.Read(r.outbuf)
	}
	if got <= 0 {
		r.cursor += practice.Frame(in)
		return
	}

	// The stretcher holds some input before it can emit the output that input
	// belongs to, so the frames coming out now are that much older than the
	// cursor suggests. Without this correction the score would run ahead of
	// what the player hears by the stretcher's latency.
	base := r.cursor - practice.Frame(r.stretch.Latency())
	if base < 0 {
		base = 0
	}

	stamps := r.stamps[:got]
	for i := range stamps {
		stamps[i] = stamp{
			pos: int64(base) + int64(in)*int64(i)/int64(got),
			gen: r.gen,
		}
	}

	pushed := r.ring.Push(r.outbuf[:got*2], stamps)
	if pushed < got {
		r.pendAudio = append(r.pendAudio[:0], r.outbuf[pushed*2:got*2]...)
		r.pendStamps = append(r.pendStamps[:0], stamps[pushed:]...)
	}
	r.cursor += practice.Frame(in)
}

// readSource copies `in` frames of backing audio into scratch, applying master
// gain and the boundary fades.
func (r *renderer) readSource(in int, fadeOut bool) {
	if r.src == nil {
		return
	}
	g := float32(math.Float64frombits(r.gain.Load()))

	for i := 0; i < in; i++ {
		var l, rr float32
		if pos := int(r.cursor) + i; pos >= 0 && pos < r.frames {
			l, rr = r.src[pos*2], r.src[pos*2+1]
		}
		f := g
		if r.fadeIn > 0 {
			f *= float32(loopFadeFrames-r.fadeIn) / loopFadeFrames
			r.fadeIn--
		}
		if fadeOut && i >= in-loopFadeFrames {
			f *= float32(in-i) / loopFadeFrames
		}
		r.scratch[i*2], r.scratch[i*2+1] = l*f, rr*f
	}
}

// flushPending retries whatever did not fit last time.
func (r *renderer) flushPending() bool {
	if len(r.pendStamps) == 0 {
		return false
	}
	pushed := r.ring.Push(r.pendAudio, r.pendStamps)
	if pushed <= 0 {
		return false
	}
	r.pendAudio = append(r.pendAudio[:0], r.pendAudio[pushed*2:]...)
	r.pendStamps = append(r.pendStamps[:0], r.pendStamps[pushed:]...)
	return true
}
