package dsp

import "math"

// Guitar pitch bounds. The low end sits below the open low E (82.41 Hz) and
// the high end above E6 (1318.51 Hz), with margin for a detuned string.
const (
	MinHz = 70.0
	MaxHz = 1400.0

	// defaultWindow must hold at least two periods of MinHz. At 48 kHz that
	// is 1372 samples, so 2048 leaves comfortable margin.
	defaultWindow = 2048

	// clarityFloor is the minimum periodicity a buffer must show before it is
	// called a note rather than noise.
	clarityFloor = 0.6
)

// Pitch is one pitch estimate over one analysis window.
type Pitch struct {
	Hz      float64
	Clarity float64 // 0..1 periodicity; how much to trust Hz
	Voiced  bool    // whether the window holds a pitched sound at all
	Agreed  bool    // set by Estimator when MPM and YIN concur
}

// lagBounds converts the frequency bounds into the lag range an estimator may
// search inside a buffer of n samples. ok is false when the buffer is too
// short to hold the periods we care about.
func lagBounds(sampleRate, n int) (minLag, maxLag int, ok bool) {
	minLag = int(float64(sampleRate) / MaxHz)
	if minLag < 2 {
		minLag = 2
	}
	maxLag = int(float64(sampleRate)/MinHz) + 1
	if half := n / 2; maxLag > half {
		maxLag = half
	}
	return minLag, maxLag, maxLag > minLag
}

// rms is the level of a buffer, used to reject silence before doing real work.
func rms(buf []float32) float64 {
	if len(buf) == 0 {
		return 0
	}
	var sum float64
	for _, s := range buf {
		v := float64(s)
		sum += v * v
	}
	return math.Sqrt(sum / float64(len(buf)))
}

// parabolic refines an extremum at index i of y by fitting a parabola through
// its neighbours. It returns the interpolated position and value.
func parabolic(y []float64, i int) (pos, val float64) {
	if i <= 0 || i >= len(y)-1 {
		return float64(i), y[i]
	}
	a, b, c := y[i-1], y[i], y[i+1]
	denom := a - 2*b + c
	if denom == 0 {
		return float64(i), b
	}
	delta := 0.5 * (a - c) / denom
	if delta < -1 || delta > 1 {
		return float64(i), b
	}
	return float64(i) + delta, b - 0.25*(a-c)*delta
}

// MPM implements the McLeod Pitch Method.
//
// It scores every lag with the normalized square difference function, then
// picks the *first* key maximum that clears a fraction of the best one rather
// than the tallest peak outright. That rule is the whole point: on a string
// with a weak fundamental the tallest NSDF peak often sits an octave up, and
// preferring the earliest strong peak is what keeps the estimate on the
// fundamental.
type MPM struct {
	sampleRate int
	window     int
	// threshold is the fraction of the highest key maximum a peak must reach
	// to be accepted. Lower values favour the fundamental more aggressively.
	threshold float64

	// Reused across calls: Detect runs on every window of a live stream, so
	// it holds its working buffers instead of allocating them each time.
	nsdf  []float64
	peaks []nsdfPeak
}

// nsdfPeak is the top of one positive hump of the NSDF curve.
type nsdfPeak struct {
	lag   float64
	value float64
}

func NewMPM(sampleRate int) *MPM {
	return &MPM{
		sampleRate: sampleRate,
		window:     defaultWindow,
		threshold:  0.9,
		nsdf:       make([]float64, defaultWindow/2+2),
	}
}

// WindowSize is the buffer length the estimator is tuned for.
func (m *MPM) WindowSize() int { return m.window }

