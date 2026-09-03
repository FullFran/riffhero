package codec

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

// --- test-local WAV byte assembly -----------------------------------------
//
// These helpers build RIFF/WAVE bytes independently of the package's own
// EncodeWAV, so the DecodeWAV tests below are exercising the parser against
// bytes it did not produce itself.

func appendChunk(buf *bytes.Buffer, id string, body []byte) {
	buf.WriteString(id)
	var sz [4]byte
	binary.LittleEndian.PutUint32(sz[:], uint32(len(body)))
	buf.Write(sz[:])
	buf.Write(body)
	if len(body)%2 != 0 {
		buf.WriteByte(0) // odd chunks are padded to an even size
	}
}

// buildWAV assembles a minimal RIFF/WAVE stream: fmt, an optional chunk in
// between (e.g. LIST, to prove the parser doesn't assume fmt is immediately
// followed by data), then data.
func buildWAV(fmtBody []byte, extraID string, extraBody []byte, dataBody []byte) []byte {
	var body bytes.Buffer
	body.WriteString("WAVE")
	appendChunk(&body, "fmt ", fmtBody)
	if extraID != "" {
		appendChunk(&body, extraID, extraBody)
	}
	appendChunk(&body, "data", dataBody)

	var out bytes.Buffer
	out.WriteString("RIFF")
	var sz [4]byte
	binary.LittleEndian.PutUint32(sz[:], uint32(body.Len()))
	out.Write(sz[:])
	out.Write(body.Bytes())
	return out.Bytes()
}

func pcmFmtBody(formatTag uint16, channels, sampleRate, bits int) []byte {
	blockAlign := channels * bits / 8
	byteRate := sampleRate * blockAlign
	buf := make([]byte, 16)
	binary.LittleEndian.PutUint16(buf[0:2], formatTag)
	binary.LittleEndian.PutUint16(buf[2:4], uint16(channels))
	binary.LittleEndian.PutUint32(buf[4:8], uint32(sampleRate))
	binary.LittleEndian.PutUint32(buf[8:12], uint32(byteRate))
	binary.LittleEndian.PutUint16(buf[12:14], uint16(blockAlign))
	binary.LittleEndian.PutUint16(buf[14:16], uint16(bits))
	return buf
}

// extensibleFmtBody builds a WAVE_FORMAT_EXTENSIBLE fmt chunk whose
// SubFormat GUID starts with subFormatTag — the field DecodeWAV must read to
// find the real sample format.
func extensibleFmtBody(subFormatTag uint16, channels, sampleRate, bits int) []byte {
	blockAlign := channels * bits / 8
	byteRate := sampleRate * blockAlign
	buf := make([]byte, 40)
	binary.LittleEndian.PutUint16(buf[0:2], wavFormatExtensible)
	binary.LittleEndian.PutUint16(buf[2:4], uint16(channels))
	binary.LittleEndian.PutUint32(buf[4:8], uint32(sampleRate))
	binary.LittleEndian.PutUint32(buf[8:12], uint32(byteRate))
	binary.LittleEndian.PutUint16(buf[12:14], uint16(blockAlign))
	binary.LittleEndian.PutUint16(buf[14:16], uint16(bits))
	binary.LittleEndian.PutUint16(buf[16:18], 22)           // cbSize
	binary.LittleEndian.PutUint16(buf[18:20], uint16(bits)) // valid bits per sample
	binary.LittleEndian.PutUint32(buf[20:24], 0)            // channel mask
	binary.LittleEndian.PutUint16(buf[24:26], subFormatTag) // GUID bytes 0-1: the real format code
	// GUID bytes 2-15 are the fixed KSDATAFORMAT_SUBTYPE suffix; DecodeWAV
	// never reads them, so they are left zeroed here.
	return buf
}

func le16(v int16) []byte {
	var b [2]byte
	binary.LittleEndian.PutUint16(b[:], uint16(v))
	return b[:]
}

func le24(v int32) []byte {
	return []byte{byte(v), byte(v >> 8), byte(v >> 16)}
}

func le32(v int32) []byte {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], uint32(v))
	return b[:]
}

func leF32(v float32) []byte {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], math.Float32bits(v))
	return b[:]
}

func leF64(v float64) []byte {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], math.Float64bits(v))
	return b[:]
}

// --- round trip -------------------------------------------------------------

