package audio

import (
	"fmt"
	"math"
	"sync/atomic"
	"time"

	"github.com/gen2brain/malgo"

	"github.com/FullFran/riffhero/internal/practice"
)

// Round-trip latency is the one number in this project that cannot be derived,
// only measured. It is the sum of the output buffer, the converter, the air or
// the cable, the input buffer and whatever the driver stack adds on top, and
// every one of those varies by machine. Guessing it wrong does not degrade
// scoring gracefully: it shifts every detection by a constant, so a player who
// is dead on time is told they are consistently late.
//
// The measurement is a click train played out and listened for on the way back.
// It works over a cable, and it works over a laptop's own speaker and
// microphone, which is what most people will actually have.

// CalibrationOptions tunes the measurement.
type CalibrationOptions struct {
	SampleRate   int
	PeriodFrames int
	Input        *Device
	Output       *Device

	// Clicks is how many bursts to play, Spacing how far apart in seconds.
	// More clicks means a correlation peak that stands further out of the
	// noise, at the cost of the player waiting.
	Clicks  int
	Spacing float64

	// Volume is how loud to play them, 0..1. Loud enough to hear over a room,
	// quiet enough not to clip a preamp.
	Volume float64

	// MaxLatencyMillis bounds the search. Anything past it is not latency, it
	// is a second click being mistaken for the first.
	MaxLatencyMillis float64
}

func (o CalibrationOptions) withDefaults() CalibrationOptions {
	if o.SampleRate <= 0 {
		o.SampleRate = 48000
	}
	if o.PeriodFrames <= 0 {
		o.PeriodFrames = DefaultPeriodFrames
	}
	if o.Clicks <= 0 {
		o.Clicks = 12
	}
	if o.Spacing <= 0 {
		o.Spacing = 0.35
	}
	if o.Volume <= 0 {
		o.Volume = 0.45
	}
	if o.MaxLatencyMillis <= 0 {
		o.MaxLatencyMillis = 250
	}
	return o
}

// Calibration is the measurement.
type Calibration struct {
	Frames     practice.Frame
	Millis     float64
	Confidence float64 // 0..1 peak correlation
	SampleRate int

	// Peak is the loudest sample that came back, in dBFS. It is the difference
	// between "the clicks were not heard" and "nothing was heard at all", and
	// those two have completely different fixes.
	PeakDB float64
}

func (c Calibration) String() string {
	return fmt.Sprintf("%.1f ms (%d frames) at %.0f%% confidence, peak %.0f dBFS",
		c.Millis, c.Frames, c.Confidence*100, c.PeakDB)
}

// CalibrationFailure is why a measurement was refused.
//
// The reason is a value rather than only a sentence because the screen that
// shows it has to say it in the player's language, and picking a translation
// apart from an English error string is not something anybody should be
// writing. The sentence stays here for the terminal and for the log.
type CalibrationFailure uint8

const (
	// HeardNothing: the input was silent. Wrong device, or muted.
	HeardNothing CalibrationFailure = iota + 1
	// ClicksNotFound: something came back, but not the clicks. Too quiet, too
	// far away, or too noisy a room.
	ClicksNotFound
	// NotRecording: the device opened and then delivered almost no frames.
	NotRecording
)

// CalibrationError is a refused measurement, with the numbers that say which
// of the three things went wrong.
type CalibrationError struct {
	Reason           CalibrationFailure
	PeakDB           float64
	Confidence       float64
	Captured, Wanted int
}

// The three sentences, as format strings, exported so the screen that shows
// them in another language keys on the same English this package prints. Two
// copies of the same sentence is how a screen quietly reverts to English: the
// copy gets edited, the translation still matches the other one, and no test
// can tell.
const (
	HeardNothingText   = "the input heard almost nothing (peak %.0f dBFS): check it is the right device and that it is not muted"
	NotRecordingText   = "only captured %d of %d frames; is the input device recording?"
	ClicksNotFoundText = "could not pick the clicks out of what came back (confidence %.0f%%, peak %.0f dBFS): turn the output up, move the microphone closer, or quieten the room"
)

func (e *CalibrationError) Error() string {
	switch e.Reason {
	case HeardNothing:
		return fmt.Sprintf(HeardNothingText, e.PeakDB)
	case NotRecording:
		return fmt.Sprintf(NotRecordingText, e.Captured, e.Wanted)
	default:
		return fmt.Sprintf(ClicksNotFoundText, e.Confidence*100, e.PeakDB)
	}
}

// MinConfidence is the correlation below which a measurement is refused. A bad
// measurement is worse than none: it would be stored and silently applied to
// every note for the rest of the session.
const MinConfidence = 0.35

