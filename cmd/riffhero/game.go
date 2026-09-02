package main

import (
	"fmt"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/FullFran/riffhero/internal/practice"
	"github.com/FullFran/riffhero/internal/ui"
)

// noticeFrames is how long a one-line confirmation stays on screen. Long
// enough to read, short enough not to become furniture.
const noticeFrames = 150

func (a *app) Layout(outsideWidth, outsideHeight int) (int, int) {
	if outsideWidth != a.width || outsideHeight != a.height {
		a.width, a.height = outsideWidth, outsideHeight
		a.layout = ui.NewLayout(float64(outsideWidth), float64(outsideHeight), a.clock)
	}
	return outsideWidth, outsideHeight
}

func (a *app) Update() error {
	if inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
		return ebiten.Termination
	}
	a.handleKeys()

	// Only the scripted path needs the game loop to move time. With a device
	// open, the callback has already moved it, and asking the loop to move it
	// too would be two clocks pretending to be one.
	if a.transport != nil {
		tps := ebiten.TPS()
		if tps <= 0 {
			tps = 60
		}
		a.transport.AdvanceSeconds(1 / float64(tps))
	}

	update := a.runner.Update()
	if n := len(update.Fed); n > 0 {
		a.lastNote, a.hasNote = update.Fed[n-1].Detected, true
	}

	if update.LapDone && a.runner.Adaptive() && update.Adjustment != practice.Repeat {
		a.showNotice(fmt.Sprintf("lap %.0f%% — %s, now %s",
			update.LapStats.Accuracy*100, update.Adjustment, ui.SpeedLabel(update.Speed)))
	}
	if a.noticeTicks > 0 {
		a.noticeTicks--
	}
	return nil
}

func (a *app) handleKeys() {
	switch {
	case inpututil.IsKeyJustPressed(ebiten.KeySpace):
		a.head.TogglePlay()
	case inpututil.IsKeyJustPressed(ebiten.KeyR):
		a.runner.Restart()
		a.head.Play()
	case inpututil.IsKeyJustPressed(ebiten.KeyH), inpututil.IsKeyJustPressed(ebiten.KeyF1):
		a.showHelp = !a.showHelp
	}

	a.handleSeek()
	a.handleLoop()
	a.handleMix()

	if inpututil.IsKeyJustPressed(ebiten.KeyP) {
		a.runner.SetAdaptive(!a.runner.Adaptive())
		a.showNotice("progressive practice " + onOff(a.runner.Adaptive()))
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyTab) {
		a.cycleTrack()
	}
}

func (a *app) handleSeek() {
	switch {
	case inpututil.IsKeyJustPressed(ebiten.KeyArrowLeft):
		a.seekBars(-1)
	case inpututil.IsKeyJustPressed(ebiten.KeyArrowRight):
		a.seekBars(1)
	case inpututil.IsKeyJustPressed(ebiten.KeyHome):
		a.head.Seek(0)
	case inpututil.IsKeyJustPressed(ebiten.KeyEnd):
		a.head.Seek(a.head.End())
	}
}

// seekBars moves the playhead by whole bars, because that is the unit a
// musician navigates in. With no grid it falls back to seconds.
func (a *app) seekBars(delta int) {
	pos := a.head.Position()
	grid := a.song.Grid

	if i := grid.BarAt(pos); i >= 0 {
		// Nudging back from just inside a bar should go to that bar's start
		// first, the way a DAW does, rather than skipping a whole bar.
		if delta < 0 && pos > grid[i].Start+a.clock.Frames(0.2) {
			a.head.Seek(grid[i].Start)
			return
		}
		target := i + delta
		if target < 0 {
			a.head.Seek(0)
			return
		}
		if target >= len(grid) {
			a.head.Seek(a.head.End())
			return
		}
		a.head.Seek(grid[target].Start)
		return
	}
	a.head.Seek(pos + practice.Frame(delta)*a.clock.Frames(2))
}

func (a *app) handleLoop() {
	switch {
	case inpututil.IsKeyJustPressed(ebiten.KeyA):
		a.loop.A = a.song.Grid.Snap(a.head.Position())
		if a.loop.B <= a.loop.A {
			a.loop.B = 0
			a.loop.Enabled = false
		}
		a.applyLoop("loop start")
	case inpututil.IsKeyJustPressed(ebiten.KeyB):
		a.loop.B = a.song.Grid.Snap(a.head.Position())
		if a.loop.B > a.loop.A {
			a.loop.Enabled = true
		}
		a.applyLoop("loop end")
	case inpututil.IsKeyJustPressed(ebiten.KeyL):
		a.loop.Enabled = !a.loop.Enabled && a.loop.B > a.loop.A
		a.applyLoop("loop")
	case inpututil.IsKeyJustPressed(ebiten.KeyX):
		a.loop = practice.Loop{}
		a.applyLoop("loop cleared")
	}
}

func (a *app) applyLoop(what string) {
	a.runner.SetLoop(a.loop)
	if a.loop.Active() {
		from, to := a.song.Grid.BarAt(a.loop.A), a.song.Grid.BarAt(a.loop.B-1)
		if from >= 0 && to >= 0 {
			a.showNotice(fmt.Sprintf("%s — bars %d to %d", what, a.song.Grid[from].Number, a.song.Grid[to].Number))
			return
		}
		a.showNotice(fmt.Sprintf("%s — %s to %s", what,
			ui.Timecode(a.clock, a.loop.A), ui.Timecode(a.clock, a.loop.B)))
		return
	}
	a.showNotice(what + " — off")
}

func (a *app) handleMix() {
	if inpututil.IsKeyJustPressed(ebiten.KeyBracketLeft) {
		a.head.SetSpeed(a.head.Speed() - 0.05)
		a.showNotice("speed " + ui.SpeedLabel(a.head.Speed()))
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyBracketRight) {
		a.head.SetSpeed(a.head.Speed() + 0.05)
		a.showNotice("speed " + ui.SpeedLabel(a.head.Speed()))
	}
	if a.engine == nil {
		return
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyMinus) {
		a.engine.SetVolume(a.engine.Volume() - 0.1)
		a.showNotice(fmt.Sprintf("backing %.0f%%", a.engine.Volume()*100))
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyEqual) {
		a.engine.SetVolume(a.engine.Volume() + 0.1)
		a.showNotice(fmt.Sprintf("backing %.0f%%", a.engine.Volume()*100))
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyM) {
		// Three steps rather than a continuous control: monitoring is either
		// off, a reference under the amp, or the only thing you can hear.
		switch {
		case a.engine.Monitor() < 0.05:
			a.engine.SetMonitor(0.35)
		case a.engine.Monitor() < 0.5:
			a.engine.SetMonitor(0.8)
		default:
			a.engine.SetMonitor(0)
		}
		a.showNotice(fmt.Sprintf("guitar monitor %.0f%%", a.engine.Monitor()*100))
	}
}

// cycleTrack moves to the next track that has notes, rebuilding the run.
func (a *app) cycleTrack() {
	if len(a.song.Tracks) < 2 {
		return
	}
	for i := 1; i <= len(a.song.Tracks); i++ {
		next := (a.track + i) % len(a.song.Tracks)
		if len(a.song.Tracks[next].Notes) == 0 {
			continue
		}
		a.track = next
		a.buildRunner()
		a.runner.SetLoop(a.loop)
		a.showNotice("track: " + a.song.Tracks[next].Name)
		return
	}
}

func (a *app) showNotice(text string) {
	a.notice, a.noticeTicks = text, noticeFrames
}

func onOff(v bool) string {
	if v {
		return "on"
	}
	return "off"
}
