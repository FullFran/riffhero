package audio

import (
	"math"
	"sync/atomic"
	"testing"

	"github.com/FullFran/riffhero/internal/practice"
)

const testRate = 48000

// rampBacking is a backing track whose left channel encodes its own frame
// number, so a stamp can be checked against the audio it is attached to.
func rampBacking(frames int) []float32 {
	out := make([]float32, frames*2)
	for i := 0; i < frames; i++ {
		out[i*2] = float32(i) / float32(frames)
		out[i*2+1] = out[i*2]
	}
	return out
}

func unityGain() *atomic.Uint64 {
	var g atomic.Uint64
	g.Store(math.Float64bits(1))
	return &g
}

// pump runs the renderer and collects the stamps of every frame it produced.
func pump(t *testing.T, r *renderer, ring *outRing, rounds int) []stamp {
	t.Helper()
	var got []stamp
	one := make([]float32, 2)

	for i := 0; i < rounds; i++ {
		r.fill()
		for {
			n, first, _, ok := ring.Pop(one, 1)
			if !ok || n == 0 {
				break
			}
			got = append(got, first)
		}
	}
	return got
}

func newTestRenderer(src []float32, end practice.Frame) (*renderer, *outRing, *Player) {
	p := NewPlayer(practice.Clock{SampleRate: testRate}, end)
	ring := newOutRing(outRingFrames)
	return newRenderer(p, ring, src, testRate, unityGain()), ring, p
}

func TestRendererFillsTheRingWhilePaused(t *testing.T) {
	// Pressing play has to be instant, so the renderer works ahead while the
	// transport is stopped. Respecting the pause is the callback's job: it
	// leaves the ring alone, so none of this counts as time passing.
	r, ring, p := newTestRenderer(rampBacking(testRate), testRate)

	for i := 0; i < 20; i++ {
		r.fill()
	}
	if ring.Len() == 0 {
		t.Fatal("a paused renderer left the ring empty; play would start on an underrun")
	}
	if got := p.Position(); got != 0 {
		t.Fatalf("the playhead moved to %d while paused", got)
	}
	// What is queued starts at the playhead, not somewhere after it.
	one := make([]float32, 2)
	if _, first, _, ok := ring.Pop(one, 1); !ok || first.pos != 0 {
		t.Fatalf("the first queued frame is stamped %d, want 0", first.pos)
	}
}

func TestRendererAdvancesTheSongAtFullSpeed(t *testing.T) {
	r, ring, p := newTestRenderer(rampBacking(testRate), testRate)
	p.Play()

	stamps := pump(t, r, ring, 20)
	if len(stamps) == 0 {
		t.Fatal("no audio produced")
	}
	if stamps[0].pos != 0 {
		t.Fatalf("first stamp is %d, want 0", stamps[0].pos)
	}
	for i := 1; i < len(stamps); i++ {
		if stamps[i].pos < stamps[i-1].pos {
			t.Fatalf("stamp %d went backwards: %d then %d", i, stamps[i-1].pos, stamps[i].pos)
		}
	}
	// At full speed one output frame is one song frame, so the span of the
	// stamps must match the number of frames produced.
	span := stamps[len(stamps)-1].pos - stamps[0].pos
	if diff := int(span) - len(stamps); diff > 4 || diff < -4 {
		t.Fatalf("%d frames covered %d song frames", len(stamps), span)
	}
}

func TestRendererStretchesTimeAtHalfSpeed(t *testing.T) {
	r, ring, p := newTestRenderer(rampBacking(testRate), testRate)
	p.SetSpeed(0.5)
	p.Play()

	stamps := pump(t, r, ring, 30)
	if len(stamps) < 1000 {
		t.Fatalf("only %d frames produced", len(stamps))
	}
	span := float64(stamps[len(stamps)-1].pos - stamps[0].pos)
	ratio := float64(len(stamps)) / span
	// Half speed means twice as many output frames per song frame.
	if ratio < 1.7 || ratio > 2.3 {
		t.Fatalf("produced %d frames over %v song frames (ratio %.2f), want about 2", len(stamps), span, ratio)
	}
}

func TestRendererWrapsTheLoopAndCountsLaps(t *testing.T) {
	clock := practice.Clock{SampleRate: testRate}
	r, ring, p := newTestRenderer(rampBacking(testRate*2), clock.Frames(2))
	p.SetLoop(practice.Loop{A: clock.Frames(0.2), B: clock.Frames(0.4), Enabled: true})
	p.Restart() // what the UI does after selecting a region
	p.Play()

	stamps := pump(t, r, ring, 60)
	if len(stamps) == 0 {
		t.Fatal("no audio produced")
	}
	a, b := int64(clock.Frames(0.2)), int64(clock.Frames(0.4))
	for i, s := range stamps {
		if s.pos < a || s.pos >= b {
			t.Fatalf("frame %d stamped %d, outside the region [%d,%d)", i, s.pos, a, b)
		}
	}
	if p.Laps() < 2 {
		t.Fatalf("counted %d laps over %d frames, want at least 2", p.Laps(), len(stamps))
	}
}