// ClickTrain builds the calibration signal: short bursts with a sharp attack
// and enough high-frequency content to survive a cheap speaker.
//
// A raw impulse would be ideal for correlation and useless in practice — one
// sample of full scale is inaudible over a laptop speaker and is exactly what
// a driver's own filtering smears out. A few milliseconds of tone with a hard
// start correlates almost as well and actually gets through.
func ClickTrain(sampleRate, clicks int, spacing, volume float64) []float32 {
	if sampleRate <= 0 || clicks <= 0 {
		return nil
	}
	step := int(spacing * float64(sampleRate))
	burst := int(0.004 * float64(sampleRate))
	if step < burst*2 {
		step = burst * 2
	}

	out := make([]float32, step*clicks+step)
	for c := 0; c < clicks; c++ {
		start := step/2 + c*step
		for i := 0; i < burst && start+i < len(out); i++ {
			// A cosine decay rather than a rectangular window: the attack is
			// what the correlation locks on to, and the tail only has to fade
			// without ringing.
			env := 1 - float64(i)/float64(burst)
			v := math.Sin(2*math.Pi*2000*float64(i)/float64(sampleRate)) * env * env * volume
			out[start+i] = float32(v)
		}
	}
	return out
}

// MeasureLag cross-correlates the envelope of what was played against the
// envelope of what came back and returns the lag in frames.
//
// Correlating the envelopes rather than the waveforms is deliberate. A speaker
// and a microphone invert phase, colour the spectrum and shift it in ways that
// destroy waveform correlation while leaving the shape of an attack perfectly
// intact.
//
// What is correlated is the *rise* in each envelope, not the envelope itself.
// A room smears a four-millisecond click into a couple of hundred milliseconds
// of decay, so the returning envelope looks nothing like the one that was sent
// and the two correlate at about a third — enough to be refused as noise. The
// attack survives the room intact, and the half-wave-rectified difference is
// exactly the attack with the tail thrown away.
//
// The correlation is Pearson's, not a plain dot product, and that matters too.
// A room has a noise floor, so what comes back sits on top of a constant. A
// dot product counts that constant as signal and scores every lag about
// equally well; subtracting the mean leaves only the shape. Confidence is the
// peak, and the caller is expected to refuse a low one rather than store a
// guess.
func MeasureLag(played, recorded []float32, sampleRate, maxLag int) (lag int, confidence float64) {
	if len(played) == 0 || len(recorded) == 0 || sampleRate <= 0 || maxLag <= 0 {
		return 0, 0
	}

	const decim = 128
	ref := flux(envelope(played, decim))
	got := flux(envelope(recorded, decim))
	if len(ref) < 8 || len(got) < 8 {
		return 0, 0
	}

	steps := maxLag / decim
	if steps >= len(got) {
		steps = len(got) - 1
	}
	if steps <= 0 {
		return 0, 0
	}

	bestLag, best := 0, -1.0
	for s := 0; s <= steps; s++ {
		n := len(ref)
		if avail := len(got) - s; avail < n {
			n = avail
		}
		if n < 8 {
			break
		}
		if score, ok := pearson(ref[:n], got[s:s+n]); ok && score > best {
			best, bestLag = score, s
		}
	}
	if best < 0 {
		return 0, 0
	}
	return bestLag * decim, best
}

// pearson is the correlation of two equal-length sequences, or ok=false when
// either of them is flat and has no shape to correlate.
func pearson(a, b []float32) (float64, bool) {
	n := float64(len(a))
	var sa, sb float64
	for i := range a {
		sa += float64(a[i])
		sb += float64(b[i])
	}
	ma, mb := sa/n, sb/n

	var dot, va, vb float64
	for i := range a {
		x, y := float64(a[i])-ma, float64(b[i])-mb
		dot += x * y
		va += x * x
		vb += y * y
	}
	if va <= 0 || vb <= 0 {
		return 0, false
	}
	return dot / math.Sqrt(va*vb), true
}

// flux is the half-wave-rectified first difference of an envelope: how much
// louder each block is than the one before it, and zero where it is quieter.
// It is what is left of a click after a room has finished with it.
func flux(env []float32) []float32 {
	if len(env) == 0 {
		return nil
	}
	out := make([]float32, len(env))
	for i := 1; i < len(env); i++ {
		if d := env[i] - env[i-1]; d > 0 {
			out[i] = d
		}
	}
	return out
}