func TestWAV_RoundTripSine(t *testing.T) {
	const rate = 44100
	n := 1000
	data := make([]float32, n)
	for i := range data {
		data[i] = float32(0.6 * math.Sin(2*math.Pi*440*float64(i)/rate))
	}
	src := &PCM{SampleRate: rate, Channels: 1, Data: data}

	var buf bytes.Buffer
	if err := EncodeWAV(&buf, src); err != nil {
		t.Fatalf("EncodeWAV: %v", err)
	}

	got, err := DecodeWAV(buf.Bytes())
	if err != nil {
		t.Fatalf("DecodeWAV: %v", err)
	}
	if got.SampleRate != rate || got.Channels != 1 {
		t.Fatalf("got rate=%d channels=%d, want %d/1", got.SampleRate, got.Channels, rate)
	}
	if len(got.Data) != len(src.Data) {
		t.Fatalf("got %d samples, want %d", len(got.Data), len(src.Data))
	}
	// 16-bit quantisation error is at most 1/32768 per sample; allow a small
	// margin for the round-to-nearest-int conversion.
	const tol = 1.0 / 32768 * 1.5
	for i := range src.Data {
		if !closeEnough(got.Data[i], src.Data[i], tol) {
			t.Fatalf("sample %d = %v, want %v (within %v)", i, got.Data[i], src.Data[i], tol)
		}
	}
}

// --- one test per WAV sample format -----------------------------------------

func TestWAV_PCM8(t *testing.T) {
	fmtBody := pcmFmtBody(wavFormatPCM, 1, 8000, 8)
	dataBody := []byte{0, 128, 255} // min, zero, max as unsigned 8-bit
	wav := buildWAV(fmtBody, "", nil, dataBody)

	got, err := DecodeWAV(wav)
	if err != nil {
		t.Fatalf("DecodeWAV: %v", err)
	}
	want := []float32{-1, 0, 0.9921875}
	for i, w := range want {
		if !closeEnough(got.Data[i], w, 1e-6) {
			t.Fatalf("sample %d = %v, want %v", i, got.Data[i], w)
		}
	}
}

func TestWAV_PCM16(t *testing.T) {
	fmtBody := pcmFmtBody(wavFormatPCM, 1, 44100, 16)
	var dataBody bytes.Buffer
	for _, v := range []int16{-32768, 0, 32767} {
		dataBody.Write(le16(v))
	}
	wav := buildWAV(fmtBody, "", nil, dataBody.Bytes())

	got, err := DecodeWAV(wav)
	if err != nil {
		t.Fatalf("DecodeWAV: %v", err)
	}
	want := []float32{-1, 0, 32767.0 / 32768}
	for i, w := range want {
		if !closeEnough(got.Data[i], w, 1e-6) {
			t.Fatalf("sample %d = %v, want %v", i, got.Data[i], w)
		}
	}
}

func TestWAV_PCM24(t *testing.T) {
	fmtBody := pcmFmtBody(wavFormatPCM, 1, 44100, 24)
	var dataBody bytes.Buffer
	for _, v := range []int32{-8388608, 0, 8388607} {
		dataBody.Write(le24(v))
	}
	wav := buildWAV(fmtBody, "", nil, dataBody.Bytes())

	got, err := DecodeWAV(wav)
	if err != nil {
		t.Fatalf("DecodeWAV: %v", err)
	}
	want := []float32{-1, 0, 8388607.0 / 8388608}
	for i, w := range want {
		if !closeEnough(got.Data[i], w, 1e-6) {
			t.Fatalf("sample %d = %v, want %v", i, got.Data[i], w)
		}
	}
}

func TestWAV_PCM32(t *testing.T) {
	fmtBody := pcmFmtBody(wavFormatPCM, 1, 44100, 32)
	var dataBody bytes.Buffer
	for _, v := range []int32{-2147483648, 0, 2147483647} {
		dataBody.Write(le32(v))
	}
	wav := buildWAV(fmtBody, "", nil, dataBody.Bytes())

	got, err := DecodeWAV(wav)
	if err != nil {
		t.Fatalf("DecodeWAV: %v", err)
	}
	want := []float32{-1, 0, 2147483647.0 / 2147483648}
	for i, w := range want {
		if !closeEnough(got.Data[i], w, 1e-6) {
			t.Fatalf("sample %d = %v, want %v", i, got.Data[i], w)
		}
	}
}

func TestWAV_Float32(t *testing.T) {
	fmtBody := pcmFmtBody(wavFormatIEEEFloat, 1, 44100, 32)
	var dataBody bytes.Buffer
	for _, v := range []float32{-1, 0, 0.5, 0.999} {
		dataBody.Write(leF32(v))
	}
	wav := buildWAV(fmtBody, "", nil, dataBody.Bytes())

	got, err := DecodeWAV(wav)
	if err != nil {
		t.Fatalf("DecodeWAV: %v", err)
	}
	want := []float32{-1, 0, 0.5, 0.999}
	for i, w := range want {
		if got.Data[i] != w {
			t.Fatalf("sample %d = %v, want exactly %v", i, got.Data[i], w)
		}
	}
}

