package codec

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestSniff(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want fileFormat
	}{
		{"wav", buildWAV(pcmFmtBody(wavFormatPCM, 1, 44100, 16), "", nil, []byte{0, 0}), formatWAV},
		{"flac signature", []byte("fLaC\x00\x00\x00\x22"), formatFLAC},
		{"mp3 id3", append([]byte("ID3"), bytes.Repeat([]byte{0}, 10)...), formatMP3},
		{"mp3 frame sync 0xE", []byte{0xFF, 0xE2, 0x00, 0x00}, formatMP3},
		{"mp3 frame sync 0xF", []byte{0xFF, 0xFB, 0x00, 0x00}, formatMP3},
		{"empty", nil, formatUnknown},
		{"garbage", []byte("not audio at all"), formatUnknown},
		{"riff but not wave", []byte("RIFF\x00\x00\x00\x00AVI "), formatUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sniff(c.data); got != c.want {
				t.Fatalf("sniff(%s) = %v, want %v", c.name, got, c.want)
			}
		})
	}
}

func TestDecode_RoutesWAVByFormat(t *testing.T) {
	wav := buildWAV(pcmFmtBody(wavFormatPCM, 1, 44100, 16), "", nil, []byte{0, 128})
	pcm, err := Decode(wav)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if pcm.SampleRate != 44100 || pcm.Channels != 1 {
		t.Fatalf("Decode routed to the wrong decoder: got rate=%d channels=%d", pcm.SampleRate, pcm.Channels)
	}
}

func TestDecode_GarbageReturnsError(t *testing.T) {
	if _, err := Decode([]byte("definitely not an audio file")); err == nil {
		t.Fatal("Decode on garbage: want an error, got nil")
	}
	if _, err := Decode(nil); err == nil {
		t.Fatal("Decode on nil: want an error, got nil")
	}
}

func TestDecodeFile_ReadsAndSniffsWAV(t *testing.T) {
	wav := buildWAV(pcmFmtBody(wavFormatPCM, 1, 8000, 16), "", nil, []byte{10, 0, 20, 0})
	path := filepath.Join(t.TempDir(), "track.wav")
	if err := os.WriteFile(path, wav, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	pcm, err := DecodeFile(path)
	if err != nil {
		t.Fatalf("DecodeFile: %v", err)
	}
	if pcm.SampleRate != 8000 || len(pcm.Data) != 2 {
		t.Fatalf("got rate=%d samples=%d, want 8000/2", pcm.SampleRate, len(pcm.Data))
	}
}

func TestDecodeFile_BytesWinOverAMisleadingExtension(t *testing.T) {
	// Byte sniffing must drive format selection, with the extension only as
	// a fallback — so valid WAV bytes must decode even under an unrelated
	// extension.
	wav := buildWAV(pcmFmtBody(wavFormatPCM, 1, 8000, 16), "", nil, []byte{5, 0})
	path := filepath.Join(t.TempDir(), "track.dat")
	if err := os.WriteFile(path, wav, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	pcm, err := DecodeFile(path)
	if err != nil {
		t.Fatalf("DecodeFile: %v", err)
	}
	if pcm.SampleRate != 8000 {
		t.Fatalf("got rate=%d, want 8000", pcm.SampleRate)
	}
}

func TestDecodeFile_CorruptedMagicStillErrorsViaFallback(t *testing.T) {
	// Even once the extension fallback kicks in, genuinely broken bytes must
	// still surface as an error rather than a panic.
	wav := buildWAV(pcmFmtBody(wavFormatPCM, 1, 8000, 16), "", nil, []byte{5, 0})
	broken := append([]byte(nil), wav...)
	copy(broken[0:4], "XXXX")

	path := filepath.Join(t.TempDir(), "track.wav")
	if err := os.WriteFile(path, broken, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := DecodeFile(path); err == nil {
		t.Fatal("DecodeFile on WAV bytes with a corrupted RIFF magic: want an error even via the extension fallback")
	}
}

func TestDecodeFile_MissingFileReturnsError(t *testing.T) {
	if _, err := DecodeFile(filepath.Join(t.TempDir(), "does-not-exist.wav")); err == nil {
		t.Fatal("DecodeFile on a missing file: want an error, got nil")
	}
}

func TestDecodeFile_UnknownExtensionAndUnsniffableDataErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "track.bin")
	if err := os.WriteFile(path, []byte("not audio"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := DecodeFile(path); err == nil {
		t.Fatal("DecodeFile on unsniffable data with an unknown extension: want an error, got nil")
	}
}