// Detect estimates the pitch of one buffer. Buffers longer than the window are
// truncated; shorter ones are analysed as far as their length allows.
func (m *MPM) Detect(buf []float32) Pitch {
	if len(buf) > m.window {
		buf = buf[:m.window]
	}
	minLag, maxLag, ok := lagBounds(m.sampleRate, len(buf))
	if !ok || rms(buf) < 1e-6 {
		return Pitch{}
	}

	if cap(m.nsdf) < maxLag+1 {
		m.nsdf = make([]float64, maxLag+1)
	}
	nsdf := m.nsdf[:maxLag+1]

	// n(tau) = 2*r(tau) / m(tau), bounded to [-1, 1]. A value near 1 means the
	// buffer repeats itself almost exactly at that lag.
	for tau := 0; tau <= maxLag; tau++ {
		var acf, norm float64
		for i := 0; i+tau < len(buf); i++ {
			a, b := float64(buf[i]), float64(buf[i+tau])
			acf += a * b
			norm += a*a + b*b
		}
		if norm == 0 {
			nsdf[tau] = 0
			continue
		}
		nsdf[tau] = 2 * acf / norm
	}

	peakLag, clarity, found := m.firstKeyMaximum(nsdf, minLag, maxLag)
	if !found || clarity < clarityFloor {
		return Pitch{Clarity: math.Max(clarity, 0)}
	}
	return Pitch{
		Hz:      float64(m.sampleRate) / peakLag,
		Clarity: clarity,
		Voiced:  true,
	}
}

// firstKeyMaximum applies McLeod's peak-picking rule: collect the highest point
// of each positive hump of the NSDF, then take the earliest hump whose peak
// clears threshold times the tallest peak found.
func (m *MPM) firstKeyMaximum(nsdf []float64, minLag, maxLag int) (lag, value float64, ok bool) {
	peaks := m.peaks[:0]

	// Skip the descent from the tau=0 lobe: a hump only starts once the curve
	// has gone negative and comes back up. This has to start at tau=0, not at
	// minLag — for the top of the range minLag lands *inside* the first hump,
	// and skipping from there would swallow the true peak and hand back the
	// hump an octave below it.
	tau := 0
	for tau < maxLag && nsdf[tau] > 0 {
		tau++
	}

	for tau < maxLag {
		for tau < maxLag && nsdf[tau] <= 0 {
			tau++
		}
		if tau >= maxLag {
			break
		}
		best := tau
		for tau < maxLag && nsdf[tau] > 0 {
			if nsdf[tau] > nsdf[best] {
				best = tau
			}
			tau++
		}
		if p, v := parabolic(nsdf, best); p >= float64(minLag) {
			peaks = append(peaks, nsdfPeak{lag: p, value: v})
		}
	}
	m.peaks = peaks
	if len(peaks) == 0 {
		return 0, 0, false
	}

	highest := peaks[0].value
	for _, p := range peaks[1:] {
		if p.value > highest {
			highest = p.value
		}
	}
	cut := m.threshold * highest
	for _, p := range peaks {
		if p.value >= cut && p.lag > 0 {
			return p.lag, p.value, true
		}
	}
	return 0, 0, false
}

// YIN implements the cumulative-mean-normalized difference estimator. It is
// kept as an independent cross-check on MPM: the two methods fail differently,
// so agreement between them is real evidence and disagreement is a warning.
type YIN struct {
	sampleRate int
	window     int
	// threshold is YIN's absolute threshold on the normalized difference; the
	// first dip below it wins, which is what biases YIN toward the fundamental.
	threshold float64

	diff []float64
}

func NewYIN(sampleRate int) *YIN {
	return &YIN{
		sampleRate: sampleRate,
		window:     defaultWindow,
		threshold:  0.15,
		diff:       make([]float64, defaultWindow/2+2),
	}
}

func (y *YIN) WindowSize() int { return y.window }

