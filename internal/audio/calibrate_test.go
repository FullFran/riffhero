package audio

import (
	"math"
	"math/rand"
	"testing"
)

// loopback models what actually comes back: the signal, delayed, quieter,
// spectrally mangled by a speaker and a microphone, with room noise on top.
func loopback(played []float32, delay int, gain float64, noise float64, seed int64) []float32 {
	rng := rand.New(rand.NewSource(seed))
	out := make([]float32, len(played))

	var lp float64
	for i := range out {
		var v float64
		if i-delay >= 0 && i-delay < len(played) {
			v = float64(played[i-delay]) * gain
		}
		// A one-pole low pass stands in for a cheap transducer: it destroys
		// waveform correlation while leaving the attack shape intact, which is
		// exactly the case the envelope correlation has to survive.
		lp += (v - lp) * 0.3
		out[i] = float32(lp + rng.NormFloat64()*noise)
	}
	return out
}

func TestClickTrainHasTheRequestedNumberOfClicks(t *testing.T) {
	train := ClickTrain(48000, 4, 0.25, 0.5)
	if len(train) == 0 {
		t.Fatal("no signal produced")
	}

	// Count bursts, not edges. Each click is a short tone, so it crosses zero
	// several dozen times; what separates one click from the next is the gap.
	gap := 48000 / 100 // 10 ms of quiet ends a burst
	clicks, last := 0, -gap*2
	for i, v := range train {
		if math.Abs(float64(v)) <= 0.02 {
			continue
		}
		if i-last > gap {
			clicks++
		}
		last = i
	}
	if clicks != 4 {
		t.Fatalf("counted %d clicks, want 4", clicks)
	}
	if peak := peakOf(train); peak > 0.51 || peak < 0.4 {
		t.Fatalf("peak %v, want about the requested 0.5", peak)
	}
}

func TestClickTrainRejectsNonsense(t *testing.T) {
	if got := ClickTrain(0, 4, 0.25, 0.5); got != nil {
		t.Fatal("a zero sample rate should produce nothing")
	}
	if got := ClickTrain(48000, 0, 0.25, 0.5); got != nil {
		t.Fatal("zero clicks should produce nothing")
	}
}

func TestMeasureLagFindsAKnownDelay(t *testing.T) {
	const rate = 48000
	played := ClickTrain(rate, 6, 0.25, 0.6)

	for _, delay := range []int{0, 240, 1024, 4800} {
		recorded := loopback(played, delay, 0.4, 0.002, 1)
		got, conf := MeasureLag(played, recorded, rate, rate/4)

		if conf < MinConfidence {
			t.Fatalf("delay %d: confidence %.2f is below the acceptance floor", delay, conf)
		}
		// The correlation runs on a decimated envelope, so it resolves to
		// within one decimation step plus the smoothing.
		if diff := got - delay; diff > 64 || diff < -64 {
			t.Fatalf("delay %d measured as %d (%.2f confidence)", delay, got, conf)
		}
	}
}

func TestMeasureLagSurvivesARoomFullOfNoise(t *testing.T) {
	const rate = 48000
	played := ClickTrain(rate, 8, 0.25, 0.6)
	recorded := loopback(played, 1500, 0.15, 0.02, 7)

	got, conf := MeasureLag(played, recorded, rate, rate/4)
	if conf < MinConfidence {
		t.Fatalf("confidence %.2f under noise, want at least %v", conf, MinConfidence)
	}
	if diff := got - 1500; diff > 128 || diff < -128 {
		t.Fatalf("measured %d, want about 1500", got)
	}
}

func TestMeasureLagReportsNoConfidenceOnSilence(t *testing.T) {
	// Nothing came back: an interface with no loopback, or a muted output. The
	// caller has to be able to tell that apart from a genuine zero latency.
	const rate = 48000
	played := ClickTrain(rate, 4, 0.25, 0.6)
	silence := make([]float32, len(played))

	_, conf := MeasureLag(played, silence, rate, rate/4)
	if conf >= MinConfidence {
		t.Fatalf("silence measured with %.2f confidence", conf)
	}
}

func TestMeasureLagRejectsPureNoise(t *testing.T) {
	const rate = 48000
	played := ClickTrain(rate, 6, 0.25, 0.6)
	rng := rand.New(rand.NewSource(3))
	noise := make([]float32, len(played))
	for i := range noise {
		noise[i] = float32(rng.NormFloat64() * 0.1)
	}

	_, conf := MeasureLag(played, noise, rate, rate/4)
	if conf >= MinConfidence {
		t.Fatalf("noise correlated at %.2f, which would be stored as a latency", conf)
	}
}

func TestMeasureLagRejectsNonsenseArguments(t *testing.T) {
	if lag, conf := MeasureLag(nil, nil, 48000, 1000); lag != 0 || conf != 0 {
		t.Fatalf("empty input gave %d/%v", lag, conf)
	}
	if lag, conf := MeasureLag([]float32{1}, []float32{1}, 0, 1000); lag != 0 || conf != 0 {
		t.Fatalf("zero sample rate gave %d/%v", lag, conf)
	}
	if lag, conf := MeasureLag([]float32{1}, []float32{1}, 48000, 0); lag != 0 || conf != 0 {
		t.Fatalf("zero search range gave %d/%v", lag, conf)
	}
}

func peakOf(buf []float32) float64 {
	var peak float64
	for _, v := range buf {
		if a := math.Abs(float64(v)); a > peak {
			peak = a
		}
	}
	return peak
}
