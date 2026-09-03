package codec

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

// WAV format tags, as stored in the fmt chunk's wFormatTag field.
const (
	wavFormatPCM        = 0x0001
	wavFormatIEEEFloat  = 0x0003
	wavFormatExtensible = 0xFFFE
)

// DecodeWAV parses a RIFF/WAVE stream chunk by chunk rather than assuming a
// fixed layout: real files interleave LIST, fact, and other metadata chunks
// between fmt and data, and some encoders write data last with an
// unreliable size, so the only safe approach is to walk the chunk list.
func DecodeWAV(data []byte) (*PCM, error) {
	if len(data) < 12 {
		return nil, errors.New("codec: wav data too short for a RIFF header")
	}
	if string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, errors.New("codec: not a RIFF/WAVE stream")
	}

	var (
		haveFmt, haveData          bool
		formatTag                  uint16
		channels, sampleRate, bits int
		sampleData                 []byte
	)

chunks:
	for pos := 12; pos+8 <= len(data); {
		id := string(data[pos : pos+4])
		declared := binary.LittleEndian.Uint32(data[pos+4 : pos+8])
		body := pos + 8
		size := int(declared)

		end := body + size
		// A chunk's declared size can run past what's left in the file (an
		// encoder that streamed the header before it knew the true length)
		// or claim a value that doesn't fit in an int at all; either way,
		// clamp to what's actually there instead of trusting the field.
		overran := size < 0 || end < body || end > len(data)
		if overran {
			end = len(data)
		}

		switch id {
		case "fmt ":
			if end-body < 16 {
				return nil, fmt.Errorf("codec: fmt chunk too short (%d bytes)", end-body)
			}
			f := data[body:end]
			formatTag = binary.LittleEndian.Uint16(f[0:2])
			channels = int(binary.LittleEndian.Uint16(f[2:4]))
			sampleRate = int(binary.LittleEndian.Uint32(f[4:8]))
			bits = int(binary.LittleEndian.Uint16(f[14:16]))
			if formatTag == wavFormatExtensible {
				// The extensible layout appends: cbSize(2), validBits(2),
				// channelMask(4), then a 16-byte SubFormat GUID whose first
				// two bytes carry the real format code (the rest of the
				// GUID is the fixed KSDATAFORMAT_SUBTYPE suffix).
				const extExtraOffset = 16 + 2 + 2 + 4
				if len(f) < extExtraOffset+2 {
					return nil, errors.New("codec: extensible fmt chunk too short")
				}
				formatTag = binary.LittleEndian.Uint16(f[extExtraOffset : extExtraOffset+2])
			}
			haveFmt = true
		case "data":
			dataEnd := end
			unreliable := overran || size == 0
			if unreliable {
				dataEnd = len(data)
			}
			sampleData = data[body:dataEnd]
			haveData = true
			if unreliable {
				// The declared length can't be trusted, so everything from
				// here to EOF is taken as sample bytes; there is nothing
				// meaningful left to parse as another chunk.
				break chunks
			}
		}

		next := body + size
		if size%2 != 0 {
			next++ // chunks are padded to an even size; the pad byte isn't counted in size
		}
		if next <= pos || next > len(data) {
			break
		}
		pos = next
	}

	if !haveFmt {
		return nil, errors.New("codec: wav stream has no fmt chunk")
	}
	if !haveData {
		return nil, errors.New("codec: wav stream has no data chunk")
	}
	if channels <= 0 {
		return nil, errors.New("codec: wav stream declares zero channels")
	}

	samples, err := decodeWAVSamples(sampleData, formatTag, bits)
	if err != nil {
		return nil, err
	}
	// The rate comes straight out of the file's header and every conversion
	// downstream divides by it. See ValidSampleRate.
	if rate := sampleRate; !ValidSampleRate(rate) {
		return nil, fmt.Errorf("sample rate %d is outside %d-%d Hz", rate, MinSampleRate, MaxSampleRate)
	}
	return &PCM{SampleRate: sampleRate, Channels: channels, Data: samples}, nil
}

