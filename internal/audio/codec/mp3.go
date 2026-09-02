package codec

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/hajimehoshi/go-mp3"
)

// DecodeMP3 decodes an MP3 stream.
//
// go-mp3 always emits 16-bit little-endian stereo, even for a mono source
// (it upmixes internally), so there is no channel count to read back from
// the decoder — the output is unconditionally 2 channels.
func DecodeMP3(data []byte) (*PCM, error) {
	dec, err := mp3.NewDecoder(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("codec: mp3: %w", err)
	}

	raw, err := io.ReadAll(dec)
	if err != nil {
		return nil, fmt.Errorf("codec: mp3: %w", err)
	}

	n := len(raw) / 2
	samples := make([]float32, n)
	for i := 0; i < n; i++ {
		v := int16(binary.LittleEndian.Uint16(raw[i*2:]))
		samples[i] = float32(v) / 32768
	}
	return &PCM{SampleRate: dec.SampleRate(), Channels: 2, Data: samples}, nil
}
