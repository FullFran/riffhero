package dsp

import "math"

const testSampleRate = 48000

// sine renders a pure tone. It is the easiest possible input: if an estimator
// cannot hold this, nothing else it reports is trustworthy.
func sine(hz float64, n int) []float32 {
	out := make([]float32, n)
	for i := range out {
		out[i] = float32(0.7 * math.Sin(2*math.Pi*hz*float64(i)/testSampleRate))
	}
	return out
}

// pluck renders a harmonically rich, decaying tone closer to a real string:
// a full harmonic series with 1/k amplitudes and a weak fundamental, which is
// exactly the shape that makes naive autocorrelation report an octave error.
func pluck(hz float64, n int, harmonics int) []float32 {
	out := make([]float32, n)
	for i := range out {
		t := float64(i) / testSampleRate
		decay := math.Exp(-2.5 * t)
		var v float64
		for k := 1; k <= harmonics; k++ {
			f := hz * float64(k)
			if f >= testSampleRate/2 {
				break
			}
			amp := 1 / float64(k)
			if k == 1 {
				amp = 0.4 // weak fundamental: the classic octave-error trap
			}
			v += amp * math.Sin(2*math.Pi*f*t+float64(k))
		}
		out[i] = float32(0.5 * decay * v)
	}
	return out
}

// noise renders low-level broadband hiss, standing in for an idle input.
func noise(n int, amp float64) []float32 {
	out := make([]float32, n)
	state := uint32(12345)
	for i := range out {
		state = state*1664525 + 1013904223
		u := float64(state>>8)/float64(1<<24)*2 - 1
		out[i] = float32(amp * u)
	}
	return out
}

// centsBetween is the interval between two frequencies, in cents.
func centsBetween(a, b float64) float64 { return 1200 * math.Log2(a/b) }
