package audio

import (
	"testing"

	"github.com/FullFran/riffhero/internal/practice"
)

func testPlayer() *Player {
	return NewPlayer(practice.Clock{SampleRate: 48000}, 480000) // ten seconds
}

func TestPlayerStartsStoppedAtFullSpeed(t *testing.T) {
	p := testPlayer()
	if p.Playing() {
		t.Fatal("a fresh player must not be playing")
	}
	if got := p.Speed(); got != 1 {
		t.Fatalf("speed %v, want 1", got)
	}
	if got := p.Position(); got != 0 {
		t.Fatalf("position %d, want 0", got)
	}
}

func TestPlayerPositionFollowsTheDevice(t *testing.T) {
	// The position is whatever the device last played, not whatever the
	// renderer last produced: that is what makes "what you hear" and "what the
	// scorer sees" the same number.
	p := testPlayer()
	p.observe(stamp{pos: 12345, gen: p.SeekGeneration()})
	if got := p.Position(); got != 12345 {
		t.Fatalf("position %d, want 12345", got)
	}
}

func TestPlayerIgnoresAudioFromBeforeASeek(t *testing.T) {
	// Audio for the old position is already queued when the user jumps. Left
	// unchecked it would drag the playhead backwards for as long as it takes
	// to drain, and the view would slide back after every seek.
	p := testPlayer()
	stale := p.SeekGeneration()

	p.Seek(200000)
	p.observe(stamp{pos: 500, gen: stale})

	if got := p.Position(); got != 200000 {
		t.Fatalf("stale audio moved the playhead to %d", got)
	}
	p.observe(stamp{pos: 200480, gen: p.SeekGeneration()})
	if got := p.Position(); got != 200480 {
		t.Fatalf("fresh audio was ignored: position %d", got)
	}
}

func TestPlayerSeekMovesImmediately(t *testing.T) {
	// The keystroke has to feel instant even though the audio for the new
	// position has not been rendered yet.
	p := testPlayer()
	p.Seek(1000)
	if got := p.Position(); got != 1000 {
		t.Fatalf("position %d, want 1000", got)
	}
	if got := p.SeekTarget(); got != 1000 {
		t.Fatalf("seek target %d, want 1000", got)
	}
}

func TestPlayerSeekClampsToTheSong(t *testing.T) {
	p := testPlayer()
	p.Seek(-100)
	if got := p.Position(); got != 0 {
		t.Fatalf("position %d, want 0", got)
	}
	p.Seek(9999999)
	if got := p.Position(); got != p.End() {
		t.Fatalf("position %d, want the song end %d", got, p.End())
	}
}

func TestPlayerPlayAtTheEndReplays(t *testing.T) {
	p := testPlayer()
	p.Seek(p.End())
	if !p.Finished() {
		t.Fatal("the player should report finished at the end")
	}
	p.Play()
	if got := p.Position(); got != 0 {
		t.Fatalf("play at the end left the playhead at %d", got)
	}
	if !p.Playing() {
		t.Fatal("play should start playback")
	}
}

func TestPlayerLoopNeverFinishes(t *testing.T) {
	p := testPlayer()
	p.SetLoop(practice.Loop{A: 1000, B: 2000, Enabled: true})
	p.observe(stamp{pos: int64(p.End()), gen: p.SeekGeneration()})
	if p.Finished() {
		t.Fatal("a looping player must never finish")
	}
}

func TestPlayerSetLoopPullsAStrandedPlayheadIn(t *testing.T) {
	p := testPlayer()
	p.Seek(400000)
	p.SetLoop(practice.Loop{A: 1000, B: 2000, Enabled: true})
	if got := p.Position(); got != 1000 {
		t.Fatalf("position %d, want to be pulled into the region", got)
	}
}

func TestPlayerRestartGoesToTheRegionStart(t *testing.T) {
	p := testPlayer()
	p.SetLoop(practice.Loop{A: 1000, B: 2000, Enabled: true})
	p.Seek(1800)
	p.Restart()
	if got := p.Position(); got != 1000 {
		t.Fatalf("restart went to %d, want 1000", got)
	}

	p.SetLoop(practice.Loop{})
	p.Seek(5000)
	p.Restart()
	if got := p.Position(); got != 0 {
		t.Fatalf("restart without a loop went to %d, want 0", got)
	}
}

func TestPlayerSpeedIsClamped(t *testing.T) {
	p := testPlayer()
	p.SetSpeed(99)
	if got := p.Speed(); got != practice.SpeedMax {
		t.Fatalf("speed %v, want %v", got, practice.SpeedMax)
	}
	p.SetSpeed(0)
	if got := p.Speed(); got != practice.SpeedMin {
		t.Fatalf("speed %v, want %v", got, practice.SpeedMin)
	}
}

func TestPlayerTogglePlay(t *testing.T) {
	p := testPlayer()
	p.TogglePlay()
	if !p.Playing() {
		t.Fatal("toggle should start playback")
	}
	p.TogglePlay()
	if p.Playing() {
		t.Fatal("toggle should stop playback")
	}
}

func TestPlayerCountsALapWhenThePlayheadWraps(t *testing.T) {
	// The lap belongs to what was heard, not to what the renderer produced.
	// The renderer is a whole output buffer ahead — counting there resets the
	// scoreboard while the playhead is still short of B, and the scoring
	// session then expires the entire fresh lap as misses.
	p := testPlayer()
	p.SetLoop(practice.Loop{A: 1000, B: 5000, Enabled: true})
	gen := p.SeekGeneration()

	for _, pos := range []int64{1000, 3000, 4990} {
		p.observe(stamp{pos: pos, gen: gen})
	}
	if got := p.Laps(); got != 0 {
		t.Fatalf("laps %d before wrapping", got)
	}

	p.observe(stamp{pos: 1010, gen: gen}) // the wrap
	if got := p.Laps(); got != 1 {
		t.Fatalf("laps %d after the wrap, want 1", got)
	}
	if got := p.Position(); got != 1010 {
		t.Fatalf("position %d, want the wrapped position", got)
	}
}

func TestPlayerDoesNotCountASeekAsALap(t *testing.T) {
	// A jump moves the position backwards exactly like a wrap does. The
	// generation is what separates them.
	p := testPlayer()
	p.SetLoop(practice.Loop{A: 1000, B: 5000, Enabled: true})
	p.observe(stamp{pos: 4000, gen: p.SeekGeneration()})

	p.Seek(1000)
	p.observe(stamp{pos: 1000, gen: p.SeekGeneration()})

	if got := p.Laps(); got != 0 {
		t.Fatalf("a seek counted %d laps", got)
	}
}

func TestPlayerCountsNoLapWithoutALoop(t *testing.T) {
	// Without a region there is nothing to lap, and a position that went
	// backwards is a bug somewhere else rather than a repetition.
	p := testPlayer()
	gen := p.SeekGeneration()
	p.observe(stamp{pos: 5000, gen: gen})
	p.observe(stamp{pos: 100, gen: gen})
	if got := p.Laps(); got != 0 {
		t.Fatalf("laps %d without a loop", got)
	}
}
