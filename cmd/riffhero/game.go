package main

import (
	"fmt"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/FullFran/riffhero/internal/config"
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
	if a.quitting {
		return ebiten.Termination
	}

	switch a.mode {
	case atTitle:
		a.updateTitle()
	case inSongs:
		a.updateBrowser()
	case inSettings:
		a.updateSettings()
	case inDevices:
		a.updateDevices()
	case inCalibration:
		a.updateCalibration()
	case asking:
		a.updateAsking()
	default:
		a.updatePractice()
	}

	a.tickClock()

	// The run is advanced whatever screen is up. A device callback does not
	// stop for a menu, and the detector has to be drained or its ring fills
	// and starts dropping — which from the player's side looks like the guitar
	// going quiet.
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

// tickClock moves the playhead when nothing else is doing it.
//
// Only the scripted path needs this. With a device open the callback has
// already moved the playhead, and asking the game loop to move it too would be
// two clocks pretending to be one.
func (a *app) tickClock() {
	if a.transport == nil {
		return
	}
	tps := ebiten.TPS()
	if tps <= 0 {
		tps = 60
	}
	a.transport.AdvanceSeconds(1 / float64(tps))
}

func (a *app) Draw(screen *ebiten.Image) {
	switch a.mode {
	case atTitle:
		a.drawTitle(screen)
	case inSongs:
		a.drawBrowser(screen)
	case inSettings:
		a.drawSettings(screen)
	case inDevices:
		a.drawDevices(screen)
	case inCalibration:
		a.drawCalibration(screen)
	case asking:
		a.drawAsking(screen)
	default:
		a.drawPractice(screen)
	}
}

// ------------------------------------------------------------------ playing

func (a *app) updatePractice() {
	switch {
	case inpututil.IsKeyJustPressed(ebiten.KeyEscape):
		a.openTitle()
		return
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
		a.cfg.Progressive = a.runner.Adaptive()
		a.showNotice("progressive practice " + onOff(a.runner.Adaptive()))
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyN) {
		a.setNotation(a.notation.Next())
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyTab) {
		a.cycleTrack()
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyS) {
		a.openSettings()
	}
}

func (a *app) handleSeek() {
	switch {
	case inpututil.IsKeyJustPressed(ebiten.KeyArrowLeft):
		a.seekBars(-1)
	case inpututil.IsKeyJustPressed(ebiten.KeyArrowRight):
		a.seekBars(1)
	case inpututil.IsKeyJustPressed(ebiten.KeyHome):
		a.runner.Seek(0)
	case inpututil.IsKeyJustPressed(ebiten.KeyEnd):
		a.runner.Seek(a.head.End())
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
			a.runner.Seek(grid[i].Start)
			return
		}
		target := i + delta
		if target < 0 {
			a.runner.Seek(0)
			return
		}
		if target >= len(grid) {
			a.runner.Seek(a.head.End())
			return
		}
		a.runner.Seek(grid[target].Start)
		return
	}
	a.runner.Seek(pos + practice.Frame(delta)*a.clock.Frames(2))
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
		a.head.SetSpeed(a.head.Speed() - speedStep)
		a.showNotice("speed " + ui.SpeedLabel(a.head.Speed()))
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyBracketRight) {
		a.head.SetSpeed(a.head.Speed() + speedStep)
		a.showNotice("speed " + ui.SpeedLabel(a.head.Speed()))
	}
	if a.engine == nil {
		return
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyMinus) {
		a.nudgeVolume(-2 * volumeStep)
		a.showNotice("backing " + percent(a.engine.Volume()))
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyEqual) {
		a.nudgeVolume(2 * volumeStep)
		a.showNotice("backing " + percent(a.engine.Volume()))
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
		a.cfg.Monitor = a.engine.Monitor()
		a.showNotice("guitar monitor " + percent(a.engine.Monitor()))
	}
}

// cycleTrack moves to the next track that has notes.
func (a *app) cycleTrack() {
	if len(a.song.Tracks) < 2 {
		return
	}
	for i := 1; i <= len(a.song.Tracks); i++ {
		next := (a.track + i) % len(a.song.Tracks)
		if len(a.song.Tracks[next].Notes) == 0 {
			continue
		}
		a.useTrack(next)
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

// showsTab and showsStaff say which readings are on screen. Both at once is a
// real choice: reading the notation while the tab underneath says where the
// hand goes is how most people learn to read.
func (a *app) showsTab() bool {
	return a.notation == config.NotationTab || a.notation == config.NotationBoth
}

func (a *app) showsStaff() bool {
	return a.notation == config.NotationStaff || a.notation == config.NotationBoth
}
