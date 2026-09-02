package ui

import (
	"fmt"
	"math"
	"strings"

	"github.com/FullFran/riffhero/internal/practice"
)

var pitchNames = [12]string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}

// NoteName renders a MIDI note the way a guitarist reads it: middle C, MIDI
// 60, is C4.
func NoteName(midi uint8) string {
	return fmt.Sprintf("%s%d", pitchNames[midi%12], int(midi)/12-1)
}

// StringLabels are the open strings of a tuning, top row first, for the left
// edge of the tab. They come from the tuning rather than being hard-coded
// "eBGDAE" so a drop-D score does not lie about its bottom string.
func StringLabels(t practice.Tuning) [6]string {
	var out [6]string
	for s := uint8(1); s <= 6; s++ {
		name := pitchNames[t.Strings[s-1]%12]
		if s <= 3 {
			name = strings.ToLower(name)
		}
		out[s-1] = name
	}
	return out
}

// Timecode renders a position as m:ss.t.
func Timecode(clock practice.Clock, f practice.Frame) string {
	if f < 0 {
		f = 0
	}
	secs := clock.Seconds(f)
	m := int(secs) / 60
	s := secs - float64(m*60)
	return fmt.Sprintf("%d:%04.1f", m, s)
}

// Cents renders an intonation error with an explicit sign, because "12" and
// "sharp by 12" are different pieces of information to someone tuning up.
func Cents(c float64) string {
	return fmt.Sprintf("%+.0f¢", c)
}

// SpeedLabel renders a practice rate.
func SpeedLabel(s float64) string { return fmt.Sprintf("%.2fx", s) }

// MeterFloorDB is the quietest level the input meter shows. Below it a guitar
// is not being played, it is being looked at.
const MeterFloorDB = -60.0

// MeterLevel maps a dBFS reading onto 0..1 for a bar. The scale is linear in
// decibels rather than in amplitude: a meter that is linear in amplitude
// spends nine tenths of its length on the top 20 dB and tells the player
// nothing about whether their pickup is even connected.
func MeterLevel(db float64) float64 {
	if math.IsInf(db, -1) || db < MeterFloorDB {
		return 0
	}
	if db > 0 {
		return 1
	}
	return (db - MeterFloorDB) / -MeterFloorDB
}

// Bar renders a 0..1 value as a text bar, for the terminal-styled HUD.
func Bar(v float64, width int) string {
	if width <= 0 {
		return ""
	}
	switch {
	case v < 0:
		v = 0
	case v > 1:
		v = 1
	}
	filled := int(math.Round(v * float64(width)))
	return strings.Repeat("#", filled) + strings.Repeat(".", width-filled)
}

// RatingLabel is the word shown when a note resolves.
func RatingLabel(r practice.Rating) string {
	switch r {
	case practice.Perfect:
		return "PERFECT"
	case practice.Good:
		return "GOOD"
	default:
		return "MISS"
	}
}