func TestWAV_Float64(t *testing.T) {
	fmtBody := pcmFmtBody(wavFormatIEEEFloat, 1, 44100, 64)
	var dataBody bytes.Buffer
	for _, v := range []float64{-1, 0, 0.25} {
		dataBody.Write(leF64(v))
	}
	wav := buildWAV(fmtBody, "", nil, dataBody.Bytes())

	got, err := DecodeWAV(wav)
	if err != nil {
		t.Fatalf("DecodeWAV: %v", err)
	}
	want := []float32{-1, 0, 0.25}
	for i, w := range want {
		if got.Data[i] != w {
			t.Fatalf("sample %d = %v, want exactly %v", i, got.Data[i], w)
		}
	}
}

func TestWAV_Extensible(t *testing.T) {
	fmtBody := extensibleFmtBody(wavFormatPCM, 2, 48000, 16)
	var dataBody bytes.Buffer
	for _, v := range []int16{-32768, 32767, 0, 100} { // one stereo frame + one more
		dataBody.Write(le16(v))
	}
	wav := buildWAV(fmtBody, "", nil, dataBody.Bytes())

	got, err := DecodeWAV(wav)
	if err != nil {
		t.Fatalf("DecodeWAV: %v", err)
	}
	if got.Channels != 2 || got.SampleRate != 48000 {
		t.Fatalf("got channels=%d rate=%d, want 2/48000", got.Channels, got.SampleRate)
	}
	want := []float32{-1, 32767.0 / 32768, 0, 100.0 / 32768}
	for i, w := range want {
		if !closeEnough(got.Data[i], w, 1e-6) {
			t.Fatalf("sample %d = %v, want %v", i, got.Data[i], w)
		}
	}
}

func TestWAV_ExtensibleFloat(t *testing.T) {
	fmtBody := extensibleFmtBody(wavFormatIEEEFloat, 1, 44100, 32)
	var dataBody bytes.Buffer
	dataBody.Write(leF32(0.75))
	wav := buildWAV(fmtBody, "", nil, dataBody.Bytes())

	got, err := DecodeWAV(wav)
	if err != nil {
		t.Fatalf("DecodeWAV: %v", err)
	}
	if got.Data[0] != 0.75 {
		t.Fatalf("sample 0 = %v, want 0.75", got.Data[0])
	}
}

// --- chunk-layout edge cases -------------------------------------------------

func TestWAV_ChunkBeforeData(t *testing.T) {
	fmtBody := pcmFmtBody(wavFormatPCM, 1, 44100, 16)
	listBody := []byte("INFOIART\x05\x00\x00\x00Test\x00") // arbitrary metadata payload
	var dataBody bytes.Buffer
	dataBody.Write(le16(1234))
	dataBody.Write(le16(-1234))

	wav := buildWAV(fmtBody, "LIST", listBody, dataBody.Bytes())
	got, err := DecodeWAV(wav)
	if err != nil {
		t.Fatalf("DecodeWAV with a LIST chunk before data: %v", err)
	}
	want := []float32{1234.0 / 32768, -1234.0 / 32768}
	for i, w := range want {
		if !closeEnough(got.Data[i], w, 1e-6) {
			t.Fatalf("sample %d = %v, want %v", i, got.Data[i], w)
		}
	}
}

func TestWAV_OddSizedChunkPadding(t *testing.T) {
	fmtBody := pcmFmtBody(wavFormatPCM, 1, 44100, 8)
	// An odd-length LIST chunk forces a pad byte; if DecodeWAV fails to skip
	// it, the following data chunk's header is read one byte off and either
	// the parse fails or the samples come out corrupted.
	listBody := []byte{1, 2, 3}
	dataBody := []byte{10, 20, 30}

	wav := buildWAV(fmtBody, "LIST", listBody, dataBody)
	got, err := DecodeWAV(wav)
	if err != nil {
		t.Fatalf("DecodeWAV with odd-sized chunk padding: %v", err)
	}
	want := []float32{(10 - 128) / 128.0, (20 - 128) / 128.0, (30 - 128) / 128.0}
	for i, w := range want {
		if !closeEnough(got.Data[i], w, 1e-6) {
			t.Fatalf("sample %d = %v, want %v", i, got.Data[i], w)
		}
	}
}

