package dsp

import (
	"math"
	"testing"
)

// level renders a constant-amplitude tone at a given dBFS RMS.
func levelAt(dbfs float64, n int) []float32 {
	target := math.Pow(10, dbfs/20)
	amp := target * math.Sqrt2 // a sine's RMS is its amplitude / sqrt(2)
	out := make([]float32, n)
	for i := range out {
		out[i] = float32(amp * math.Sin(2*math.Pi*220*float64(i)/testSampleRate))
	}
	return out
}

func TestGateStartsClosed(t *testing.T) {
	if NewGate().Open() {
		t.Error("a fresh gate is open, want closed")
	}
}

func TestGateOpensOnLoudSignal(t *testing.T) {
	g := NewGate()
	if !g.Update(levelAt(-20, 512)) {
		t.Errorf("gate stayed closed at -20 dBFS (level %.1f dBFS)", g.LevelDB())
	}
}

func TestGateStaysClosedOnSilence(t *testing.T) {
	g := NewGate()
	if g.Update(make([]float32, 512)) {
		t.Error("gate opened on silence")
	}
}

func TestGateStaysClosedOnQuietNoise(t *testing.T) {
	g := NewGate()
	if g.Update(noise(512, 0.0005)) {
		t.Errorf("gate opened on hiss (level %.1f dBFS)", g.LevelDB())
	}
}

// Hysteresis is what stops a decaying string from chattering the gate open and
// shut around a single threshold.
func TestGateHysteresisHoldsOpenBetweenThresholds(t *testing.T) {
	g := NewGate()
	g.Update(levelAt(-20, 512))
	if !g.Open() {
		t.Fatal("gate did not open at -20 dBFS")
	}

	between := (g.OpenDB + g.CloseDB) / 2
	if !g.Update(levelAt(between, 512)) {
		t.Errorf("gate closed at %.1f dBFS, between its own thresholds", between)
	}
}

func TestGateClosesBelowCloseThreshold(t *testing.T) {
	g := NewGate()
	g.Update(levelAt(-20, 512))
	if !g.Open() {
		t.Fatal("gate did not open at -20 dBFS")
	}
	if g.Update(levelAt(g.CloseDB-10, 512)) {
		t.Error("gate stayed open well below its close threshold")
	}
}

func TestGateLevelTracksSignal(t *testing.T) {
	g := NewGate()
	g.Update(levelAt(-30, 4096))
	if got := g.LevelDB(); math.Abs(got+30) > 3 {
		t.Errorf("LevelDB = %.1f, want about -30", got)
	}
}

func TestGateLevelOfSilenceIsVeryNegative(t *testing.T) {
	g := NewGate()
	g.Update(make([]float32, 512))
	if got := g.LevelDB(); got > -100 {
		t.Errorf("LevelDB on silence = %.1f, want a very negative value", got)
	}
}