func TestRendererStopsAtTheEndOfTheSong(t *testing.T) {
	clock := practice.Clock{SampleRate: testRate}
	end := clock.Frames(0.1)
	r, ring, p := newTestRenderer(rampBacking(int(end)), end)
	p.Play()

	stamps := pump(t, r, ring, 40)
	if p.Playing() {
		t.Fatal("the renderer should stop the player at the end of the song")
	}
	for i, s := range stamps {
		if s.pos >= int64(end) {
			t.Fatalf("frame %d stamped %d, past the song end %d", i, s.pos, end)
		}
	}
}

func TestRendererKeepsTheClockWithoutABackingTrack(t *testing.T) {
	// Practising unaccompanied has to advance the timeline exactly like a
	// backing track would, or nothing would ever be scored.
	clock := practice.Clock{SampleRate: testRate}
	r, ring, p := newTestRenderer(nil, clock.Frames(1))
	p.Play()

	stamps := pump(t, r, ring, 20)
	if len(stamps) == 0 {
		t.Fatal("silence still has to be produced, and stamped")
	}
	span := stamps[len(stamps)-1].pos - stamps[0].pos
	if diff := int(span) - len(stamps); diff > 4 || diff < -4 {
		t.Fatalf("%d silent frames covered %d song frames", len(stamps), span)
	}
}

func TestRendererRestartsFromASeek(t *testing.T) {
	clock := practice.Clock{SampleRate: testRate}
	r, ring, p := newTestRenderer(rampBacking(testRate), clock.Frames(1))
	p.Play()

	pump(t, r, ring, 3)
	genBefore := p.SeekGeneration()

	target := clock.Frames(0.5)
	p.Seek(target)
	stamps := pump(t, r, ring, 5)

	if len(stamps) == 0 {
		t.Fatal("no audio after the seek")
	}
	for i, s := range stamps {
		if s.gen == genBefore {
			t.Fatalf("frame %d still carries the pre-seek generation", i)
		}
	}
	if first := stamps[0].pos; first < int64(target)-16 || first > int64(target)+2048 {
		t.Fatalf("first frame after the seek is stamped %d, want about %d", first, target)
	}
}

func TestRendererFadesRatherThanClicksAtTheLoopPoint(t *testing.T) {
	// Cutting a waveform mid-cycle and restarting somewhere else is a step
	// change, and a step change is a click. A smooth source makes the test
	// meaningful: a 100 Hz sine never moves more than about 0.013 between
	// neighbouring samples, so anything much larger in the output came from
	// the seam and not from the music.
	clock := practice.Clock{SampleRate: testRate}
	src := make([]float32, testRate*2)
	for i := 0; i < testRate; i++ {
		v := float32(math.Sin(2 * math.Pi * 100 * float64(i) / testRate))
		src[i*2], src[i*2+1] = v, v
	}

	p := NewPlayer(clock, clock.Frames(1))
	ring := newOutRing(outRingFrames)
	r := newRenderer(p, ring, src, testRate, unityGain())
	// A region whose ends are deliberately out of phase with each other, so a
	// naive wrap would step straight from near +1 to near -1.
	p.SetLoop(practice.Loop{A: 2400, B: 2400 + 360, Enabled: true})
	p.Play()

	var prev float32
	first := true
	var maxJump float32
	buf := make([]float32, 2)
	for round := 0; round < 40; round++ {
		r.fill()
		for {
			n, _, _, ok := ring.Pop(buf, 1)
			if !ok || n == 0 {
				break
			}
			if !first {
				if j := float32(math.Abs(float64(buf[0] - prev))); j > maxJump {
					maxJump = j
				}
			}
			prev, first = buf[0], false
		}
	}
	if first {
		t.Fatal("no audio produced")
	}
	if maxJump > 0.05 {
		t.Fatalf("largest sample-to-sample jump was %v; the loop seam is clicking", maxJump)
	}
}

func TestRendererPlaysIntoTheRegionBeforeLooping(t *testing.T) {
	// Setting a loop ahead of the playhead must not yank the playhead forward.
	// The run plays through into the region and only starts looping once it
	// reaches the far end, which is what a musician expects from a DAW.
	clock := practice.Clock{SampleRate: testRate}
	r, ring, p := newTestRenderer(rampBacking(testRate*2), clock.Frames(2))
	a, b := clock.Frames(0.2), clock.Frames(0.4)
	p.SetLoop(practice.Loop{A: a, B: b, Enabled: true})
	p.Play()

	stamps := pump(t, r, ring, 60)
	if len(stamps) == 0 {
		t.Fatal("no audio produced")
	}
	if stamps[0].pos != 0 {
		t.Fatalf("playback started at %d, want 0", stamps[0].pos)
	}

	var leadIn, inRegion int
	for _, s := range stamps {
		switch {
		case s.pos < int64(a):
			leadIn++
		case s.pos < int64(b):
			inRegion++
		default:
			t.Fatalf("frame stamped %d, past the region end %d", s.pos, b)
		}
	}
	if leadIn == 0 {
		t.Fatal("the lead-in before A was never played")
	}
	if inRegion == 0 {
		t.Fatal("the region itself was never reached")
	}
	if p.Laps() == 0 {
		t.Fatal("reaching B should have wrapped at least once")
	}
}
