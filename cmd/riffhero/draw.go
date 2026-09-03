package main

import (
	"fmt"
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"github.com/FullFran/riffhero/internal/i18n"
	"github.com/FullFran/riffhero/internal/practice"
	"github.com/FullFran/riffhero/internal/ui"
)

var (
	colorBackground = color.RGBA{0x12, 0x14, 0x18, 0xff}
	colorPanel      = color.RGBA{0x1a, 0x1d, 0x23, 0xff}
	colorString     = color.RGBA{0x3a, 0x40, 0x4a, 0xff}
	colorBarLine    = color.RGBA{0x2a, 0x2f, 0x38, 0xff}
	colorBeatLine   = color.RGBA{0x20, 0x24, 0x2b, 0xff}
	colorPlayhead   = color.RGBA{0xf2, 0xc0, 0x4c, 0xff}
	colorPending    = color.RGBA{0x8a, 0x93, 0xa4, 0xff}
	colorNext       = color.RGBA{0xe8, 0xe4, 0xd8, 0xff}
	colorPerfect    = color.RGBA{0x4c, 0xd9, 0x7a, 0xff}
	colorGood       = color.RGBA{0x4c, 0x9a, 0xd9, 0xff}
	colorMiss       = color.RGBA{0xd9, 0x4c, 0x5a, 0xff}
	colorLoop       = color.RGBA{0x2b, 0x3a, 0x33, 0xff}
	colorLoopEdge   = color.RGBA{0x4c, 0xd9, 0x7a, 0x80}
	colorMeter      = color.RGBA{0x4c, 0xd9, 0x7a, 0xff}
	colorMeterBed   = color.RGBA{0x25, 0x2a, 0x32, 0xff}
)

// drawPractice is the reading view: whichever of the tab and the staff is
// switched on, the playhead, and the scoreboard.
//
// Both readings scroll on one timeline, so a note is at the same x in each and
// showing them together lines up rather than merely coexisting. The vertical
// space is what they share, and the tab gives up rather more than half of it
// when they do — five staff lines with ledgers either side need the room, and
// six strings survive being squeezed.
func (a *app) drawPractice(screen *ebiten.Image) {
	screen.Fill(colorBackground)

	now := a.head.Position()
	top, bottom := a.layout.Band()

	a.tab = a.layout
	staffOn := a.showsStaff()
	switch {
	case staffOn && a.showsTab():
		mid := top + (bottom-top)*0.52
		a.staff = staffFor(top, mid)
		a.tab = a.layout.WithBand(mid+10, bottom)
	case staffOn:
		a.staff = staffFor(top, bottom)
	}

	a.drawLoopRegion(screen, now)
	if staffOn {
		a.drawStaffLines(screen, a.staff)
		a.drawStaffGrid(screen, a.staff, now)
		a.drawStaffNotes(screen, a.staff, now)
	}
	if a.showsTab() {
		a.drawGrid(screen, now)
		a.drawStrings(screen)
		a.drawNotes(screen, now)
	}
	a.drawBarNumbers(screen, now)
	a.drawPlayhead(screen, now)
	a.drawHUD(screen)

	if a.showHelp {
		a.drawHelp(screen)
	}
}

// drawLoopRegion shades the A-B section behind everything else, so the player
// can see at a glance which part of the song is actually being scored.
func (a *app) drawLoopRegion(screen *ebiten.Image, now practice.Frame) {
	x1, x2, ok := a.layout.LoopBounds(now, a.loop)
	if !ok {
		return
	}
	top, bottom := a.readingBand()

	vector.DrawFilledRect(screen, float32(x1), top, float32(x2-x1), bottom-top, colorLoop, false)
	vector.StrokeLine(screen, float32(x1), top, float32(x1), bottom, 2, colorLoopEdge, false)
	vector.StrokeLine(screen, float32(x2), top, float32(x2), bottom, 2, colorLoopEdge, false)
}

