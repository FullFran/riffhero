package dsp

import (
	"math"
	"math/bits"
)

// FFT computes a fixed-size radix-2 transform. Size must be a power of two.
//
// Everything the transform needs — twiddle factors, the bit-reversal
// permutation, the analysis window and the working buffers — is built once in
// NewFFT. The chord verifier runs this on every onset, inside the same game
// loop that has to draw a frame in 16 ms, so a transform that allocated per
// call would hand the garbage collector a job it does not need.
//
// An FFT is not safe for concurrent use: Magnitudes writes into buffers the
// value owns. One per analysis chain, not one shared by several.
type FFT struct {
	n int

	// rev[i] is the bit-reversed position of i. Decimation in time needs the
	// input permuted before the butterflies run.
	rev []int

	// Twiddles for the whole transform, indexed by k = 0..n/2-1 as
	// exp(-2*pi*i*k/n). Every stage reads this one table with a stride, which
	// beats a table per stage on cache and costs nothing extra to build.
	cos []float64
	sin []float64

	win []float64 // periodic Hann, applied by Magnitudes

	// winScale converts a windowed bin magnitude back into the amplitude of
	// the sinusoid that produced it, so Magnitudes reads in signal units
	// rather than in units of "however long the window happened to be".
	winScale float64

	re []float64 // scratch for Magnitudes
	im []float64
}

// nextPowerOfTwo rounds n up to a power of two, with a floor of 2.
func nextPowerOfTwo(n int) int {
	if n < 2 {
		return 2
	}
	return 1 << bits.Len(uint(n-1))
}

// NewFFT returns a transform of size n, rounded up to a power of two.
func NewFFT(n int) *FFT {
	n = nextPowerOfTwo(n)

	f := &FFT{
		n:   n,
		rev: make([]int, n),
		cos: make([]float64, n/2),
		sin: make([]float64, n/2),
		win: HannWindow(n),
		re:  make([]float64, n),
		im:  make([]float64, n),
	}

	shift := bits.UintSize - bits.Len(uint(n-1))
	for i := range f.rev {
		f.rev[i] = int(bits.Reverse(uint(i)) >> shift)
	}
	for k := range f.cos {
		angle := -2 * math.Pi * float64(k) / float64(n)
		f.cos[k] = math.Cos(angle)
		f.sin[k] = math.Sin(angle)
	}

	// Coherent gain of the window: a sinusoid of amplitude A lands in its bin
	// with magnitude A*sum(win)/2, so undo exactly that.
	var sum float64
	for _, w := range f.win {
		sum += w
	}
	if sum > 0 {
		f.winScale = 1 / sum
	}
	return f
}

// Size is the transform length, which is what Forward requires and what
// Magnitudes zero-pads a short buffer up to.
func (f *FFT) Size() int { return f.n }

// Forward transforms re/im in place; both must have length Size().
//
// A caller who gets the length wrong gets nothing done rather than a panic:
// this sits under the audio path, and taking the process down over a slice
// header is never the right trade in a practice tool.
func (f *FFT) Forward(re, im []float64) {
	if len(re) != f.n || len(im) != f.n {
		return
	}

	for i, j := range f.rev {
		if i < j {
			re[i], re[j] = re[j], re[i]
			im[i], im[j] = im[j], im[i]
		}
	}

	for size := 2; size <= f.n; size <<= 1 {
		half := size >> 1
		stride := f.n / size
		for start := 0; start < f.n; start += size {
			k := 0
			for j := start; j < start+half; j++ {
				c, s := f.cos[k], f.sin[k]
				k += stride

				tre := re[j+half]*c - im[j+half]*s
				tim := re[j+half]*s + im[j+half]*c

				re[j+half] = re[j] - tre
				im[j+half] = im[j] - tim
				re[j] += tre
				im[j] += tim
			}
		}
	}
}

// Magnitudes fills mag (length Size()/2+1) with the magnitude spectrum of a
// real windowed signal. buf may be shorter than Size(); it is zero-padded.
//
// The result is scaled so a full-length sinusoid of amplitude A reads A in its
// own bin, which makes a magnitude comparable across window sizes and makes a
// threshold on one mean something. Bins 0 and Size()/2 have no mirror image to
// fold in, so they carry half the gain of the rest — neither is musical, and
// no caller here looks at them.
//
// A short buffer sees the leading taps of the full-size window, not a window
// of its own length: the intended caller is padding a block that ran out of
// audio, and re-tapering the padding would only pretend the missing samples
// were quiet rather than absent. Nothing is allocated per call.
func (f *FFT) Magnitudes(buf []float32, mag []float64) {
	half := f.n / 2
	if len(mag) < half+1 {
		return
	}

	n := len(buf)
	if n > f.n {
		n = f.n
	}
	for i := 0; i < n; i++ {
		f.re[i] = float64(buf[i]) * f.win[i]
	}
	for i := n; i < f.n; i++ {
		f.re[i] = 0
	}
	for i := range f.im {
		f.im[i] = 0
	}

	f.Forward(f.re, f.im)

	mag[0] = math.Abs(f.re[0]) * f.winScale
	for k := 1; k < half; k++ {
		mag[k] = math.Hypot(f.re[k], f.im[k]) * 2 * f.winScale
	}
	mag[half] = math.Abs(f.re[half]) * f.winScale
}

// HannWindow returns the window this uses, for callers that need to match it.
//
// It is the periodic form, w[i] = 0.5*(1 - cos(2*pi*i/n)), not the symmetric
// one that divides by n-1. The DFT treats the buffer as one period of an
// infinite signal, and only the periodic window closes on itself under that
// assumption; the symmetric one repeats its endpoint and smears every partial
// slightly wider than the three-bin kernel Hann is chosen for.
func HannWindow(n int) []float64 {
	if n < 1 {
		return nil
	}
	w := make([]float64, n)
	for i := range w {
		w[i] = 0.5 * (1 - math.Cos(2*math.Pi*float64(i)/float64(n)))
	}
	return w
}
