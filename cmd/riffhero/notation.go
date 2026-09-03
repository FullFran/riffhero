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

// staffReach is how far beyond the outer staff lines a note can be drawn, in
// staff-line gaps.
//
// It is not a guess. A guitar's low E is written three ledger lines below the
// staff (step -7, so 3.5 gaps), and a stem hangs 3.2 gaps from the head with a
// note head half a gap thick on top of that. Reserving less than this is how
// the low E ends up drawn through the tab underneath it and the high notes end
// up behind the header.
const staffReach = 3.5 + 3.2 + 0.5

// staffFor places a five-line staff inside a vertical band, leaving room for
// everything that hangs off it.
func staffFor(top, bottom float64) ui.StaffLayout {
	// The staff itself is four gaps tall; the reach either side is what makes
	// the divisor eleven rather than four.
	gap := (bottom - top) / (4 + 2*staffReach)
	switch {
	case gap > 20:
		gap = 20
	case gap < 4:
		gap = 4
	}
	return ui.StaffLayout{Top: top + ((bottom-top)-4*gap)/2, LineGap: gap}
}

// staffExtent is the vertical space a staff really occupies, ledger lines and
// stems included.
func staffExtent(s ui.StaffLayout) (top, bottom float64) {
	return s.LineY(0) - staffReach*s.LineGap, s.LineY(4) + staffReach*s.LineGap
}

// gutterX is where the reading area starts: everything left of it belongs to
// the clef and the string names.
const gutterX = 44

func (a *app) drawStaffLines(screen *ebiten.Image, s ui.StaffLayout) {
	left, right := float32(gutterX), float32(a.layout.Width)-24
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
	x := float32(a.layout.NoteX(now, n.Start))
	if x < gutterX {
		// The left edge is the labels' column. A note drawn into it lands on
		// top of the clef or the string names.
		return
	}
	place := ui.PlaceOnStaff(n.MIDI, a.spelling)
	y := float32(s.Y(place.Step))

	// A note head is round. An earlier version drew it as a thick line, which
	// with square caps is a rectangle and reads as a bar rather than a note.
	head := float32(s.LineGap * 0.42)

	// Ledger lines first, so the head sits on top of them.
	for _, step := range place.Ledgers {
		ly := float32(s.Y(step))
		vector.StrokeLine(screen, x-head*2, ly, x+head*2, ly, 1, colorString, false)
	}

	rhythm := ui.ClassifyDuration(a.clock, a.barBPM(n.Start), n.Duration)

	if highlight {
		vector.StrokeCircle(screen, x, y, head*2.4, 2, colorNext, true)
	}
	drawNoteHead(screen, x, y, head, rhythm.Value.Hollow(), c)

	if rhythm.Value.Stem() {
		drawStem(screen, x, y, head, float32(s.LineGap), rhythm.Value.Flags(), ui.StemUp(place.Step), c)
	}
	if rhythm.Dotted {
		vector.DrawFilledCircle(screen, x+head*2, y-head, 1.6, c, true)
	}
	if acc := place.Accidental.String(); acc != "" {
		drawTinted(screen, acc, int(x-head)-glyphW-3, int(y)-glyphH/2, c)
	}
}

// drawNoteHead is filled for a quarter and shorter, open for a half and a
// whole. A real engraver would draw a slanted ellipse; at a dozen pixels a
// circle is indistinguishable and does not need a path.
func drawNoteHead(screen *ebiten.Image, x, y, r float32, hollow bool, c color.RGBA) {
	if hollow {
		vector.StrokeCircle(screen, x, y, r, 1.8, c, true)
		return
	}
	vector.DrawFilledCircle(screen, x, y, r, c, true)
}

func drawStem(screen *ebiten.Image, x, y, head, gap float32, flags int, up bool, c color.RGBA) {
	length := gap * 3.2
	sx, dir := x+head, float32(-1)
	if !up {
		sx, dir = x-head, 1
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
