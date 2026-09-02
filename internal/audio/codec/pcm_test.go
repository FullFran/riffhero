package codec

import (
	"math"
	"testing"
)

// ramp renders a linear ramp from 0 to 1 across n frames, on one channel.
// It is the right shape for testing resampling: monotonicity and endpoint
// values are trivial to check by eye against the source.
func ramp(n int) []float32 {
	out := make([]float32, n)
	for i := range out {
		out[i] = float32(i) / float32(n-1)
	}
	return out
}

func TestPCM_Frames(t *testing.T) {
	p := &PCM{SampleRate: 48000, Channels: 2, Data: make([]float32, 20)}
	if got := p.Frames(); got != 10 {
		t.Fatalf("Frames() = %d, want 10", got)
	}
	if got := (&PCM{Channels: 0, Data: make([]float32, 4)}).Frames(); got != 0 {
		t.Fatalf("Frames() with zero channels = %d, want 0", got)
	}
	var nilPCM *PCM
	if got := nilPCM.Frames(); got != 0 {
		t.Fatalf("Frames() on nil = %d, want 0", got)
	}
}

func TestPCM_Duration(t *testing.T) {
	p := &PCM{SampleRate: 1000, Channels: 1, Data: make([]float32, 500)}
	if got := p.Duration(); got != 0.5 {
		t.Fatalf("Duration() = %v, want 0.5", got)
	}
}

func TestPCM_Peak(t *testing.T) {
	p := &PCM{Channels: 1, Data: []float32{0.1, -0.9, 0.4, -0.2}}
	if got := p.Peak(); got != 0.9 {
		t.Fatalf("Peak() = %v, want 0.9", got)
	}
	var nilPCM *PCM
	if got := nilPCM.Peak(); got != 0 {
		t.Fatalf("Peak() on nil = %v, want 0", got)
	}
}

