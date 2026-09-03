package main

import (
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"github.com/FullFran/riffhero/internal/practice"
	"github.com/FullFran/riffhero/internal/ui"
)

// Standard notation, for reading the same part off a staff instead of off the
// tab.
//
// Guitar music is written in treble clef sounding an octave below what is
// written, which internal/ui takes care of; everything here is drawing. The
// staff scrolls on exactly the same timeline as the tab, so a note is at the
// same x in both readings and the two can be shown at once.

// staffFor is the five-line staff placed inside a vertical band.
func staffFor(top, bottom float64) ui.StaffLayout {
	gap := (bottom - top) / 4
	switch {
	case gap > 15:
		gap = 15
	case gap < 5:
		gap = 5
	}
	// Centred in the band, leaving the ledger lines room either side.
	height := 4 * gap
	return ui.StaffLayout{Top: top + ((bottom-top)-height)/2, LineGap: gap}
}

func (a *app) drawStaffLines(screen *ebiten.Image, s ui.StaffLayout) {
	left, right := float32(44), float32(a.layout.Width)-24
	for n := 0; n < 5; n++ {
		y := float32(s.LineY(n))
		vector.StrokeLine(screen, left, y, right, y, 1, colorString, false)
	}
	// There is no clef glyph in the debug font, so the gutter says what the
	// staff is in words. "8vb" is the part a guitarist needs: the notes sound
	// an octave below where they are written.
	drawTinted(screen, "8vb", 18, int(s.LineY(2))-6, menuDim)
}

// drawStaffGrid puts the bar lines through the staff, so the two readings line
// up visibly and not just arithmetically.
func (a *app) drawStaffGrid(screen *ebiten.Image, s ui.StaffLayout, now practice.Frame) {
	top, bottom := float32(s.LineY(0)), float32(s.LineY(4))
	for _, bar := range a.layout.VisibleBars(now, a.song.Grid) {
		for _, beat := range bar.Beats[1:] {
			x := float32(a.layout.NoteX(now, beat))
			vector.StrokeLine(screen, x, top, x, bottom, 1, colorBeatLine, false)
		}
		x := float32(a.layout.NoteX(now, bar.Start))
		vector.StrokeLine(screen, x, top, x, bottom, 1, colorBarLine, false)
	}
}

func (a *app) drawStaffNotes(screen *ebiten.Image, s ui.StaffLayout, now practice.Frame) {
	from, to := a.layout.VisibleWindow(now)
	session := a.runner.Session()

	// Whatever is outside the practised region is still drawn, dimmed, so the
	// player can see what is coming after the loop.
	if scoped := a.runner.Scope(); len(scoped) != len(a.runner.All()) {
		for _, n := range a.runner.All() {
			if n.Start < from || n.Start > to || a.loop.Contains(n.Start) {
				continue
			}
			a.drawStaffNote(screen, s, now, n, colorBarLine, false)
		}
	}

	next, hasNext := session.NextExpected(now)
	for _, v := range session.Upcoming(from, to) {
		c := colorPending
		if v.Resolved {
			c = ratingColor(v.Rating)
		}
		a.drawStaffNote(screen, s, now, v.Note, c, hasNext && v.Index == next.Index && !v.Resolved)
	}
}

func (a *app) drawStaffNote(screen *ebiten.Image, s ui.StaffLayout, now practice.Frame, n practice.Note, c color.RGBA, highlight bool) {
	place := ui.PlaceOnStaff(n.MIDI, a.spelling)
	x := float32(a.layout.NoteX(now, n.Start))
	y := float32(s.Y(place.Step))

	headW := float32(s.LineGap * 0.72)
	headH := float32(s.LineGap * 0.52)

	// Ledger lines first, so the head sits on top of them.
	for _, step := range place.Ledgers {
		ly := float32(s.Y(step))
		vector.StrokeLine(screen, x-headW*1.5, ly, x+headW*1.5, ly, 1, colorString, false)
	}

	rhythm := ui.ClassifyDuration(a.clock, a.barBPM(n.Start), n.Duration)

	if highlight {
		vector.StrokeCircle(screen, x, y, headW*1.6, 2, colorNext, true)
	}
	drawNoteHead(screen, x, y, headW, headH, rhythm.Value.Hollow(), c)

	if rhythm.Value.Stem() {
		drawStem(screen, x, y, headW, float32(s.LineGap), rhythm.Value.Flags(), ui.StemUp(place.Step), c)
	}
	if rhythm.Dotted {
		vector.DrawFilledCircle(screen, x+headW*1.5, y-headH*0.6, 1.6, c, true)
	}
	if acc := place.Accidental.String(); acc != "" {
		drawTinted(screen, acc, int(x-headW)-glyphW-2, int(y)-glyphH/2, c)
	}
}

// drawNoteHead is an ellipse, filled for a quarter and shorter, open for a
// half and a whole. Ebiten draws circles, so the ellipse is a circle scaled on
// one axis by drawing a short thick line — cheaper than a path and, at this
// size, indistinguishable.
func drawNoteHead(screen *ebiten.Image, x, y, w, h float32, hollow bool, c color.RGBA) {
	if hollow {
		vector.StrokeLine(screen, x-w/2, y, x+w/2, y, h, c, true)
		vector.StrokeLine(screen, x-w/2+1, y, x+w/2-1, y, h-2.6, colorBackground, true)
		return
	}
	vector.StrokeLine(screen, x-w/2, y, x+w/2, y, h, c, true)
}

func drawStem(screen *ebiten.Image, x, y, headW, gap float32, flags int, up bool, c color.RGBA) {
	length := gap * 3.5
	sx, dir := x+headW/2, float32(-1)
	if !up {
		sx, dir = x-headW/2, 1
	}
	end := y + dir*length
	vector.StrokeLine(screen, sx, y, sx, end, 1.4, c, false)

	// Flags hang off the far end of the stem, one per halving of the value.
	for i := 0; i < flags; i++ {
		fy := end + dir*float32(i)*gap*0.5
		vector.StrokeLine(screen, sx, fy, sx+gap*0.8, fy-dir*gap*0.9, 1.4, c, true)
	}
}

// barBPM is the tempo in force where a note sits, which is what decides
// whether its duration is a quarter or an eighth.
func (a *app) barBPM(at practice.Frame) float64 {
	if i := a.song.Grid.BarAt(at); i >= 0 {
		return a.song.Grid[i].BPM
	}
	if len(a.song.Grid) > 0 {
		return a.song.Grid[0].BPM
	}
	return 120
}
