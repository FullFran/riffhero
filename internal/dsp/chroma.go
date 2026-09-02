package dsp

import "math"

const (
	// chromaHarmonics is how far up the series a bin is allowed to be read as
	// somebody else's overtone. Four covers the partials a guitar puts real
	// energy into; past that the sub-fundamental a bin implies is so far down
	// that the vote says more about the harmonic series than about the string.
	chromaHarmonics = 4

	// chromaDecay is the weight of each successive harmonic. 0.6 per step is
	// the usual HPCP figure and it matters more than it looks: too flat and a
	// single loud note spreads a full major chord across the profile, too
	// steep and a string with a weak fundamental never reaches its own class.
	chromaDecay = 0.6

	// The band worth folding. Below this is rumble and DC skirt; above it a
	// guitar's partials are string noise and fret buzz, and the semitone grid
	// is finer than the ear cares about anyway.
	chromaMinHz = 55.0
	chromaMaxHz = 5000.0

	// chromaMinFundamentalHz is the lowest implied fundamental a bin may vote
	// for. A0 is well under any guitar, and stopping there keeps the high
	// harmonics from voting for notes nothing on the instrument can play.
	chromaMinFundamentalHz = 27.5
)

// Chroma maps a magnitude spectrum onto the twelve pitch classes, weighting
// each bin by the harmonic series so a note's overtones reinforce its own
// class instead of voting for others.
//
// The naive version — drop every bin into the class of its own frequency —
// reads one plucked string as a chord: the third harmonic lands a fifth up,
// the fifth harmonic a major third up, and a single low E arrives looking like
// E, B and G#. Here each bin instead votes for every fundamental it could be a
// harmonic *of*, with a weight that decays up the series, so the string's own
// partials all pile onto the string's own class.
//
// This answers "which pitch classes are sounding", which is a question for
// display and for loose harmonic context. It is deliberately not what
// ChordVerifier scores against: folding to twelve classes throws away the
// octave, and the octave is precisely what separates an E2 from the E3 above
// it, while normalising away the bin-level frequency is what would make a
// cents reading impossible. The verifier reads partials; this reads colour.
//
// Every bin's votes are precomputed, because the mapping only depends on the
// sample rate and the transform size. Compute is then a flat walk over a
// table and allocates nothing.
type Chroma struct {
	nBins int

	// class[k*chromaHarmonics+h] is the pitch class bin k supports when read
	// as the (h+1)-th harmonic, or -1 when that reading is out of range.
	class  []int8
	weight []float64
}

// NewChroma returns a profile for a spectrum produced by an FFT of the given
// size at the given sample rate. fftSize is rounded up to a power of two the
// same way NewFFT rounds it, so the two agree about how many bins there are.
func NewChroma(sampleRate, fftSize int) *Chroma {
	size := nextPowerOfTwo(fftSize)
	nBins := size/2 + 1

	c := &Chroma{
		nBins:  nBins,
		class:  make([]int8, nBins*chromaHarmonics),
		weight: make([]float64, nBins*chromaHarmonics),
	}
	for i := range c.class {
		c.class[i] = -1
	}
	if sampleRate <= 0 {
		return c
	}

	binHz := float64(sampleRate) / float64(size)
	for k := 0; k < nBins; k++ {
		hz := float64(k) * binHz
		if hz < chromaMinHz || hz > chromaMaxHz {
			continue
		}
		w := 1.0
		for h := 1; h <= chromaHarmonics; h++ {
			fundamental := hz / float64(h)
			if fundamental < chromaMinFundamentalHz {
				break
			}
			class, dev := pitchClassOf(fundamental)
			// Taper by distance from the semitone centre. A bin sitting
			// halfway between two classes is evidence for neither, and
			// letting it count in full is what makes a plain chroma jitter
			// between neighbouring classes as a string drifts.
			taper := math.Cos(math.Pi * dev)
			idx := k*chromaHarmonics + (h - 1)
			c.class[idx] = int8(class)
			c.weight[idx] = w * taper * taper
			w *= chromaDecay
		}
	}
	return c
}

// Compute fills out (length 12) from a magnitude spectrum. Normalized so the
// largest class is 1, or all zeros for silence.
func (c *Chroma) Compute(mag []float64, out []float64) {
	if len(out) < 12 {
		return
	}
	for i := 0; i < 12; i++ {
		out[i] = 0
	}

	n := len(mag)
	if n > c.nBins {
		n = c.nBins
	}
	for k := 0; k < n; k++ {
		m := mag[k]
		if m <= 0 {
			continue
		}
		base := k * chromaHarmonics
		for h := 0; h < chromaHarmonics; h++ {
			class := c.class[base+h]
			if class < 0 {
				continue
			}
			out[class] += m * c.weight[base+h]
		}
	}

	max := 0.0
	for i := 0; i < 12; i++ {
		if out[i] > max {
			max = out[i]
		}
	}
	if max <= 0 {
		return
	}
	for i := 0; i < 12; i++ {
		out[i] /= max
	}
}

// PitchClass is the chroma index of a MIDI note. Class 0 is C, so A440
// (MIDI 69) is class 9.
func PitchClass(midi uint8) int { return int(midi % 12) }

// pitchClassOf maps a frequency to its nearest pitch class and how far off the
// semitone centre it sits, in semitones within [-0.5, 0.5].
func pitchClassOf(hz float64) (class int, dev float64) {
	if hz <= 0 {
		return 0, 0
	}
	exact := 69 + 12*math.Log2(hz/440)
	nearest := math.Round(exact)
	class = int(math.Mod(nearest, 12))
	if class < 0 {
		class += 12
	}
	return class, exact - nearest
}