func TestWAV_DataChunkWithZeroDeclaredSize(t *testing.T) {
	// Some encoders write 0 as a placeholder length for a streamed data
	// chunk; the real samples still follow it up to EOF.
	fmtBody := pcmFmtBody(wavFormatPCM, 1, 44100, 16)

	var body bytes.Buffer
	body.WriteString("WAVE")
	appendChunk(&body, "fmt ", fmtBody)
	body.WriteString("data")
	var zero [4]byte
	body.Write(zero[:]) // declared size 0
	body.Write(le16(500))
	body.Write(le16(-500))

	var wav bytes.Buffer
	wav.WriteString("RIFF")
	var sz [4]byte
	binary.LittleEndian.PutUint32(sz[:], uint32(body.Len()))
	wav.Write(sz[:])
	wav.Write(body.Bytes())

	got, err := DecodeWAV(wav.Bytes())
	if err != nil {
		t.Fatalf("DecodeWAV with a zero-declared data size: %v", err)
	}
	want := []float32{500.0 / 32768, -500.0 / 32768}
	if len(got.Data) != 2 {
		t.Fatalf("got %d samples, want 2", len(got.Data))
	}
	for i, w := range want {
		if !closeEnough(got.Data[i], w, 1e-6) {
			t.Fatalf("sample %d = %v, want %v", i, got.Data[i], w)
		}
	}
}

func TestWAV_DataChunkWithOversizedDeclaredSize(t *testing.T) {
	// A declared size bigger than what's actually in the file must not
	// panic (or read past the buffer) — clamp to what's present.
	fmtBody := pcmFmtBody(wavFormatPCM, 1, 44100, 16)

	var body bytes.Buffer
	body.WriteString("WAVE")
	appendChunk(&body, "fmt ", fmtBody)
	body.WriteString("data")
	var sz [4]byte
	binary.LittleEndian.PutUint32(sz[:], 0xFFFFFFFF) // absurd declared size
	body.Write(sz[:])
	body.Write(le16(42))

	var wav bytes.Buffer
	wav.WriteString("RIFF")
	var riffSz [4]byte
	binary.LittleEndian.PutUint32(riffSz[:], uint32(body.Len()))
	wav.Write(riffSz[:])
	wav.Write(body.Bytes())

	got, err := DecodeWAV(wav.Bytes())
	if err != nil {
		t.Fatalf("DecodeWAV with an oversized declared data size: %v", err)
	}
	if len(got.Data) != 1 {
		t.Fatalf("got %d samples, want 1", len(got.Data))
	}
	if !closeEnough(got.Data[0], 42.0/32768, 1e-6) {
		t.Fatalf("sample 0 = %v, want %v", got.Data[0], 42.0/32768)
	}
}

// --- malformed input ---------------------------------------------------------

func TestWAV_MissingFmtChunkErrors(t *testing.T) {
	var body bytes.Buffer
	body.WriteString("WAVE")
	appendChunk(&body, "data", []byte{1, 2, 3, 4})
	var wav bytes.Buffer
	wav.WriteString("RIFF")
	var sz [4]byte
	binary.LittleEndian.PutUint32(sz[:], uint32(body.Len()))
	wav.Write(sz[:])
	wav.Write(body.Bytes())

	if _, err := DecodeWAV(wav.Bytes()); err == nil {
		t.Fatal("DecodeWAV with no fmt chunk: want an error, got nil")
	}
}

func TestWAV_MissingDataChunkErrors(t *testing.T) {
	var body bytes.Buffer
	body.WriteString("WAVE")
	appendChunk(&body, "fmt ", pcmFmtBody(wavFormatPCM, 1, 44100, 16))
	var wav bytes.Buffer
	wav.WriteString("RIFF")
	var sz [4]byte
	binary.LittleEndian.PutUint32(sz[:], uint32(body.Len()))
	wav.Write(sz[:])
	wav.Write(body.Bytes())

	if _, err := DecodeWAV(wav.Bytes()); err == nil {
		t.Fatal("DecodeWAV with no data chunk: want an error, got nil")
	}
}

func TestWAV_UnsupportedFormatTagErrors(t *testing.T) {
	fmtBody := pcmFmtBody(0x0002, 1, 44100, 16) // WAVE_FORMAT_ADPCM: not supported
	wav := buildWAV(fmtBody, "", nil, []byte{1, 2, 3, 4})

	if _, err := DecodeWAV(wav); err == nil {
		t.Fatal("DecodeWAV with an unsupported format tag: want an error, got nil")
	}
}

func TestWAV_UnsupportedBitDepthErrors(t *testing.T) {
	fmtBody := pcmFmtBody(wavFormatPCM, 1, 44100, 12) // not a supported PCM depth
	wav := buildWAV(fmtBody, "", nil, []byte{1, 2, 3, 4})

	if _, err := DecodeWAV(wav); err == nil {
		t.Fatal("DecodeWAV with an unsupported bit depth: want an error, got nil")
	}
}

func TestWAV_GarbageDoesNotPanic(t *testing.T) {
	garbage := [][]byte{
		nil,
		{0, 1, 2},
		bytes.Repeat([]byte{0xFF}, 100),
	}
	for _, g := range garbage {
		if _, err := DecodeWAV(g); err == nil {
			t.Fatalf("DecodeWAV(%v): want an error, got nil", g)
		}
	}
}
