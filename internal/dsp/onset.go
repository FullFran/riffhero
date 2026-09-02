package dsp

import "math"

// Onset reports where a new note starts.
//
// It works on the energy envelope rather than on spectral flux: a plucked
// string is an energy event, and an FFT per hop would buy accuracy the
// practice loop cannot use while costing far more than the whole rest of the
// chain.
//
// The rule is a rise over the *quietest* of the last few hops, not over their
// average. That distinction is the difference between working and not: on a
// guitar the previous note is usually still ringing when the next one is
// struck, so the new energy adds to the old rather than replacing it. An
// average is dragged up by that ringing and swallows the attack; the recent
// minimum is not.
type Onset struct {
	hop int

	// RiseRatio is how far above the recent minimum a hop must jump to count
	// as an attack. 2.0 is +6 dB.
	RiseRatio float64
	// FloorDB is the absolute level below which nothing is an attack, however
	// large the relative jump. It keeps hiss out.
	FloorDB float64
	// Refractory is the shortest gap between two onsets. Anything closer is
	// the same note still ringing.
	Refractory int
	// Lookback is how many hops back the recent minimum is taken over.
	Lookback int
	// EnvelopeHops is how many hops the energy of a single point on the
	// envelope is measured over. One hop alone is not enough: at 48 kHz a
	// 256-sample hop holds barely half a cycle of the low E, so its RMS
	// swings with the phase of the waveform rather than with its loudness,
	// and a rule that compares against a recent minimum would read that swing
	// as an attack several times per note.
	EnvelopeHops int

	hopSums   []float64 // sum of squares per hop, for the sliding envelope
	energies  []float64 // recent envelope values
	level     float64   // envelope of the most recent hop
	position  int64
	lastOnset int64
}

// NewOnset returns a detector tuned for guitar at the given sample rate.
func NewOnset(sampleRate int) *Onset {
	return &Onset{
		hop:          256,
		RiseRatio:    2.0,
		FloorDB:      -50,
		Refractory:   int(0.05 * float64(sampleRate)),
		Lookback:     8, // ~43 ms at 48 kHz
		EnvelopeHops: 4, // ~21 ms at 48 kHz, over two periods of the low E
		lastOnset:    math.MinInt64 / 4,
	}
}

// Hop is the block size the detector expects.
func (o *Onset) Hop() int { return o.hop }

// Position is the absolute sample position the detector has consumed up to.
func (o *Onset) Position() int64 { return o.position }

// Level is the smoothed envelope of the most recent hop, measured over several
// hops. It is the only level in the chain worth judging a low note by, so
// anything else that needs one should take it from here rather than measuring
// a single hop for itself.
func (o *Onset) Level() float64 { return o.level }

// Skip advances the detector past samples that were never delivered, so a gap
// in the input does not shift every later position.
func (o *Onset) Skip(n int64) {
	if n <= 0 {
		return
	}
	o.position += n
	// The audio either side of a gap is unrelated, so the lookback window is
	// history about a signal that no longer exists.
	o.hopSums = o.hopSums[:0]
	o.energies = o.energies[:0]
	o.level = 0
}

// Push feeds one hop and reports the absolute sample position of an onset when
// this hop starts one.
func (o *Onset) Push(block []float32) (at int64, fired bool) {
	start := o.position
	o.position += int64(len(block))

	level := o.envelope(block)
	o.level = level
	defer o.remember(level)

	floor := math.Pow(10, o.FloorDB/20)
	if level < floor {
		return 0, false
	}
	// The refractory window runs from where the rise was *confirmed*, not from
	// the back-dated attack, so back-dating can never shorten it.
	if start-o.lastOnset < int64(o.Refractory) {
		return 0, false
	}

	quietest := o.quietest()
	// An empty or silent history means the note started out of nothing, which
	// is an onset by definition.
	if quietest > 0 && level < o.RiseRatio*quietest {
		return 0, false
	}

	o.lastOnset = start
	return o.attackStart(start, quietest, floor), true
}

// envelope adds this hop to the sliding energy window and returns the RMS over
// the whole window.
func (o *Onset) envelope(block []float32) float64 {
	var sum float64
	for _, s := range block {
		v := float64(s)
		sum += v * v
	}

	o.hopSums = append(o.hopSums, sum)
	if len(o.hopSums) > o.EnvelopeHops {
		o.hopSums = append(o.hopSums[:0], o.hopSums[len(o.hopSums)-o.EnvelopeHops:]...)
	}

	var total float64
	for _, s := range o.hopSums {
		total += s
	}
	n := len(o.hopSums) * len(block)
	if n == 0 {
		return 0
	}
	return math.Sqrt(total / float64(n))
}

// quietest is the lowest energy over the lookback window.
func (o *Onset) quietest() float64 {
	if len(o.energies) == 0 {
		return 0
	}
	min := o.energies[0]
	for _, e := range o.energies[1:] {
		if e < min {
			min = e
		}
	}
	return min
}

// attackStart back-dates an onset to where the rise actually began.
//
// A rise is confirmed a hop or two after the string was struck, and reporting
// the confirming hop would bias every note late. Walking back to the last hop
// that was still quiet recovers the real attack, which matters because this
// position is what the scorer compares against the beat.
func (o *Onset) attackStart(confirmedAt int64, quietest, floor float64) int64 {
	mid := math.Max(quietest*math.Sqrt(o.RiseRatio), floor)

	at := confirmedAt
	for k := len(o.energies) - 1; k >= 0; k-- {
		if o.energies[k] <= mid {
			break
		}
		at = confirmedAt - int64(len(o.energies)-k)*int64(o.hop)
	}
	if at < 0 {
		at = 0
	}
	return at
}

// remember appends this hop's energy to the lookback window.
func (o *Onset) remember(level float64) {
	o.energies = append(o.energies, level)
	if len(o.energies) > o.Lookback {
		o.energies = append(o.energies[:0], o.energies[len(o.energies)-o.Lookback:]...)
	}
}

// Reset clears the detector's position and history so a run can be replayed.
func (o *Onset) Reset() {
	o.hopSums = o.hopSums[:0]
	o.energies = o.energies[:0]
	o.level = 0
	o.position = 0
	o.lastOnset = math.MinInt64 / 4
}