func (y *YIN) Detect(buf []float32) Pitch {
	if len(buf) > y.window {
		buf = buf[:y.window]
	}
	minLag, maxLag, ok := lagBounds(y.sampleRate, len(buf))
	if !ok || rms(buf) < 1e-6 {
		return Pitch{}
	}

	if cap(y.diff) < maxLag+1 {
		y.diff = make([]float64, maxLag+1)
	}
	d := y.diff[:maxLag+1]

	// Squared difference between the buffer and a copy of itself shifted by tau.
	for tau := 1; tau <= maxLag; tau++ {
		var sum float64
		for i := 0; i+tau < len(buf); i++ {
			delta := float64(buf[i]) - float64(buf[i+tau])
			sum += delta * delta
		}
		d[tau] = sum
	}

	// Cumulative mean normalization: divide each lag by the running mean of
	// the lags before it. This removes the trivial dip at tau=0 and makes a
	// single absolute threshold meaningful across levels.
	d[0] = 1
	var running float64
	for tau := 1; tau <= maxLag; tau++ {
		running += d[tau]
		if running == 0 {
			d[tau] = 1
			continue
		}
		d[tau] *= float64(tau) / running
	}

	best := -1
	for tau := minLag; tau <= maxLag; tau++ {
		if d[tau] >= y.threshold {
			continue
		}
		// Walk to the bottom of this dip before accepting it.
		for tau+1 <= maxLag && d[tau+1] < d[tau] {
			tau++
		}
		best = tau
		break
	}
	if best < 0 {
		// No dip cleared the threshold; fall back to the global minimum so the
		// caller still sees the clarity it scored.
		best = minLag
		for tau := minLag; tau <= maxLag; tau++ {
			if d[tau] < d[best] {
				best = tau
			}
		}
		return Pitch{Clarity: math.Max(0, 1-d[best])}
	}

	pos, val := parabolic(d, best)
	if pos <= 0 {
		return Pitch{}
	}
	clarity := math.Max(0, 1-val)
	if clarity < clarityFloor {
		return Pitch{Clarity: clarity}
	}
	return Pitch{
		Hz:      float64(y.sampleRate) / pos,
		Clarity: clarity,
		Voiced:  true,
	}
}

// Estimator runs MPM as the primary estimate and YIN as a cross-check.
//
// MPM's answer is the one reported, because it holds the fundamental better on
// plucked strings. YIN decides confidence: when the two agree the estimate is
// marked Agreed and keeps its clarity, and when they disagree — nearly always
// an octave taken by one of them — the estimate survives but with its clarity
// cut, so the note tracker needs more evidence before emitting anything.
type Estimator struct {
	mpm *MPM
	yin *YIN

	// agreeCents is how far apart the two methods may land and still count as
	// agreement.
	agreeCents float64
}

func NewEstimator(sampleRate int) *Estimator {
	return &Estimator{
		mpm:        NewMPM(sampleRate),
		yin:        NewYIN(sampleRate),
		agreeCents: 50,
	}
}

func (e *Estimator) WindowSize() int { return e.mpm.WindowSize() }

func (e *Estimator) Detect(buf []float32) Pitch {
	primary := e.mpm.Detect(buf)
	if !primary.Voiced {
		return primary
	}

	cross := e.yin.Detect(buf)
	if !cross.Voiced {
		primary.Clarity *= 0.5
		return primary
	}
	if math.Abs(1200*math.Log2(primary.Hz/cross.Hz)) <= e.agreeCents {
		primary.Agreed = true
		return primary
	}

	primary.Clarity *= 0.5
	return primary
}

// NearestNote maps a frequency onto the nearest equal-tempered semitone,
// returning that note and how far off it the frequency actually is. The cents
// figure is what the practice view shows as intonation feedback.
func NearestNote(hz float64) (midi uint8, cents float64) {
	if hz <= 0 {
		return 0, 0
	}
	exact := 69 + 12*math.Log2(hz/440)
	rounded := math.Round(exact)
	switch {
	case rounded < 0:
		rounded = 0
	case rounded > 127:
		rounded = 127
	}
	return uint8(rounded), (exact - rounded) * 100
}
