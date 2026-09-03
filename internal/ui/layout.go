// Package ui holds presentation logic for the practice view. It maps domain
// values to screen coordinates and deliberately imports no rendering library,
// so the layout stays testable headlessly.
package ui

import (
	"math"

	"github.com/FullFran/riffhero/internal/practice"
)

// HeaderHeight and FooterHeight are the panels the tab must not run under.
// The layout knows about them because it is the thing that decides where the
// strings go, and a tab that slides under the scoreboard is unreadable.
const (
	HeaderHeight = 46
	FooterHeight = 74

	// tabMargin is the clearance either side of the strings, for bar numbers
	// above and the rating below.
	tabMargin = 44

	// maxStringSpacing stops a very tall window spreading six strings across
	// half a metre of screen, where the eye has to travel between them.
	maxStringSpacing = 78
	minStringSpacing = 18
)

// Layout maps the practice timeline onto a scrolling six-string tab. Notes
// travel right to left and are due when they cross the playhead.
type Layout struct {
	Width           float64
	Height          float64
	PlayheadX       float64
	PixelsPerSecond float64
	TopString       float64 // y of string 1 (high E)
	StringSpacing   float64

	clock practice.Clock
}

// NewLayout centres the tab in whatever room is left between the panels.
//
// The window is whatever the window manager decides — a tiling desktop will
// hand this a tall half-screen without asking — so the geometry is derived
// from the space available rather than from fractions of the total height,
// which left a third of the window empty at the top and bottom.
func NewLayout(width, height float64, clock practice.Clock) Layout {
	const top = HeaderHeight + tabMargin
	bottom := height - FooterHeight - tabMargin

	spacing, first := fitStrings(top, bottom)

	return Layout{
		Width:           width,
		Height:          height,
		PlayheadX:       width * 0.25,
		PixelsPerSecond: 220,
		TopString:       first,
		StringSpacing:   spacing,
		clock:           clock,
	}
}

// StringY returns the vertical position of a tab string, 1 (high E) on top.
func (l Layout) StringY(str uint8) float64 {
	switch {
	case str < 1:
		str = 1
	case str > 6:
		str = 6
	}
	return l.TopString + float64(str-1)*l.StringSpacing
}

// NoteX places a frame position on screen for the given playhead position.
func (l Layout) NoteX(now, at practice.Frame) float64 {
	return l.PlayheadX + l.clock.Seconds(at-now)*l.PixelsPerSecond
}

// VisibleWindow returns the frame range currently on screen, clamped at the
// start of the song.
func (l Layout) VisibleWindow(now practice.Frame) (from, to practice.Frame) {
	from = now - l.framesForPixels(l.PlayheadX)
	if from < 0 {
		from = 0
	}
	return from, now + l.framesForPixels(l.Width-l.PlayheadX)
}

func (l Layout) framesForPixels(px float64) practice.Frame {
	if l.PixelsPerSecond <= 0 || px <= 0 {
		return 0
	}
	return practice.Frame(math.Ceil(px / l.PixelsPerSecond * float64(l.clock.SampleRate)))
}

// LoopBounds is the on-screen span of an A-B region, so the view can shade it.
// visible is false when the region is entirely off screen.
func (l Layout) LoopBounds(now practice.Frame, loop practice.Loop) (x1, x2 float64, visible bool) {
	if !loop.Active() {
		return 0, 0, false
	}
	x1, x2 = l.NoteX(now, loop.A), l.NoteX(now, loop.B)
	if x2 < 0 || x1 > l.Width {
		return 0, 0, false
	}
	return x1, x2, true
}

// VisibleBars returns the bars that intersect the screen at this playhead
// position, for drawing bar lines and numbers.
func (l Layout) VisibleBars(now practice.Frame, grid practice.Grid) []practice.Bar {
	from, to := l.VisibleWindow(now)
	var out []practice.Bar
	for _, bar := range grid {
		if bar.End < from {
			continue
		}
		if bar.Start > to {
			break
		}
		out = append(out, bar)
	}
	return out
}

// Band is the vertical space between the header and the footer that a reading
// is drawn in.
func (l Layout) Band() (top, bottom float64) {
	return HeaderHeight + tabMargin, l.Height - FooterHeight - tabMargin
}

// WithBand re-centres the tab inside a narrower band, which is how it makes
// room for standard notation above it when both readings are shown.
func (l Layout) WithBand(top, bottom float64) Layout {
	l.StringSpacing, l.TopString = fitStrings(top, bottom)
	return l
}

// fitStrings spaces six strings inside a band and says where the first one
// goes.
//
// The centring is done after the clamp, and then the origin is pinned to the
// top of the band. Without that pin a band thinner than the minimum spacing
// needs gets a negative offset, and the tab is drawn *above* the space it was
// handed — over the staff in a window short enough, or under the header in a
// tab-only one. Overflowing downwards is ugly; overflowing upwards is two
// readings on top of each other.
func fitStrings(top, bottom float64) (spacing, first float64) {
	spacing = (bottom - top) / 5
	switch {
	case spacing > maxStringSpacing:
		spacing = maxStringSpacing
	case spacing < minStringSpacing:
		spacing = minStringSpacing
	}
	first = top + ((bottom-top)-spacing*5)/2
	if first < top {
		first = top
	}
	return spacing, first
}
