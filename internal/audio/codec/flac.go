package codec

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/mewkiz/flac"
)

// DecodeFLAC decodes a FLAC stream.
//
// It reads frame by frame rather than pulling in a higher-level helper
// because mewkiz/flac already hands back each subframe's samples corrected
// for inter-channel decorrelation (mid/side and the like) inside
// Stream.ParseNext — all that's left to do here is interleave the channels
// and rescale from the stream's native bit depth to float32.
func DecodeFLAC(data []byte) (*PCM, error) {
	stream, err := flac.New(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("codec: flac: %w", err)
	}
	defer stream.Close()

	channels := int(stream.Info.NChannels)
	bits := int(stream.Info.BitsPerSample)
	if channels <= 0 || bits <= 0 || bits > 32 {
		return nil, errors.New("codec: flac: invalid stream info")
	}
	scale := float32(int64(1) << uint(bits-1))

	var samples []float32
	for {
		f, err := stream.ParseNext()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("codec: flac: %w", err)
		}

		n := int(f.BlockSize)
		base := len(samples)
		samples = append(samples, make([]float32, n*channels)...)
		for c, sub := range f.Subframes {
			if c >= channels {
				break
			}
			m := n
			if len(sub.Samples) < m {
				m = len(sub.Samples)
			}
			for i := 0; i < m; i++ {
				samples[base+i*channels+c] = float32(sub.Samples[i]) / scale
			}
		}
	}

	return &PCM{SampleRate: int(stream.Info.SampleRate), Channels: channels, Data: samples}, nil
}
