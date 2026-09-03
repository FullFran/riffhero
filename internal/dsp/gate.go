package dsp

import "math"

// Gate reports whether the input carries a signal worth analysing.
//
// It is the cheapest stage in the chain and it runs first, so an idle input
// costs almost nothing: with no string ringing there is no reason to spend a
// window of autocorrelation on hiss. Two thresholds rather than one give it
// hysteresis, which keeps a decaying note from chattering the gate open and
// shut as it fades across a single boundary.
type Gate struct {
	// OpenDB is the level the input must exceed to open the gate.
	OpenDB float64
	// CloseDB is the level the input must fall below to close it again. It
	// sits below OpenDB; the span between them is the hysteresis.
	CloseDB float64

	open  bool
	level float64 // linear RMS of the last block
}

func NewGate() *Gate {
	return &Gate{OpenDB: -45, CloseDB: -55}
}

// Update feeds one block and returns whether the gate is open afterwards.
//
// Measuring a single short block is only meaningful when that block spans
// several cycles of the lowest note in play. Callers working hop by hop should
// use UpdateLevel with a smoothed envelope instead; see Onset.Level.
func (g *Gate) Update(block []float32) bool {
	return g.UpdateLevel(rms(block))
}

// UpdateLevel feeds an already-measured linear RMS level.
func (g *Gate) UpdateLevel(level float64) bool {
	g.level = level
	db := g.LevelDB()

	switch {
	case !g.open && db > g.OpenDB:
		g.open = true
	case g.open && db < g.CloseDB:
		g.open = false
	}
	return g.open
}

// Open reports the current state without feeding new samples.
func (g *Gate) Open() bool { return g.open }

// LevelDB is the level of the last block in dBFS.
func (g *Gate) LevelDB() float64 {
	if g.level <= 0 {
		return math.Inf(-1)
	}
	return 20 * math.Log10(g.level)
}

// Reset returns the gate to its initial closed state.
func (g *Gate) Reset() {
	g.open = false
	g.level = 0
}
