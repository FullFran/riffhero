// Package codec decodes backing tracks into the audio engine's working
// format — interleaved float32 PCM — and does the rate/channel conversion
// needed to bring a track onto whatever the output device wants.
package codec

import "math"

// PCM is decoded audio in the engine's working format: interleaved float32
// samples in [-1,1].
type PCM struct {
	SampleRate int
	Channels   int
	Data       []float32
}

// Frames returns the number of interleaved sample frames held.
func (p *PCM) Frames() int {
	if p == nil || p.Channels <= 0 {
		return 0
	}
	return len(p.Data) / p.Channels
}

// Duration in seconds.
func (p *PCM) Duration() float64 {
	if p == nil || p.SampleRate <= 0 {
		return 0
	}
	return float64(p.Frames()) / float64(p.SampleRate)
}

// Peak is the largest absolute sample, for a level meter or a sanity check.
func (p *PCM) Peak() float32 {
	if p == nil {
		return 0
	}
	var peak float32
	for _, s := range p.Data {
		a := s
		if a < 0 {
			a = -a
		}
		if a > peak {
			peak = a
		}
	}
	return peak
}

// Mono returns a single-channel copy, averaging the channels.
func (p *PCM) Mono() []float32 {
	if p == nil || p.Channels <= 0 {
		return nil
	}
	frames := p.Frames()
	out := make([]float32, frames)
	if p.Channels == 1 {
		copy(out, p.Data[:frames])
		return out
	}
	ch := p.Channels
	for i := 0; i < frames; i++ {
		base := i * ch
		var sum float32
		for c := 0; c < ch; c++ {
			sum += p.Data[base+c]
		}
		out[i] = sum / float32(ch)
	}
	return out
}

// clone makes an independent copy, which is what every PCM-returning method
// hands back so the caller can never mutate a track shared with anything
// else (the player, other loop sections, ...) through the result.
func (p *PCM) clone() *PCM {
	if p == nil {
		return nil
	}
	data := make([]float32, len(p.Data))
	copy(data, p.Data)
	return &PCM{SampleRate: p.SampleRate, Channels: p.Channels, Data: data}
}

// Resample returns a copy at the given sample rate.
//
// It uses linear interpolation between neighbouring input frames: cheap, and
// exact at the frame it starts from, which is what keeps a resampled backing
// track from drifting against the beat grid at the loop boundary. It is not
// a band-limited resampler, so heavy downsampling can alias — for a
// pre-rendered practice track played back at a fixed engine rate, that
// trade-off is fine.
func (p *PCM) Resample(rate int) *PCM {
	if p == nil {
		return nil
	}
	if rate <= 0 || rate == p.SampleRate || p.Channels <= 0 {
		return p.clone()
	}

	inFrames := p.Frames()
	ch := p.Channels
	if inFrames == 0 {
		return &PCM{SampleRate: rate, Channels: ch}
	}
	if inFrames == 1 {
		out := &PCM{SampleRate: rate, Channels: ch, Data: make([]float32, ch)}
		copy(out.Data, p.Data[:ch])
		return out
	}

	outFrames := int(math.Round(float64(inFrames) * float64(rate) / float64(p.SampleRate)))
	if outFrames < 1 {
		outFrames = 1
	}
	out := make([]float32, outFrames*ch)

	if outFrames == 1 {
		copy(out, p.Data[:ch])
		return &PCM{SampleRate: rate, Channels: ch, Data: out}
	}

	// step maps an output frame index onto a position in the input, in
	// input-frame units; step*(outFrames-1) lands exactly on the last input
	// frame (up to float rounding), which the clamp below then makes exact.
	step := float64(inFrames-1) / float64(outFrames-1)
	last := float64(inFrames - 1)
	for i := 0; i < outFrames; i++ {
		pos := float64(i) * step
		if pos > last {
			pos = last // guards the last output frame against float overshoot
		}
		i0 := int(pos)
		if i0 > inFrames-2 {
			i0 = inFrames - 2 // keeps i0+1 in bounds on the final frame
		}
		frac := float32(pos - float64(i0))
		a := i0 * ch
		b := a + ch
		for c := 0; c < ch; c++ {
			va, vb := p.Data[a+c], p.Data[b+c]
			out[i*ch+c] = va + (vb-va)*frac
		}
	}
	return &PCM{SampleRate: rate, Channels: ch, Data: out}
}

// Remix returns a copy with the given channel count (mono->stereo
// duplicates, stereo->mono averages).
func (p *PCM) Remix(channels int) *PCM {
	if p == nil {
		return nil
	}
	if channels < 1 {
		channels = 1
	}
	if channels == p.Channels {
		return p.clone()
	}

	frames := p.Frames()
	out := &PCM{SampleRate: p.SampleRate, Channels: channels, Data: make([]float32, frames*channels)}
	if frames == 0 {
		return out
	}

	switch {
	case p.Channels <= 1:
		// Mono (or malformed zero-channel, treated as silence) source:
		// duplicate the single channel into every output channel.
		mono := p.Mono()
		for i := 0; i < frames; i++ {
			var v float32
			if i < len(mono) {
				v = mono[i]
			}
			base := i * channels
			for c := 0; c < channels; c++ {
				out.Data[base+c] = v
			}
		}
	case channels == 1:
		copy(out.Data, p.Mono())
	default:
		// An uncommon channel-count change (e.g. stereo to 5.1): there is no
		// single correct spatial mapping, and the engine only ever asks for
		// mono or stereo in practice, so fold to mono and duplicate.
		mono := p.Mono()
		for i, v := range mono {
			base := i * channels
			for c := 0; c < channels; c++ {
				out.Data[base+c] = v
			}
		}
	}
	return out
}

// Conform returns a copy at the given rate and channel count.
func (p *PCM) Conform(rate, channels int) *PCM {
	if p == nil {
		return nil
	}
	return p.Resample(rate).Remix(channels)
}