func (a *app) drawGrid(screen *ebiten.Image, now practice.Frame) {
	top := float32(a.tab.StringY(1)) - 22
	bottom := float32(a.tab.StringY(6)) + 22

	for _, bar := range a.layout.VisibleBars(now, a.song.Grid) {
		// Beats first so the bar line draws over them.
		for _, beat := range bar.Beats[1:] {
			x := float32(a.layout.NoteX(now, beat))
			vector.StrokeLine(screen, x, top, x, bottom, 1, colorBeatLine, false)
		}
		x := float32(a.layout.NoteX(now, bar.Start))
		vector.StrokeLine(screen, x, top, x, bottom, 1, colorBarLine, false)
	}
}

// drawBarNumbers labels the bars above everything on screen, once, whichever
// readings are showing.
//
// Above the tab is where they used to go, which on the staff meant on top of
// its low notes — and in this music that is the open low E on the downbeat,
// the number and the note in the same place every bar.
func (a *app) drawBarNumbers(screen *ebiten.Image, now practice.Frame) {
	top, _ := a.readingBand()
	y := int(top) - lineH
	if y < ui.HeaderHeight+2 {
		y = ui.HeaderHeight + 2
	}
	for _, bar := range a.layout.VisibleBars(now, a.song.Grid) {
		x := a.layout.NoteX(now, bar.Start)
		if x < gutterX {
			continue
		}
		drawTinted(screen, fmt.Sprintf("%d", bar.Number), int(x)+3, y, menuDim)
	}
}

func (a *app) drawStrings(screen *ebiten.Image) {
	labels := ui.StringLabels(a.tuning())
	for s := uint8(1); s <= 6; s++ {
		y := float32(a.tab.StringY(s))
		vector.StrokeLine(screen, gutterX, y, float32(a.layout.Width)-24, y, 1, colorString, false)
		ebitenutil.DebugPrintAt(screen, labels[s-1], 26, int(y)-7)
	}
}

func (a *app) drawNotes(screen *ebiten.Image, now practice.Frame) {
	from, to := a.layout.VisibleWindow(now)
	session := a.runner.Session()

	// Notes outside the practised region still get drawn, dimmed: the player
	// needs to see what comes after the loop to know where they are.
	if scoped := a.runner.Scope(); len(scoped) != len(a.runner.All()) {
		for _, n := range a.runner.All() {
			if n.Start < from || n.Start > to || a.loop.Contains(n.Start) {
				continue
			}
			a.drawNote(screen, now, n, colorBarLine, false)
		}
	}

	next, hasNext := session.NextExpected(now)
	for _, v := range session.Upcoming(from, to) {
		c := colorPending
		if v.Resolved {
			c = ratingColor(v.Rating)
		}
		a.drawNote(screen, now, v.Note, c, hasNext && v.Index == next.Index && !v.Resolved)
	}
}

func (a *app) drawNote(screen *ebiten.Image, now practice.Frame, n practice.Note, c color.RGBA, highlight bool) {
	x := float32(a.layout.NoteX(now, n.Start))
	if x < gutterX {
		// The left edge is the string names' column; a note drawn into it
		// lands on top of them.
		return
	}
	y := float32(a.tab.StringY(n.String))

	// A held note is drawn as a bar rather than a dot, so a whole note and a
	// sixteenth do not look like the same instruction.
	if width := float32(a.layout.NoteX(now, n.Start+n.Duration)) - x; width > 26 {
		vector.DrawFilledRect(screen, x, y-3, width-6, 6, dim(c, 0.35), false)
	}
	if highlight {
		vector.StrokeCircle(screen, x, y, 15, 2, colorNext, true)
	}
	vector.DrawFilledCircle(screen, x, y, 11, c, true)
	ebitenutil.DebugPrintAt(screen, fmt.Sprintf("%d", n.Fret), int(x)-fretOffset(n.Fret), int(y)-7)
}

func fretOffset(fret uint8) int {
	if fret >= 10 {
		return 6
	}
	return 3
}

func (a *app) drawPlayhead(screen *ebiten.Image, now practice.Frame) {
	x := float32(a.layout.PlayheadX)
	top, bottom := a.readingBand()
	vector.StrokeLine(screen, x, top, x, bottom, 2, colorPlayhead, false)

	if a.runner.Session().HasRating() {
		label := ui.RatingLabel(a.runner.Session().LastRating())
		ebitenutil.DebugPrintAt(screen, label, int(x-float32(textWidth(label))/2), int(bottom)+10)
	}
	_ = now
}