func TestPCM_MonoAverages(t *testing.T) {
	// L=1, R=-1 should average to silence; L=0.4, R=0.2 should average to 0.3.
	p := &PCM{Channels: 2, Data: []float32{1, -1, 0.4, 0.2}}
	got := p.Mono()
	want := []float32{0, 0.3}
	for i := range want {
		if !closeEnough(got[i], want[i], 1e-6) {
			t.Fatalf("Mono()[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestPCM_MonoAlreadyMonoCopies(t *testing.T) {
	p := &PCM{Channels: 1, Data: []float32{0.1, 0.2, 0.3}}
	got := p.Mono()
	got[0] = 99
	if p.Data[0] == 99 {
		t.Fatal("Mono() must return an independent copy, not alias the receiver's data")
	}
}

func TestPCM_ResampleSameRateIsNoopCopy(t *testing.T) {
	p := &PCM{SampleRate: 48000, Channels: 1, Data: []float32{0.1, 0.2, 0.3}}
	got := p.Resample(48000)
	if got == p {
		t.Fatal("Resample at the same rate must return a copy, not the receiver itself")
	}
	for i, v := range p.Data {
		if got.Data[i] != v {
			t.Fatalf("Resample at same rate changed sample %d: got %v, want %v", i, got.Data[i], v)
		}
	}
	got.Data[0] = 99
	if p.Data[0] == 99 {
		t.Fatal("Resample must not let the copy alias the receiver's data")
	}
}

func TestPCM_ResampleDownsamples(t *testing.T) {
	const in, out = 48000, 24000
	src := &PCM{SampleRate: in, Channels: 1, Data: ramp(in)}
	got := src.Resample(out)

	if got.SampleRate != out {
		t.Fatalf("SampleRate = %d, want %d", got.SampleRate, out)
	}
	if len(got.Data) == 0 {
		t.Fatal("Resample produced no samples")
	}
	if got.Data[0] != src.Data[0] {
		t.Fatalf("first sample = %v, want exactly %v (the input's first sample)", got.Data[0], src.Data[0])
	}
	last := got.Data[len(got.Data)-1]
	if !closeEnough(last, src.Data[len(src.Data)-1], 1e-4) {
		t.Fatalf("last sample = %v, want close to %v", last, src.Data[len(src.Data)-1])
	}
	assertMonotonic(t, got.Data)
}

func TestPCM_ResampleUpsamples(t *testing.T) {
	const in, out = 24000, 48000
	src := &PCM{SampleRate: in, Channels: 1, Data: ramp(in)}
	got := src.Resample(out)

	if got.SampleRate != out {
		t.Fatalf("SampleRate = %d, want %d", got.SampleRate, out)
	}
	if got.Data[0] != src.Data[0] {
		t.Fatalf("first sample = %v, want exactly %v", got.Data[0], src.Data[0])
	}
	last := got.Data[len(got.Data)-1]
	if !closeEnough(last, src.Data[len(src.Data)-1], 1e-4) {
		t.Fatalf("last sample = %v, want close to %v", last, src.Data[len(src.Data)-1])
	}
	assertMonotonic(t, got.Data)
}

func TestPCM_ResampleStereoKeepsChannelsInterleaved(t *testing.T) {
	// Left ramps up, right ramps down; if channels got crossed or
	// de-interleaved this would show up immediately.
	n := 100
	data := make([]float32, n*2)
	for i := 0; i < n; i++ {
		data[i*2] = float32(i) / float32(n-1)
		data[i*2+1] = 1 - float32(i)/float32(n-1)
	}
	src := &PCM{SampleRate: 1000, Channels: 2, Data: data}
	got := src.Resample(500)

	if got.Channels != 2 {
		t.Fatalf("Channels = %d, want 2", got.Channels)
	}
	if got.Data[0] != 0 || got.Data[1] != 1 {
		t.Fatalf("first frame = (%v,%v), want (0,1)", got.Data[0], got.Data[1])
	}
	last := len(got.Data) - 2
	if !closeEnough(got.Data[last], 1, 1e-4) || !closeEnough(got.Data[last+1], 0, 1e-4) {
		t.Fatalf("last frame = (%v,%v), want close to (1,0)", got.Data[last], got.Data[last+1])
	}
}

func TestPCM_ResampleHandlesNilAndEmptyWithoutPanicking(t *testing.T) {
	var nilPCM *PCM
	if got := nilPCM.Resample(44100); got != nil {
		t.Fatalf("Resample on nil receiver = %v, want nil", got)
	}

	empty := &PCM{SampleRate: 48000, Channels: 2}
	got := empty.Resample(44100)
	if got == nil || len(got.Data) != 0 {
		t.Fatalf("Resample on empty PCM = %+v, want a non-nil PCM with no samples", got)
	}
}

func TestPCM_RemixMonoToStereoToMono(t *testing.T) {
	mono := &PCM{SampleRate: 48000, Channels: 1, Data: []float32{0.2, -0.5, 0.9}}

	stereo := mono.Remix(2)
	if stereo.Channels != 2 {
		t.Fatalf("Channels = %d, want 2", stereo.Channels)
	}
	for i, v := range mono.Data {
		l, r := stereo.Data[i*2], stereo.Data[i*2+1]
		if l != v || r != v {
			t.Fatalf("frame %d = (%v,%v), want both channels equal to %v", i, l, r, v)
		}
	}

	back := stereo.Remix(1)
	if back.Channels != 1 {
		t.Fatalf("Channels = %d, want 1", back.Channels)
	}
	for i, v := range mono.Data {
		if !closeEnough(back.Data[i], v, 1e-6) {
			t.Fatalf("round-tripped sample %d = %v, want %v", i, back.Data[i], v)
		}
	}
}

func TestPCM_RemixSameChannelsCopies(t *testing.T) {
	p := &PCM{Channels: 2, Data: []float32{0.1, 0.2}}
	got := p.Remix(2)
	got.Data[0] = 99
	if p.Data[0] == 99 {
		t.Fatal("Remix at the same channel count must return a copy, not alias the receiver")
	}
}

func TestPCM_RemixHandlesNilAndEmptyWithoutPanicking(t *testing.T) {
	var nilPCM *PCM
	if got := nilPCM.Remix(2); got != nil {
		t.Fatalf("Remix on nil receiver = %v, want nil", got)
	}

	empty := &PCM{SampleRate: 48000, Channels: 1}
	got := empty.Remix(2)
	if got == nil || len(got.Data) != 0 {
		t.Fatalf("Remix on empty PCM = %+v, want a non-nil PCM with no samples", got)
	}
}

func TestPCM_Conform(t *testing.T) {
	mono := &PCM{SampleRate: 48000, Channels: 1, Data: ramp(480)}
	got := mono.Conform(24000, 2)
	if got.SampleRate != 24000 || got.Channels != 2 {
		t.Fatalf("Conform() = rate %d channels %d, want 24000/2", got.SampleRate, got.Channels)
	}
	if got.Data[0] != mono.Data[0] {
		t.Fatalf("first sample = %v, want exactly %v", got.Data[0], mono.Data[0])
	}
}

func TestPCM_ConformNilReceiverDoesNotPanic(t *testing.T) {
	var nilPCM *PCM
	if got := nilPCM.Conform(44100, 2); got != nil {
		t.Fatalf("Conform on nil receiver = %v, want nil", got)
	}
}

func closeEnough(a, b, tol float32) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= tol
}

func assertMonotonic(t *testing.T, data []float32) {
	t.Helper()
	for i := 1; i < len(data); i++ {
		if data[i] < data[i-1] {
			t.Fatalf("sample %d (%v) is less than sample %d (%v): not monotonic", i, data[i], i-1, data[i-1])
		}
		if math.IsNaN(float64(data[i])) {
			t.Fatalf("sample %d is NaN", i)
		}
	}
}
