package practice

// Playhead is what the UI and the scoring loop are allowed to know about the
// thing driving time. Two very different implementations satisfy it: the
// deterministic Transport below, stepped by the game loop, and the audio
// engine, stepped by the device callback. Everything above this interface is
// identical either way, which is what keeps a practice run reproducible in a
// test and honest on hardware.
type Playhead interface {
	Clock() Clock
	Position() Frame
	End() Frame
	Playing() bool
	Finished() bool

	Play()
	Pause()
	TogglePlay()
	Seek(to Frame)
	Restart()

	Speed() float64
	SetSpeed(s float64)
	Loop() Loop
	SetLoop(l Loop)

	// Laps counts how many times the A-B region has wrapped, so the caller can
	// notice a new lap without polling positions.
	Laps() int
}

// SpeedMin and SpeedMax bound practice playback rate. Below half speed a
// backing track stops being useful to play against; above 1.0 is not practice.
const (
	SpeedMin = 0.25
	SpeedMax = 1.0
)

// Transport is the authoritative playhead of a practice run when no audio
// device is driving one. It moves in sample frames only: nothing here reads the
// wall clock, so a run is reproducible whether frames come from a test or from
// a fixed-rate game loop.
type Transport struct {
	clock   Clock
	end     Frame
	pos     Frame
	playing bool
	speed   float64
	loop    Loop
	laps    int
}

func NewTransport(clock Clock, end Frame) *Transport {
	if end < 0 {
		end = 0
	}
	return &Transport{clock: clock, end: end, speed: 1}
}

func (t *Transport) Position() Frame { return t.pos }
func (t *Transport) End() Frame      { return t.end }
func (t *Transport) Playing() bool   { return t.playing }
func (t *Transport) Clock() Clock    { return t.clock }
func (t *Transport) Laps() int       { return t.laps }
func (t *Transport) Loop() Loop      { return t.loop }

// Finished reports whether the playhead reached the end of the song. A live
// loop never finishes: the region is the run.
func (t *Transport) Finished() bool { return !t.loop.Active() && t.pos >= t.end }

func (t *Transport) Play()  { t.playing = true }
func (t *Transport) Pause() { t.playing = false }

func (t *Transport) TogglePlay() {
	t.playing = !t.playing
}

// Speed is the practice playback rate, where 1 is the written tempo.
func (t *Transport) Speed() float64 {
	if t.speed <= 0 {
		return 1
	}
	return t.speed
}

// SetSpeed clamps to the practice range.
func (t *Transport) SetSpeed(s float64) { t.speed = ClampSpeed(s) }

// SetLoop installs an A-B region. Setting one while the playhead sits past its
// end pulls the playhead into the region, because the alternative is a loop
// that appears to do nothing until the song ends.
func (t *Transport) SetLoop(l Loop) {
	t.loop = l
	if l.Active() && t.pos >= l.B {
		t.pos = l.A
	}
}

// Advance moves the playhead by n frames while playing, wrapping inside the
// A-B region when one is set and stopping at the end of the song when not.
func (t *Transport) Advance(n Frame) {
	if !t.playing || n <= 0 {
		return
	}
	next, laps := t.loop.Step(t.pos, n)
	t.laps += laps
	t.pos = next

	if t.loop.Active() {
		return
	}
	if t.pos >= t.end {
		t.pos = t.end
		t.playing = false
	}
}

// AdvanceSeconds is the frame-rate-driven entry point used by the UI loop.
// Practice speed is applied here: half speed means half as much of the song
// passes per second of real time.
func (t *Transport) AdvanceSeconds(seconds float64) {
	t.Advance(t.clock.Frames(seconds * t.Speed()))
}

// Seek jumps the playhead, clamped to the song bounds, without changing the
// playing state.
func (t *Transport) Seek(to Frame) {
	switch {
	case to < 0:
		t.pos = 0
	case to > t.end:
		t.pos = t.end
	default:
		t.pos = to
	}
}

// Restart returns to the beginning of the loop region, or of the song when
// there is none, and keeps playing if it was playing.
func (t *Transport) Restart() {
	if t.loop.Active() {
		t.pos = t.loop.A
		return
	}
	t.pos = 0
}

// ClampSpeed keeps a rate inside the practice range.
func ClampSpeed(s float64) float64 {
	switch {
	case s < SpeedMin:
		return SpeedMin
	case s > SpeedMax:
		return SpeedMax
	default:
		return s
	}
}
