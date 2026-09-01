package practice

import "math"

type Rating uint8

const (
	Miss Rating = iota
	Good
	Perfect
)

type TimingWindows struct {
	Perfect Frame
	Good    Frame
}

type Match struct {
	Rating      Rating
	TimingError Frame
	PitchOK     bool
}

func MatchSingle(expected Note, got DetectedNote, windows TimingWindows, maxCents float64) Match {
	delta := got.Onset - expected.Start
	absFrames := Frame(math.Abs(float64(delta)))
	pitchOK := got.MIDI == expected.MIDI && math.Abs(got.CentsError) <= maxCents

	m := Match{Rating: Miss, TimingError: delta, PitchOK: pitchOK}
	if !pitchOK {
		return m
	}
	if absFrames <= windows.Perfect {
		m.Rating = Perfect
	} else if absFrames <= windows.Good {
		m.Rating = Good
	}
	return m
}