// envelope reduces a signal to the energy of each block of decim samples.
//
// Block RMS rather than a peak-following detector: a peak follower locks onto
// whatever is loudest in a block, which in a quiet room is the noise, and the
// envelope then tracks the noise instead of the clicks. Averaging over the
// block is what makes the measurement survive a laptop microphone.
func envelope(buf []float32, decim int) []float32 {
	if decim < 1 {
		decim = 1
	}
	out := make([]float32, 0, len(buf)/decim+1)
	for i := 0; i < len(buf); i += decim {
		end := i + decim
		if end > len(buf) {
			end = len(buf)
		}
		var sum float64
		for _, v := range buf[i:end] {
			sum += float64(v) * float64(v)
		}
		out = append(out, float32(math.Sqrt(sum/float64(end-i))))
	}
	return out
}

// Calibrate plays a click train and listens for it, returning the round trip.
//
// It opens its own duplex stream rather than borrowing the engine's, because
// the engine's clock is the song's clock and this measurement has no business
// moving it.
func Calibrate(host *Host, opts CalibrationOptions) (Calibration, error) {
	if host == nil || host.ctx == nil {
		return Calibration{}, fmt.Errorf("audio host is closed")
	}
	opts = opts.withDefaults()

	played := ClickTrain(opts.SampleRate, opts.Clicks, opts.Spacing, opts.Volume)
	if len(played) == 0 {
		return Calibration{}, fmt.Errorf("could not build a calibration signal")
	}
	recorded := make([]float32, len(played))

	var outPos, inPos atomic.Int64
	mono := make([]float32, chunkFrames)

	// The same channel-count rules as the engine: whatever the device has,
	// folded down here rather than demanded of the backend.
	callback := func(outBytes, inBytes []byte, frames uint32) {
		n := int(frames)
		if n <= 0 {
			return
		}
		inCh := channelsOf(inBytes, n)
		outCh := channelsOf(outBytes, n)

		for off := 0; off < n; {
			size := n - off
			if size > chunkFrames {
				size = chunkFrames
			}

			if inCh > 0 {
				in := asFloat32(inBytes, n*inCh)
				// The measurement averages every input: the clicks come back on
				// whichever socket is connected, and which one that is is
				// exactly what is not known yet.
				pick(mono[:size], in[off*inCh:(off+size)*inCh], inCh, ChannelMix)
				if at := int(inPos.Load()); at < len(recorded) {
					inPos.Store(int64(at + copy(recorded[at:], mono[:size])))
				}
			}
			if outCh > 0 {
				out := asFloat32(outBytes, n*outCh)
				at := int(outPos.Load())
				for i := 0; i < size; i++ {
					var v float32
					if at+i < len(played) {
						v = played[at+i]
					}
					base := (off + i) * outCh
					for c := 0; c < outCh; c++ {
						out[base+c] = v
					}
				}
				outPos.Store(int64(at + size))
			}
			off += size
		}
	}

	cfg := deviceConfigFor(malgo.Duplex, opts.SampleRate, opts.PeriodFrames)
	pinner := pinDevices(&cfg, opts.Input, opts.Output)
	device, err := malgo.InitDevice(host.ctx.Context, cfg, malgo.DeviceCallbacks{Data: callback})
	pinner()
	if err != nil {
		return Calibration{}, fmt.Errorf("opening the calibration device: %w", err)
	}
	defer device.Uninit()

	if err := device.Start(); err != nil {
		return Calibration{}, fmt.Errorf("starting the calibration device: %w", err)
	}

	deadline := time.Now().Add(time.Duration(float64(len(played))/float64(opts.SampleRate)*1000)*time.Millisecond + 2*time.Second)
	for time.Now().Before(deadline) {
		if inPos.Load() >= int64(len(recorded)) && outPos.Load() >= int64(len(played)) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = device.Stop()

	got := int(inPos.Load())
	if got < len(recorded)/2 {
		return Calibration{}, &CalibrationError{Reason: NotRecording, Captured: got, Wanted: len(recorded)}
	}

	maxLag := int(opts.MaxLatencyMillis / 1000 * float64(opts.SampleRate))
	lag, confidence := MeasureLag(played, recorded[:got], opts.SampleRate, maxLag)

	result := Calibration{
		Frames:     practice.Frame(lag),
		Millis:     float64(lag) / float64(opts.SampleRate) * 1000,
		Confidence: confidence,
		SampleRate: opts.SampleRate,
		PeakDB:     peakDB(recorded[:got]),
	}
	if confidence < MinConfidence {
		// Two very different failures, and the fix for each is different, so
		// they get different sentences.
		reason := ClicksNotFound
		if result.PeakDB < -50 {
			reason = HeardNothing
		}
		return result, &CalibrationError{Reason: reason, PeakDB: result.PeakDB, Confidence: confidence}
	}
	return result, nil
}

func peakDB(buf []float32) float64 {
	var peak float64
	for _, v := range buf {
		if a := math.Abs(float64(v)); a > peak {
			peak = a
		}
	}
	if peak <= 0 {
		return math.Inf(-1)
	}
	return 20 * math.Log10(peak)
}
