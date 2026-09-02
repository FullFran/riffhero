// Package ui holds presentation logic for the practice view. It maps domain
// values to screen coordinates and deliberately imports no rendering library,
// so the layout stays testable headlessly.
package ui

import (
	"math"

	"github.com/FullFran/riffhero/internal/practice"
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

func NewLayout(width, height float64, clock practice.Clock) Layout {
	spacing := height * 0.09
	return Layout{
		Width:           width,
		Height:          height,
		PlayheadX:       width * 0.28,
		PixelsPerSecond: 220,
		TopString:       height * 0.26,
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