func decodeWAVSamples(raw []byte, formatTag uint16, bits int) ([]float32, error) {
	switch formatTag {
	case wavFormatPCM:
		switch bits {
		case 8:
			return decodePCM8(raw), nil
		case 16:
			return decodePCM16(raw), nil
		case 24:
			return decodePCM24(raw), nil
		case 32:
			return decodePCM32(raw), nil
		default:
			return nil, fmt.Errorf("codec: unsupported PCM bit depth %d", bits)
		}
	case wavFormatIEEEFloat:
		switch bits {
		case 32:
			return decodeFloat32(raw), nil
		case 64:
			return decodeFloat64(raw), nil
		default:
			return nil, fmt.Errorf("codec: unsupported float bit depth %d", bits)
		}
	default:
		return nil, fmt.Errorf("codec: unsupported wav format tag 0x%04x", formatTag)
	}
}

// decodePCM8 converts 8-bit PCM, which — uniquely among the PCM depths — is
// unsigned with 128 as its zero point.
func decodePCM8(raw []byte) []float32 {
	out := make([]float32, len(raw))
	for i, b := range raw {
		out[i] = (float32(b) - 128) / 128
	}
	return out
}

func decodePCM16(raw []byte) []float32 {
	n := len(raw) / 2
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		v := int16(binary.LittleEndian.Uint16(raw[i*2:]))
		out[i] = float32(v) / 32768
	}
	return out
}

func decodePCM24(raw []byte) []float32 {
	n := len(raw) / 3
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		b := raw[i*3 : i*3+3]
		v := int32(b[0]) | int32(b[1])<<8 | int32(b[2])<<16
		if v&0x800000 != 0 {
			v |= ^int32(0xFFFFFF) // sign-extend the 24-bit two's-complement value
		}
		out[i] = float32(v) / 8388608
	}
	return out
}

func decodePCM32(raw []byte) []float32 {
	n := len(raw) / 4
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		v := int32(binary.LittleEndian.Uint32(raw[i*4:]))
		out[i] = float32(v) / 2147483648
	}
	return out
}

func decodeFloat32(raw []byte) []float32 {
	n := len(raw) / 4
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
	}
	return out
}

func decodeFloat64(raw []byte) []float32 {
	n := len(raw) / 8
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		out[i] = float32(math.Float64frombits(binary.LittleEndian.Uint64(raw[i*8:])))
	}
	return out
}

// EncodeWAV writes 16-bit PCM WAV. It exists so a recorded calibration take
// or a test fixture can be written out and listened to.
func EncodeWAV(w io.Writer, p *PCM) error {
	if p == nil {
		return errors.New("codec: cannot encode a nil PCM")
	}
	channels := p.Channels
	if channels <= 0 {
		channels = 1
	}
	const bitsPerSample = 16
	blockAlign := channels * bitsPerSample / 8
	byteRate := p.SampleRate * blockAlign
	dataSize := len(p.Data) * 2

	var header [44]byte
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], uint32(36+dataSize))
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], wavFormatPCM)
	binary.LittleEndian.PutUint16(header[22:24], uint16(channels))
	binary.LittleEndian.PutUint32(header[24:28], uint32(p.SampleRate))
	binary.LittleEndian.PutUint32(header[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(header[32:34], uint16(blockAlign))
	binary.LittleEndian.PutUint16(header[34:36], bitsPerSample)
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], uint32(dataSize))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}

	buf := make([]byte, dataSize)
	for i, s := range p.Data {
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(quantize16(s)))
	}
	_, err := w.Write(buf)
	return err
}

// quantize16 clamps before converting so an out-of-range input (a hot mix,
// or a sum that briefly exceeds [-1,1]) wraps to silence-adjacent noise
// instead of an int16 overflow.
func quantize16(s float32) int16 {
	switch {
	case s >= 1:
		return 32767
	case s <= -1:
		return -32768
	default:
		return int16(s * 32768)
	}
}
