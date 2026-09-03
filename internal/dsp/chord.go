package dsp

import "math"

const (
	// chordBinWidthHz is the frequency resolution the verifier is sized for,
	// and it is the number that decides the window length.
	//
	// A semitone at the open low E is under 5 Hz. Nothing short of a
	// multi-second window resolves that, so the fundamentals of the bottom
	// strings are simply not separable here and the verifier does not pretend
	// otherwise — it leans on the harmonics, where a semitone is worth many
	// bins. 6 Hz puts the whole register above the open A within about two
	// bins per semitone, which is enough for the harmonic sum to mean
	// something, and costs a 171 ms window at 48 kHz. That is a long time in
	// a game loop and a short time in a strummed chord.
	chordBinWidthHz = 6.0

	// chordEvidenceHarmonics is how far up the series a pitch is looked for.
	// Six reaches the fifth above two octaves up, which on a wound string is
	// still louder than the fundamental.
	chordEvidenceHarmonics = 6

	// chordMaskHarmonics is how far up the series counts as *explained* energy
	// for the unexpected-energy penalty. It reaches higher than the evidence
	// set on purpose: a string's tenth partial is far too weak to prove the
	// string is sounding, but it is unquestionably the string's own energy,
	// and charging it to the player as "unexpected" would penalise every
	// correctly played chord.
	chordMaskHarmonics = 10

	// chordToleranceCents is the half-width of a partial's search band. Half a
	// semitone is the natural choice: it is the widest band that can never
	// reach into the neighbouring semitone, so two different expected pitches
	// can only ever collide when their partials genuinely coincide.
	chordToleranceCents = 50.0

	// chordMaskMinHalfBins widens the mask bands to cover the Hann mainlobe,
	// which is two bins either side of a partial. Without it the skirts of
	// every correctly played partial would be counted as unexplained energy.
	chordMaskMinHalfBins = 2

	// chordFloorGain scales the spectral background in the presence test
	// below. Four means a partial has to stand four times clear of the
	// average bin before it counts as more likely there than not.
	chordFloorGain = 4.0

	// The band the verifier looks at. Below is rumble and DC skirt, above is
	// pick noise and fret buzz that no expected pitch should be judged on.
	chordMinAnalysisHz = 50.0
	chordMaxAnalysisHz = 6000.0

	// chordSilenceRMS is the level below which there is nothing to judge. It
	// is the same floor the pitch estimators use, so both halves of the DSP
	// agree about what silence is.
	chordSilenceRMS = 1e-6
)

// ChordEvidence is what the spectrum says about one expected pitch.
type ChordEvidence struct {
	MIDI    uint8
	Present bool
	Score   float64 // 0..1 strength of the evidence for this pitch
	Cents   float64 // measured deviation of the strongest supporting partial
}

// ChordResult is the verdict on a whole expected event.
type ChordResult struct {
	Notes      []ChordEvidence
	Found      int     // how many expected pitches were present
	Unexpected float64 // 0..1 share of the energy no expected pitch explains
	Score      float64 // 0..1 overall, penalized by Unexpected
	Voiced     bool    // whether there was a signal to judge at all
}

