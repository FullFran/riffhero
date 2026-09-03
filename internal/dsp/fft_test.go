package dsp

import (
	"math"
	"math/rand"
	"testing"
)

// directDFT is the definition of the transform: O(n^2), obviously correct, and
// far too slow to ship. It is the only thing worth checking a fast
// implementation against, because a wrong twiddle table or a wrong
// bit-reversal still produces a plausible-looking spectrum.
func directDFT(re, im []float64) (outRe, outIm []float64) {
	n := len(re)
	outRe = make([]float64, n)
	outIm = make([]float64, n)
	for k := 0; k < n; k++ {
		var sr, si float64
		for t := 0; t < n; t++ {
			angle := -2 * math.Pi * float64(k) * float64(t) / float64(n)
			c, s := math.Cos(angle), math.Sin(angle)
			sr += re[t]*c - im[t]*s
			si += re[t]*s + im[t]*c
		}
		outRe[k], outIm[k] = sr, si
	}
	return outRe, outIm
}

// peakBin is the index of the largest magnitude in a spectrum.
func peakBin(mag []float64) int {
	best := 0
	for k, m := range mag {
		if m > mag[best] {
			best = k
		}
	}
	return best
}

func TestFFTMatchesDirectDFT(t *testing.T) {
	const n = 128
	f := NewFFT(n)

	rng := rand.New(rand.NewSource(7))
	re := make([]float64, n)
	im := make([]float64, n)
	for i := range re {
		re[i] = rng.Float64()*2 - 1
		im[i] = rng.Float64()*2 - 1
	}
	wantRe, wantIm := directDFT(re, im)

	f.Forward(re, im)
	for k := range re {
		if math.Abs(re[k]-wantRe[k]) > 1e-9 || math.Abs(im[k]-wantIm[k]) > 1e-9 {
			t.Fatalf("bin %d = (%.12f, %.12f), want (%.12f, %.12f)", k, re[k], im[k], wantRe[k], wantIm[k])
		}
	}
}

func TestFFTRoundsSizeUpToPowerOfTwo(t *testing.T) {
	for _, tc := range []struct{ in, want int }{
		{-1, 2}, {0, 2}, {1, 2}, {2, 2}, {3, 4}, {1000, 1024}, {1024, 1024}, {8000, 8192},
	} {
		if got := NewFFT(tc.in).Size(); got != tc.want {
			t.Errorf("NewFFT(%d).Size() = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestFFTMagnitudesPutsASineInItsOwnBin(t *testing.T) {
	f := NewFFT(8192)
	mag := make([]float64, f.Size()/2+1)
	f.Magnitudes(sine(440, f.Size()), mag)

	want := int(math.Round(440 * float64(f.Size()) / testSampleRate))
	if got := peakBin(mag); got != want {
		t.Errorf("peak at bin %d (%.2f Hz), want bin %d", got, float64(got)*testSampleRate/float64(f.Size()), want)
	}
	// sine renders at amplitude 0.7, and the scaling exists so that reads back
	// as 0.7 rather than as some multiple of the window length.
	if got := mag[want]; math.Abs(got-0.7) > 0.05 {
		t.Errorf("peak magnitude = %.4f, want ~0.7 (the amplitude of the tone)", got)
	}
}

func TestFFTMagnitudesZeroPadsAShortBuffer(t *testing.T) {
	f := NewFFT(8192)
	mag := make([]float64, f.Size()/2+1)
	f.Magnitudes(sine(440, f.Size()/2), mag)

	want := int(math.Round(440 * float64(f.Size()) / testSampleRate))
	if got := peakBin(mag); math.Abs(float64(got-want)) > 2 {
		t.Errorf("peak at bin %d, want within 2 bins of %d", got, want)
	}
}

// Magnitudes runs once per onset inside the frame budget, so an allocation
// here is an allocation in the game loop.
func TestFFTMagnitudesAllocatesNothing(t *testing.T) {
	f := NewFFT(8192)
	mag := make([]float64, f.Size()/2+1)
	buf := sine(196, f.Size())
	f.Magnitudes(buf, mag) // warm up

	if got := testing.AllocsPerRun(20, func() { f.Magnitudes(buf, mag) }); got != 0 {
		t.Errorf("Magnitudes allocated %.1f times per call, want 0", got)
	}
}

// Wrong lengths must not take the process down: this sits under the audio path.
func TestFFTToleratesMismatchedBuffers(t *testing.T) {
	f := NewFFT(64)

	short := make([]float64, 8)
	short[0] = 1
	f.Forward(short, make([]float64, 8))
	if short[0] != 1 {
		t.Error("Forward touched a buffer of the wrong length")
	}

	mag := make([]float64, 4)
	mag[0] = -1
	f.Magnitudes(sine(220, 64), mag)
	if mag[0] != -1 {
		t.Error("Magnitudes wrote into an output slice too short to hold a spectrum")
	}
}

func TestHannWindowIsPeriodicAndSymmetric(t *testing.T) {
	const n = 1024
	w := HannWindow(n)
	if len(w) != n {
		t.Fatalf("len = %d, want %d", len(w), n)
	}
	if w[0] != 0 {
		t.Errorf("w[0] = %v, want 0", w[0])
	}
	if math.Abs(w[n/2]-1) > 1e-12 {
		t.Errorf("w[n/2] = %v, want 1", w[n/2])
	}
	for i := 1; i < n; i++ {
		if math.Abs(w[i]-w[n-i]) > 1e-12 {
			t.Fatalf("w[%d] = %v but w[%d] = %v; the window is not symmetric", i, w[i], n-i, w[n-i])
		}
	}
	// A periodic Hann sums to exactly n/2. The symmetric form does not, which
	// is the cheapest way to tell the two apart.
	var sum float64
	for _, v := range w {
		sum += v
	}
	if math.Abs(sum-float64(n)/2) > 1e-9 {
		t.Errorf("sum = %.9f, want %d (the periodic form)", sum, n/2)
	}

	if HannWindow(0) != nil {
		t.Error("HannWindow(0) must return nil, not an empty non-nil slice")
	}
}

func BenchmarkFFTMagnitudes(b *testing.B) {
	f := NewFFT(8192)
	mag := make([]float64, f.Size()/2+1)
	buf := sine(196, f.Size())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.Magnitudes(buf, mag)
	}
}
