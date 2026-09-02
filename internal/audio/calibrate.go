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
		o.Clicks = 8
	}
	if o.Spacing <= 0 {
		o.Spacing = 0.35
	}
	if o.Volume <= 0 {
		o.Volume = 0.5
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
}

func (c Calibration) String() string {
	return fmt.Sprintf("%.1f ms (%d frames) at %.0f%% confidence", c.Millis, c.Frames, c.Confidence*100)
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
// The correlation is Pearson's, not a plain dot product, and that matters more
// than it looks. A room has a noise floor, so the returning envelope is the
// clicks sitting on top of a constant. A dot product sees that constant as
// signal and scores every lag about equally well; subtracting the mean first
// leaves only the shape, and the peak stands out of the noise instead of
// drowning in it. Confidence is that peak, and the caller is expected to
// refuse a low one rather than store a guess.
func MeasureLag(played, recorded []float32, sampleRate, maxLag int) (lag int, confidence float64) {
	if len(played) == 0 || len(recorded) == 0 || sampleRate <= 0 || maxLag <= 0 {
		return 0, 0
	}

	const decim = 16
	ref := envelope(played, decim)
	got := envelope(recorded, decim)
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

	callback := func(outBytes, inBytes []byte, frames uint32) {
		n := int(frames)
		if n <= 0 {
			return
		}
		if len(inBytes) >= n*4 {
			in := asFloat32(inBytes, n)
			at := int(inPos.Load())
			c := copy(recorded[min(at, len(recorded)):], in)
			inPos.Store(int64(at + c))
		}
		if len(outBytes) >= n*8 {
			out := asFloat32(outBytes, n*2)
			at := int(outPos.Load())
			for i := 0; i < n; i++ {
				var v float32
				if at+i < len(played) {
					v = played[at+i]
				}
				out[i*2], out[i*2+1] = v, v
			}
			outPos.Store(int64(at + n))
		}
	}

	cfg := malgo.DefaultDeviceConfig(malgo.Duplex)
	cfg.SampleRate = uint32(opts.SampleRate)
	cfg.PeriodSizeInFrames = uint32(opts.PeriodFrames)
	cfg.PerformanceProfile = malgo.LowLatency
	cfg.Capture.Format = malgo.FormatF32
	cfg.Capture.Channels = 1
	cfg.Playback.Format = malgo.FormatF32
	cfg.Playback.Channels = 2

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
		return Calibration{}, fmt.Errorf("only captured %d of %d frames; is the input device recording?", got, len(recorded))
	}

	maxLag := int(opts.MaxLatencyMillis / 1000 * float64(opts.SampleRate))
	lag, confidence := MeasureLag(played, recorded[:got], opts.SampleRate, maxLag)

	result := Calibration{
		Frames:     practice.Frame(lag),
		Millis:     float64(lag) / float64(opts.SampleRate) * 1000,
		Confidence: confidence,
		SampleRate: opts.SampleRate,
	}
	if confidence < MinConfidence {
		return result, fmt.Errorf("could not hear the calibration clicks (confidence %.0f%%); check the input is picking up the output", confidence*100)
	}
	return result, nil
}