// ChordVerifier answers "is this set of pitches sounding?" for a window of audio.
//
// It deliberately does not ask what is being played. The score already says
// which pitches are expected, so the hypothesis space is one set of pitches
// wide and the job is to test it — which is a solvable problem, where
// unconstrained polyphonic transcription is not.
//
// Three rules carry the whole thing:
//
//   - The harmonic sum decides, not the fundamental. On a wound low E most of
//     the energy is in the partials and the fundamental is a rumour; a
//     verifier that looked only at the fundamental would fail on exactly the
//     power chords this exists to check. Each pitch is scored on a weighted
//     mean over its first partials, so a missing fundamental costs it the
//     weight of one harmonic rather than the verdict.
//
//   - A partial belongs to the lowest expected pitch that predicts it. Every
//     harmonic of a pitch an octave up is also a harmonic of the pitch below,
//     so spectral evidence alone can never separate the two. Rather than let
//     both claim the same peak, the lower pitch takes it and the upper one is
//     judged on what is left. What that buys is the case that matters: with
//     only {E2} expected and an E3 sounding, E2 finds its even harmonics and
//     nothing at all on the odd ones, and scores far too low to be called
//     present. What it costs is stated below.
//
//   - Energy that no expected pitch explains is subtracted from the verdict.
//     Getting the right notes while a wrong string rings is not the right
//     chord, and without this term it would score the same.
//
// The known limitation of the second rule: when the expected set itself
// contains an octave pair — an open E chord has E2, E3 and E4 in it — the
// upper pitches are judged on the partials their lower partner does not reach,
// and those partials are present whether or not the upper string was actually
// struck. An octave doubling cannot be confirmed independently from a
// magnitude spectrum. For practice that is the right failure: the chord is
// still rejected unless every non-doubled pitch is there.
//
// A verifier is not safe for concurrent use, and the Notes slice of a result
// aliases scratch that the next Verify overwrites. Copy it if it has to live
// longer than the call.
type ChordVerifier struct {
	// MinScore is the per-note evidence a pitch needs to count as present.
	MinScore float64
	// UnexpectedPenalty scales how much unexplained energy hurts the overall
	// score. At 1 a window where half the energy is unaccounted for loses half
	// its score.
	UnexpectedPenalty float64

	sampleRate int
	window     int
	binHz      float64
	tolRatio   float64 // frequency ratio of chordToleranceCents

	loBin, hiBin int // the analysis range, in bins

	fft *FFT
	mag []float64

	// Scratch, all sized in the constructor. Verify allocates only when the
	// expected set grows past what these already hold.
	notes     []ChordEvidence
	order     []int  // indices into notes, by ascending pitch
	claimed   []bool // bins already taken by a lower expected pitch
	explained []bool // bins any expected pitch accounts for
}

// NewChordVerifier returns a verifier tuned for guitar at the given sample rate.
func NewChordVerifier(sampleRate int) *ChordVerifier {
	if sampleRate <= 0 {
		sampleRate = 48000
	}
	fft := NewFFT(int(float64(sampleRate) / chordBinWidthHz))
	size := fft.Size()
	nBins := size/2 + 1
	binHz := float64(sampleRate) / float64(size)

	loBin := int(math.Ceil(chordMinAnalysisHz / binHz))
	if loBin < 1 {
		loBin = 1
	}
	hiBin := int(math.Floor(chordMaxAnalysisHz / binHz))
	// Leave a neighbour on each side: every peak in the range gets parabolic
	// interpolation, and that needs the bin either side of it to exist.
	if hiBin > nBins-2 {
		hiBin = nBins - 2
	}

	return &ChordVerifier{
		MinScore:          0.5,
		UnexpectedPenalty: 1.0,

		sampleRate: sampleRate,
		window:     size,
		binHz:      binHz,
		tolRatio:   math.Exp2(chordToleranceCents / 1200),

		loBin: loBin,
		hiBin: hiBin,

		fft: fft,
		mag: make([]float64, nBins),

		notes:     make([]ChordEvidence, 0, 6),
		order:     make([]int, 0, 6),
		claimed:   make([]bool, nBins),
		explained: make([]bool, nBins),
	}
}

// WindowSize is the buffer length the verifier is tuned for. Shorter buffers
// are zero-padded, which costs resolution rather than correctness.
func (v *ChordVerifier) WindowSize() int { return v.window }