func (a *app) drawHUD(screen *ebiten.Image) {
	hud := ui.BuildHUD(a.hudInput())

	vector.DrawFilledRect(screen, 0, 0, float32(a.layout.Width), 46, colorPanel, false)
	ebitenutil.DebugPrintAt(screen, hud.Title, 16, 10)
	ebitenutil.DebugPrintAt(screen, hud.Transport, 16, 26)

	bottom := int(a.layout.Height)
	vector.DrawFilledRect(screen, 0, float32(bottom-74), float32(a.layout.Width), 74, colorPanel, false)
	ebitenutil.DebugPrintAt(screen, hud.Score, 16, bottom-64)
	ebitenutil.DebugPrintAt(screen, hud.Practice, 16, bottom-48)
	ebitenutil.DebugPrintAt(screen, hud.Input, 16, bottom-32)
	hint := i18n.T("H help    ESC menu")
	drawDim(screen, hint, int(a.layout.Width)-16-int(textWidth(hint)), bottom-16)

	a.drawMeter(screen, bottom)

	y := 56
	for _, w := range a.warnings {
		ebitenutil.DebugPrintAt(screen, "! "+w, 16, y)
		y += 14
	}
	for _, w := range hud.Warnings {
		ebitenutil.DebugPrintAt(screen, "! "+w, 16, y)
		y += 14
	}
	if a.noticeTicks > 0 {
		ebitenutil.DebugPrintAt(screen, a.notice, 16, y+4)
	}
}

// readingBand is the vertical span the playhead and the loop shading cover.
//
// It is what is actually on show, not the space that was available. Shading a
// whole empty window green because one staff is being drawn in the middle of
// it tells the player nothing about where the region is.
func (a *app) readingBand() (top, bottom float32) {
	t, b := 0.0, 0.0
	set := false
	stretch := func(lo, hi float64) {
		if !set {
			t, b, set = lo, hi, true
			return
		}
		if lo < t {
			t = lo
		}
		if hi > b {
			b = hi
		}
	}

	if a.showsStaff() {
		stretch(staffExtent(a.staff))
	}
	if a.showsTab() {
		stretch(a.tab.StringY(1)-26, a.tab.StringY(6)+26)
	}
	if !set {
		lo, hi := a.layout.Band()
		return float32(lo), float32(hi)
	}
	return float32(t), float32(b)
}

// drawMeter is the input level, drawn rather than spelled out: a bar is read
// at a glance and a number is not, and the first question when nothing scores
// is always whether the guitar is being heard at all.
func (a *app) drawMeter(screen *ebiten.Image, bottom int) {
	if a.det == nil {
		return
	}
	const w, h = 180, 8
	x := float32(a.layout.Width) - w - 16
	y := float32(bottom - 44)

	vector.DrawFilledRect(screen, x, y, w, h, colorMeterBed, false)
	level := float32(ui.MeterLevel(a.det.Level()))
	c := colorMeter
	if a.det.Level() > -3 {
		c = colorMiss // clipping: the player has to see this before the notes stop resolving
	}
	vector.DrawFilledRect(screen, x, y, w*level, h, c, false)
}

func (a *app) hudInput() ui.HUDInput {
	session := a.runner.Session()
	in := ui.HUDInput{
		Clock:    a.clock,
		Grid:     a.song.Grid,
		Tuning:   a.tuning(),
		Title:    a.song.Title,
		Artist:   a.song.Artist,
		Track:    a.song.Tracks[a.track].Name,
		Position: a.head.Position(),
		End:      a.head.End(),
		Playing:  a.head.Playing(),
		Finished: a.head.Finished(),
		Speed:    a.head.Speed(),
		Loop:     a.loop,
		Adaptive: a.runner.Adaptive(),
		Stats:    session.Stats(),
		LastLap:  a.runner.LastLap(),
		HasLap:   a.runner.LastLap().Total > 0,
		Rating:   session.LastRating(),
		HasRatng: session.HasRating(),
		Live:     a.live(),
		Backing:  a.backingPath != "",

		Detected:    a.lastNote,
		HasDetected: a.hasNote,
	}
	if a.det != nil {
		in.Level = a.det.Level()
		in.Present = a.det.Present()
		in.Dropped = a.det.Dropped()
		in.Latency = a.det.LatencyOffset
		in.Calibrated = a.calibrated
	}
	if a.engine != nil {
		in.Underruns = a.engine.Underruns()
	}
	return in
}

