package practice

// Transport is the authoritative playhead of a practice run. It is driven by
// sample frames only: nothing here reads the wall clock, so a run is
// reproducible whether frames come from an audio callback or from a test.
type Transport struct {
	clock   Clock
	end     Frame
	pos     Frame
	playing bool
}

func NewTransport(clock Clock, end Frame) *Transport {
	if end < 0 {
		end = 0
	}
	return &Transport{clock: clock, end: end}
}

func (t *Transport) Position() Frame { return t.pos }
func (t *Transport) End() Frame      { return t.end }
func (t *Transport) Playing() bool   { return t.playing }
func (t *Transport) Clock() Clock    { return t.clock }

// Finished reports whether the playhead reached the end of the song.
func (t *Transport) Finished() bool { return t.pos >= t.end }

func (t *Transport) Play()  { t.playing = true }
func (t *Transport) Pause() { t.playing = false }

func (t *Transport) TogglePlay() {
	t.playing = !t.playing
}

// Advance moves the playhead by n frames while playing, stopping at the end.
func (t *Transport) Advance(n Frame) {
	if !t.playing || n <= 0 {
		return
	}
	t.pos += n
	if t.pos >= t.end {
		t.pos = t.end
		t.playing = false
	}
}

// AdvanceSeconds is the frame-rate-driven entry point used by the UI loop.
func (t *Transport) AdvanceSeconds(seconds float64) {
	t.Advance(t.clock.Frames(seconds))
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

// Restart returns to the beginning and keeps playing if it was playing.
func (t *Transport) Restart() { t.pos = 0 }