// Verify judges buf against the expected pitches. Allocation-free per call for
// a stable expected-set size.
func (v *ChordVerifier) Verify(buf []float32, expected []uint8) ChordResult {
	if cap(v.notes) < len(expected) {
		v.notes = make([]ChordEvidence, 0, len(expected))
		v.order = make([]int, 0, len(expected))
	}
	v.notes = v.notes[:0]
	for _, midi := range expected {
		v.notes = append(v.notes, ChordEvidence{MIDI: midi})
	}
	res := ChordResult{Notes: v.notes}

	if rms(buf) < chordSilenceRMS {
		return res
	}
	v.fft.Magnitudes(buf, v.mag)

	// Total energy, and the average bin level that the presence test measures
	// each partial against.
	var total, sum float64
	for k := v.loBin; k <= v.hiBin; k++ {
		m := v.mag[k]
		total += m * m
		sum += m
	}
	if total <= 0 {
		return res
	}
	res.Voiced = true
	floor := sum / float64(v.hiBin-v.loBin+1)

	v.sortByPitch()

	for i := range v.claimed {
		v.claimed[i] = false
	}
	var sumScore float64
	for _, i := range v.order {
		n := &v.notes[i]
		score, cents, ok := v.judge(n.MIDI, floor, true)
		if !ok {
			// Every partial was already taken, which only happens when the
			// same pitch appears twice in the expected set — the same note
			// fingered on two strings. It is one prediction, not two, so it
			// gets the same answer rather than none.
			score, cents, _ = v.judge(n.MIDI, floor, false)
		}
		n.Score = score
		n.Cents = cents
		n.Present = score >= v.MinScore
		if n.Present {
			res.Found++
		}
		sumScore += score
	}

	res.Unexpected = v.unexpected(total)
	if len(v.notes) > 0 {
		res.Score = clampUnit(sumScore / float64(len(v.notes)) * (1 - v.UnexpectedPenalty*res.Unexpected))
	}
	res.Notes = v.notes
	return res
}

// sortByPitch fills order with note indices, lowest pitch first.
//
// Insertion sort rather than sort.Slice: a chord is at most six notes, and
// sort.Slice's closure and interface conversion are the only allocations that
// would be left in this function.
func (v *ChordVerifier) sortByPitch() {
	v.order = v.order[:0]
	for i := range v.notes {
		v.order = append(v.order, i)
		for j := len(v.order) - 1; j > 0 && v.notes[v.order[j]].MIDI < v.notes[v.order[j-1]].MIDI; j-- {
			v.order[j], v.order[j-1] = v.order[j-1], v.order[j]
		}
	}
}

// judge scores one expected pitch as a weighted mean of the evidence for its
// partials.
//
// With respectClaims set, a partial already taken by a lower expected pitch is
// dropped from *both* halves of the mean rather than scored as missing. A
// pitch cannot be blamed for evidence it was never allowed to collect; the
// price of sharing a partial is losing the vote, not failing it.
func (v *ChordVerifier) judge(midi uint8, floor float64, respectClaims bool) (score, cents float64, ok bool) {
	f0 := midiHz(midi)

	var num, den, strongest float64
	for h := 1; h <= chordEvidenceHarmonics; h++ {
		hz := f0 * float64(h)
		lo, hi, inRange := v.band(hz, 0)
		if !inRange {
			continue
		}
		if respectClaims {
			if v.anyClaimed(lo, hi) {
				continue
			}
			v.claim(lo, hi)
		}

		// 1/h: the partials a string actually puts energy into, in the order
		// it puts it there. The fundamental still carries the most weight, but
		// not enough of it to decide alone — which is the point.
		w := 1 / float64(h)
		evidence, off := v.partial(lo, hi, hz, floor)
		num += w * evidence
		den += w
		if contribution := w * evidence; contribution > strongest {
			strongest, cents = contribution, off
		}
	}
	if den == 0 {
		return 0, 0, false
	}
	return num / den, cents, true
}

