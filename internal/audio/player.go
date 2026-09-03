package audio

import (
	"math"
	"sync/atomic"

	"github.com/FullFran/riffhero/internal/practice"
)

// Player is the transport state shared by the three threads that care about
// it: the UI sets it, the render goroutine reads it to decide what audio to
// produce, and the audio callback writes the one field that is authoritative —
// the position actually being heard.
//
// It satisfies practice.Playhead, so everything above it is written once and
// works whether time comes from a device or from a test's Transport.
//
// Every field is atomic because the callback may not take a lock. Nothing here
// allocates.
type Player struct {
	clock practice.Clock

	end       atomic.Int64
	pos       atomic.Int64
	playing   atomic.Bool
	speedBits atomic.Uint64
	laps      atomic.Int64

	loopA  atomic.Int64
	loopB  atomic.Int64
	loopOn atomic.Bool

	// seekGen is bumped on every jump. Output audio is stamped with the
	// generation that produced it, and the callback ignores stamps from an
	// older one, so the playhead never rewinds through audio the user already
	// seeked away from.
	seekTarget atomic.Int64
	seekGen    atomic.Uint32
	// observedGen is the generation of the last stamp accepted, so a jump can
	// be told apart from a lap: both move the position backwards.
	observedGen atomic.Uint32
}

var _ practice.Playhead = (*Player)(nil)

// NewPlayer returns a stopped player over a song of the given length.
func NewPlayer(clock practice.Clock, end practice.Frame) *Player {
	p := &Player{clock: clock}
	p.end.Store(int64(end))
	p.speedBits.Store(math.Float64bits(1))
	return p
}

func (p *Player) Clock() practice.Clock     { return p.clock }
func (p *Player) Position() practice.Frame  { return practice.Frame(p.pos.Load()) }
func (p *Player) End() practice.Frame       { return practice.Frame(p.end.Load()) }
func (p *Player) Playing() bool             { return p.playing.Load() }
func (p *Player) Laps() int                 { return int(p.laps.Load()) }
func (p *Player) SetEnd(end practice.Frame) { p.end.Store(int64(end)) }

// Finished reports that the song ran out. An active loop never finishes: the
// region is the run.
func (p *Player) Finished() bool {
	if p.Loop().Active() {
		return false
	}
	return p.pos.Load() >= p.end.Load()
}

func (p *Player) Play() {
	// Pressing play at the end of the song should replay it, not sit there.
	if p.Finished() {
		p.Seek(p.startOfRun())
	}
	p.playing.Store(true)
}

func (p *Player) Pause() { p.playing.Store(false) }

func (p *Player) TogglePlay() {
	if p.playing.Load() {
		p.Pause()
		return
	}
	p.Play()
}

func (p *Player) Speed() float64 {
	s := math.Float64frombits(p.speedBits.Load())
	if s <= 0 {
		return 1
	}
	return s
}

func (p *Player) SetSpeed(s float64) {
	p.speedBits.Store(math.Float64bits(practice.ClampSpeed(s)))
}

func (p *Player) Loop() practice.Loop {
	return practice.Loop{
		A:       practice.Frame(p.loopA.Load()),
		B:       practice.Frame(p.loopB.Load()),
		Enabled: p.loopOn.Load(),
	}
}

// SetLoop installs an A-B region. As with the pure Transport, a playhead
// already past the region is pulled into it, because a loop that appears to do
// nothing until the song ends is worse than one that jumps.
func (p *Player) SetLoop(l practice.Loop) {
	p.loopA.Store(int64(l.A))
	p.loopB.Store(int64(l.B))
	p.loopOn.Store(l.Enabled)
	if l.Active() && p.Position() >= l.B {
		p.Seek(l.A)
	}
}

// Seek jumps the playhead. The position moves immediately so the view responds
// to the keystroke, and the generation bump tells the renderer to throw away
// what it was about to produce.
//
// The generation goes up first. A callback landing between the position store
// and the bump would still match the old generation, pass observe's staleness
// guard and put the pre-seek position back; bumping first makes that same
// guard reject it.
func (p *Player) Seek(to practice.Frame) {
	if to < 0 {
		to = 0
	}
	if end := p.End(); to > end {
		to = end
	}
	p.seekGen.Add(1)
	p.seekTarget.Store(int64(to))
	p.pos.Store(int64(to))
}

// Restart returns to the start of the loop region, or of the song.
func (p *Player) Restart() { p.Seek(p.startOfRun()) }

func (p *Player) startOfRun() practice.Frame {
	if l := p.Loop(); l.Active() {
		return l.A
	}
	return 0
}

// SeekGeneration is the current jump counter, used to stamp and to validate
// rendered audio.
func (p *Player) SeekGeneration() uint32 { return p.seekGen.Load() }

// SeekTarget is where the renderer should resume from after a jump.
func (p *Player) SeekTarget() practice.Frame { return practice.Frame(p.seekTarget.Load()) }

// observe is called by the audio callback with the stamp of the last frame the
// device actually received. Stamps from a superseded generation are dropped.
//
// A lap is counted here, and that placement is the whole point. The renderer
// wraps its cursor a whole output buffer ahead of what is being heard — about
// 70 ms — so counting there told the scoreboard a lap had finished while the
// playhead was still short of B. The scoring session would reset for the new
// lap and then immediately expire every note in it, because they were all
// still behind a playhead that had not wrapped yet. Every lap after the first
// scored zero, and with the progressive rule on, the speed fell to the floor.
//
// The position going backwards is what a wrap looks like from here. A seek
// looks the same, and is told apart by its generation: the first stamp of a
// new generation is a jump, not a lap.
func (p *Player) observe(s stamp) {
	gen := p.seekGen.Load()
	if s.gen != gen {
		return
	}

	prev := p.pos.Load()
	p.pos.Store(s.pos)

	if p.observedGen.Load() == gen && s.pos < prev && p.Loop().Active() {
		p.laps.Add(1)
	}
	p.observedGen.Store(gen)
}
