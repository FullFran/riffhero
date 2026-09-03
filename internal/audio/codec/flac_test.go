package codec

import (
	"bytes"
	"testing"

	"github.com/mewkiz/flac"
	"github.com/mewkiz/flac/frame"
	"github.com/mewkiz/flac/meta"
)

// buildFLAC encodes a single verbatim (uncompressed) FLAC frame from raw
// per-channel samples, using the mewkiz/flac encoder itself. This gives the
// test a byte-exact, spec-valid FLAC stream to decode without downloading or
// checking in a fixture file — verbatim prediction is the one subframe type
// simple enough to construct directly, the same approach the upstream
// package's own benchmark uses.
func buildFLAC(t *testing.T, sampleRate, bits int, channelSamples [][]int32) []byte {
	t.Helper()
	channels := len(channelSamples)
	n := len(channelSamples[0])

	info := &meta.StreamInfo{
		BlockSizeMin:  uint16(n),
		BlockSizeMax:  uint16(n),
		SampleRate:    uint32(sampleRate),
		NChannels:     uint8(channels),
		BitsPerSample: uint8(bits),
		NSamples:      uint64(n),
	}

	var buf bytes.Buffer
	enc, err := flac.NewEncoder(&buf, info)
	if err != nil {
		t.Fatalf("flac.NewEncoder: %v", err)
	}

	ch := frame.ChannelsMono
	if channels == 2 {
		ch = frame.ChannelsLR
	}

	f := &frame.Frame{
		Header: frame.Header{
			HasFixedBlockSize: true,
			BlockSize:         uint16(n),
			SampleRate:        uint32(sampleRate),
			Channels:          ch,
			BitsPerSample:     uint8(bits),
		},
		Subframes: make([]*frame.Subframe, channels),
	}
	for c := 0; c < channels; c++ {
		f.Subframes[c] = &frame.Subframe{
			SubHeader: frame.SubHeader{Pred: frame.PredVerbatim},
			Samples:   channelSamples[c],
			NSamples:  n,
		}
	}
	if err := enc.WriteFrame(f); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return buf.Bytes()
}

func TestDecodeFLAC_RoundTripMono(t *testing.T) {
	// FLAC's StreamInfo requires a block size of at least 16 samples, so the
	// fixture needs to be padded out even though only the first few values
	// are actually interesting.
	samples := []int32{-32768, -100, 0, 100, 32767, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
	data := buildFLAC(t, 44100, 16, [][]int32{samples})

	got, err := DecodeFLAC(data)
	if err != nil {
		t.Fatalf("DecodeFLAC: %v", err)
	}
	if got.SampleRate != 44100 || got.Channels != 1 {
		t.Fatalf("got rate=%d channels=%d, want 44100/1", got.SampleRate, got.Channels)
	}
	if len(got.Data) != len(samples) {
		t.Fatalf("got %d samples, want %d", len(got.Data), len(samples))
	}
	for i, s := range samples {
		// Dividing by a power-of-two scale is exact in floating point, so
		// the round trip should match bit for bit, not just approximately.
		want := float32(s) / 32768
		if got.Data[i] != want {
			t.Fatalf("sample %d = %v, want exactly %v", i, got.Data[i], want)
		}
	}
}

func TestDecodeFLAC_RoundTripStereo(t *testing.T) {
	left := []int32{-1000, 0, 1000, 2000, 3000, 4000, 5000, 6000, 7000, 8000, 9000, 10000, 11000, 12000, 13000, 14000}
	right := []int32{2000, 1000, 0, -1000, -2000, -3000, -4000, -5000, -6000, -7000, -8000, -9000, -10000, -11000, -12000, -13000}
	data := buildFLAC(t, 48000, 16, [][]int32{left, right})

	got, err := DecodeFLAC(data)
	if err != nil {
		t.Fatalf("DecodeFLAC: %v", err)
	}
	if got.Channels != 2 {
		t.Fatalf("Channels = %d, want 2", got.Channels)
	}
	for i := range left {
		gotL, gotR := got.Data[i*2], got.Data[i*2+1]
		wantL, wantR := float32(left[i])/32768, float32(right[i])/32768
		if gotL != wantL || gotR != wantR {
			t.Fatalf("frame %d = (%v,%v), want (%v,%v)", i, gotL, gotR, wantL, wantR)
		}
	}
}

func TestDecode_RoutesFLACSignatureToFLACDecoder(t *testing.T) {
	data := buildFLAC(t, 44100, 16, [][]int32{{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}})
	if got := sniff(data); got != formatFLAC {
		t.Fatalf("sniff(flac data) = %v, want formatFLAC", got)
	}
	pcm, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if pcm.SampleRate != 44100 {
		t.Fatalf("SampleRate = %d, want 44100", pcm.SampleRate)
	}
}

func TestDecodeFLAC_GarbageReturnsErrorNotPanic(t *testing.T) {
	inputs := [][]byte{
		nil,
		{},
		[]byte("fLaC"), // signature only, no StreamInfo block
		bytes.Repeat([]byte{0x42}, 64),
	}
	for _, in := range inputs {
		if _, err := DecodeFLAC(in); err == nil {
			t.Fatalf("DecodeFLAC(%v): want an error, got nil", in)
		}
	}
}