// partial measures how strongly one predicted partial is present, and where it
// actually landed.
//
// Presence is a contrast against the spectral background, not an absolute
// level: peak/(peak + gain*floor) saturates towards 1 for anything that stands
// clear of the noise and falls towards 0 for anything that does not. That is
// what makes a weak sixth harmonic count as evidence at all — an absolute
// threshold tuned to hear it would also hear hiss, and one tuned to reject
// hiss would never hear it.
func (v *ChordVerifier) partial(lo, hi int, hz, floor float64) (evidence, cents float64) {
	peak := lo
	for k := lo + 1; k <= hi; k++ {
		if v.mag[k] > v.mag[peak] {
			peak = k
		}
	}

	// Parabolic interpolation over the peak's neighbours recovers the real
	// frequency to a fraction of a bin, which is the only reason Cents is
	// worth reporting: at 6 Hz per bin, the nearest bin alone would be out by
	// up to 60 cents at the bottom of the range.
	pos, val := parabolic(v.mag, peak)
	if val < v.mag[peak] {
		val = v.mag[peak]
	}

	if denom := val + chordFloorGain*floor; denom > 0 {
		evidence = val / denom
	}
	if pos > 0 && hz > 0 {
		cents = 1200 * math.Log2(pos*v.binHz/hz)
	}
	return evidence, cents
}

// unexpected is the share of the window's energy that sits outside every
// expected pitch's harmonic mask.
func (v *ChordVerifier) unexpected(total float64) float64 {
	for i := range v.explained {
		v.explained[i] = false
	}
	for i := range v.notes {
		f0 := midiHz(v.notes[i].MIDI)
		for h := 1; h <= chordMaskHarmonics; h++ {
			lo, hi, ok := v.band(f0*float64(h), chordMaskMinHalfBins)
			if !ok {
				continue
			}
			for k := lo; k <= hi; k++ {
				v.explained[k] = true
			}
		}
	}

	var explained float64
	for k := v.loBin; k <= v.hiBin; k++ {
		if v.explained[k] {
			m := v.mag[k]
			explained += m * m
		}
	}
	return clampUnit(1 - explained/total)
}

// band is the bin range a partial at hz may occupy, clipped to the analysis
// range. ok is false when the partial falls outside it entirely.
//
// The band is the half-semitone around hz and, at low frequencies, that is
// narrower than a single bin — in which case it collapses to the nearest bin
// rather than widening. Widening would be worse than useless down there: one
// bin either side of the open A spans a minor third, so a padded band around
// A2 would happily accept the B2 next to it.
func (v *ChordVerifier) band(hz float64, minHalfBins int) (lo, hi int, ok bool) {
	if hz <= 0 {
		return 0, 0, false
	}
	lo = int(math.Ceil(hz / v.tolRatio / v.binHz))
	hi = int(math.Floor(hz * v.tolRatio / v.binHz))
	if hi < lo {
		centre := int(math.Round(hz / v.binHz))
		lo, hi = centre, centre
	}
	if minHalfBins > 0 {
		centre := int(math.Round(hz / v.binHz))
		if lo > centre-minHalfBins {
			lo = centre - minHalfBins
		}
		if hi < centre+minHalfBins {
			hi = centre + minHalfBins
		}
	}
	if lo < v.loBin {
		lo = v.loBin
	}
	if hi > v.hiBin {
		hi = v.hiBin
	}
	return lo, hi, lo <= hi
}

func (v *ChordVerifier) anyClaimed(lo, hi int) bool {
	for k := lo; k <= hi; k++ {
		if v.claimed[k] {
			return true
		}
	}
	return false
}

func (v *ChordVerifier) claim(lo, hi int) {
	for k := lo; k <= hi; k++ {
		v.claimed[k] = true
	}
}

// midiHz is the equal-tempered frequency of a MIDI note, the inverse of what
// NearestNote does.
func midiHz(midi uint8) float64 { return 440 * math.Exp2((float64(midi)-69)/12) }

// clampUnit holds a value inside 0..1. NaN falls through both comparisons, so
// it is caught explicitly rather than escaping into a score.
func clampUnit(v float64) float64 {
	switch {
	case math.IsNaN(v):
		return 0
	case v < 0:
		return 0
	case v > 1:
		return 1
	}
	return v
}
