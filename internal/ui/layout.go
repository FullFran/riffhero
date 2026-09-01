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
