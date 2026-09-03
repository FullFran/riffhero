package audio

import (
	"math"
	"testing"
)

// A two-input interface puts the guitar in socket one and leaves socket two
// open. Averaging the pair halves the guitar and mixes in whatever the empty
// socket is picking up, which is why the channel is a setting.
func TestPickTakesOneInputRatherThanTheAverage(t *testing.T) {
	// Two frames, guitar at full scale on the left, hum on the right.
	src := []float32{1, 0.1, -1, 0.1}
	dst := make([]float32, 2)

	pick(dst, src, 2, ChannelLeft)
	if dst[0] != 1 || dst[1] != -1 {
		t.Fatalf("left = %v, want the guitar untouched", dst)
	}

	pick(dst, src, 2, ChannelRight)
	if dst[0] != 0.1 || dst[1] != 0.1 {
		t.Fatalf("right = %v", dst)
	}

	pick(dst, src, 2, ChannelMix)
	if math.Abs(float64(dst[0])-0.55) > 1e-6 {
		t.Fatalf("mix = %v, want the average", dst)
	}
}

func TestPickHandlesEveryChannelCount(t *testing.T) {
	mono := []float32{0.5, -0.5}
	dst := make([]float32, 2)
	for _, which := range []InputChannel{ChannelMix, ChannelLeft, ChannelRight} {
		pick(dst, mono, 1, which)
		if dst[0] != 0.5 || dst[1] != -0.5 {
			t.Fatalf("a single input under %v gave %v", which, dst)
		}
	}

	// Four inputs, one frame: the mix averages all of them, and asking for a
	// named channel still lands on the right sample.
	quad := []float32{1, 2, 3, 4}
	one := make([]float32, 1)
	pick(one, quad, 4, ChannelMix)
	if one[0] != 2.5 {
		t.Fatalf("mix of four = %v", one[0])
	}
	pick(one, quad, 4, ChannelRight)
	if one[0] != 2 {
		t.Fatalf("second of four = %v", one[0])
	}
}

func TestInputChannelCycles(t *testing.T) {
	c := ChannelMix
	seen := map[InputChannel]bool{}
	for i := 0; i < 3; i++ {
		seen[c] = true
		c = c.Next()
	}
	if len(seen) != 3 || c != ChannelMix {
		t.Fatalf("cycled through %v and landed on %v", seen, c)
	}
	if ChannelLeft.String() != "left" || ChannelRight.String() != "right" || ChannelMix.String() != "mix" {
		t.Fatal("the names are wrong")
	}
}

func TestMeterReportsEachInputSeparately(t *testing.T) {
	// The point of the meter: it shows which socket the guitar is actually in,
	// rather than leaving somebody to work it out by playing and watching one
	// number that never moves.
	e := &Engine{}
	src := []float32{0.9, 0.05, -0.7, 0.02}

	e.meter(src, 2, true)
	peaks := e.InputPeaks()
	if math.Abs(peaks[0]-0.9) > 1e-6 {
		t.Fatalf("left peak %v, want 0.9", peaks[0])
	}
	if math.Abs(peaks[1]-0.05) > 1e-6 {
		t.Fatalf("right peak %v, want 0.05", peaks[1])
	}

	// A second chunk of the same callback keeps the loudest of the two rather
	// than replacing it, or a long period would report only its last piece.
	e.meter([]float32{0.1, 0.4}, 2, false)
	peaks = e.InputPeaks()
	if math.Abs(peaks[0]-0.9) > 1e-6 {
		t.Fatalf("left peak fell to %v mid-callback", peaks[0])
	}
	if math.Abs(peaks[1]-0.4) > 1e-6 {
		t.Fatalf("right peak %v, want the louder 0.4", peaks[1])
	}

	// And a fresh callback starts again, or the meter would only ever rise.
	e.meter([]float32{0.01, 0.01}, 2, true)
	if peaks = e.InputPeaks(); peaks[0] > 0.02 {
		t.Fatalf("the meter never came down: %v", peaks)
	}
}

func TestMeterSurvivesMoreInputsThanItReports(t *testing.T) {
	e := &Engine{}
	e.meter([]float32{0.1, 0.2, 0.3, 0.4, 0.5, 0.6}, 6, true)
	peaks := e.InputPeaks()
	if math.Abs(peaks[0]-0.1) > 1e-6 || math.Abs(peaks[1]-0.2) > 1e-6 {
		t.Fatalf("got %v", peaks)
	}
}