// helpLines is worked out afresh every time it is drawn rather than once at
// init: the language is settled after the package variables are, and a table
// built too early stays in the language the process started in.
func helpLines() []string {
	return []string{
		"RiffHero",
		"",
		i18n.T("  ESC        back to the menu"),
		i18n.T("  S          settings"),
		i18n.T("  N          tablature / notation / both"),
		i18n.T("  SPACE      play / pause"),
		i18n.T("  R          restart and clear the scoreboard"),
		i18n.T("  LEFT/RIGHT seek a bar back / forward"),
		i18n.T("  HOME/END   jump to the start / the end"),
		"",
		i18n.T("  A          set the loop start at the playhead"),
		i18n.T("  B          set the loop end"),
		i18n.T("  L          loop on / off"),
		i18n.T("  X          clear the loop"),
		"",
		i18n.T("  [ ]        practice speed down / up"),
		i18n.T("  P          progressive practice on / off"),
		i18n.T("  - =        backing track quieter / louder"),
		i18n.T("  M          guitar monitoring level"),
		i18n.T("  TAB        next track"),
		"",
		i18n.T("  H          this help"),
		i18n.T("  ESC        back to the menu"),
		"",
		i18n.T("Loop a hard bar, turn on progressive practice and play it clean:"),
		i18n.T("the speed comes up on its own until you are at tempo."),
	}
}

// drawHelp is the key list, clipped to the window rather than centred blindly.
//
// Centred on a window shorter than the panel, the first lines land above the
// top edge and the last below the bottom, and at 400 px wide the left margin
// goes negative and cuts two characters off every line. What does not fit is
// scrolled off the end instead, which loses the last few keys rather than the
// first few and the left of all of them.
func (a *app) drawHelp(screen *ebiten.Image) {
	const pad, lineStep = 18, 14

	lines := helpLines()

	w := float32(0)
	for _, line := range lines {
		if tw := float32(textWidth(line)) + 2*pad; tw > w {
			w = tw
		}
	}
	if limit := float32(a.layout.Width) - 24; w > limit {
		w = limit
	}
	x := float32(a.layout.Width)/2 - w/2
	if x < 12 {
		x = 12
	}

	visible := len(lines)
	if room := (int(a.layout.Height) - 40 - 32) / lineStep; visible > room {
		visible = room
	}
	if visible < 1 {
		return
	}
	h := float32(visible*lineStep + 32)
	y := float32(a.layout.Height)/2 - h/2
	if y < 12 {
		y = 12
	}

	vector.DrawFilledRect(screen, x, y, w, h, colorPanel, false)
	vector.StrokeRect(screen, x, y, w, h, 1, colorString, false)
	for i, line := range lines[:visible] {
		drawTinted(screen, clip(line, int(w-2*pad)/glyphW), int(x)+pad, int(y)+16+i*lineStep, menuInk)
	}
	if visible < len(lines) {
		drawDim(screen, "...", int(x)+pad, int(y+h)-16)
	}
}

func ratingColor(r practice.Rating) color.RGBA {
	switch r {
	case practice.Perfect:
		return colorPerfect
	case practice.Good:
		return colorGood
	default:
		return colorMiss
	}
}

// dim scales a colour towards the background, for notes outside the practised
// region.
func dim(c color.RGBA, f float64) color.RGBA {
	return color.RGBA{
		R: uint8(float64(c.R) * f),
		G: uint8(float64(c.G) * f),
		B: uint8(float64(c.B) * f),
		A: c.A,
	}
}
