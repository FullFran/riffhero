package codec

import (
	"bytes"
	"testing"
)

// Building a byte-exact, valid MP3 frame by hand (Huffman-coded MDCT
// coefficients and all) isn't practical to assemble in a test; go-mp3 itself
// is exercised elsewhere in the ebiten dependency tree. What this package
// controls — and must get right without downloading any fixture — is that a
// stream sniffing as MP3 gets routed to DecodeMP3, and that DecodeMP3 fails
// cleanly instead of panicking on data that isn't actually a valid stream.

func TestDecode_RoutesID3ToMP3Decoder(t *testing.T) {
	// A real ID3v2 header followed by garbage frame data: sniffing must
	// still pick MP3 (that's the whole point of an ID3 tag), even though
	// decoding it then fails for lack of real audio frames.
	id3 := append([]byte("ID3\x03\x00\x00"), bytes.Repeat([]byte{0}, 20)...)
	if got := sniff(id3); got != formatMP3 {
		t.Fatalf("sniff(ID3-tagged data) = %v, want formatMP3", got)
	}
	if _, err := Decode(id3); err == nil {
		t.Fatal("Decode on an ID3 tag with no real MP3 frames: want an error, got nil")
	}
}

func TestDecode_RoutesFrameSyncToMP3Decoder(t *testing.T) {
	frameSync := []byte{0xFF, 0xFB, 0x90, 0x00}
	if got := sniff(frameSync); got != formatMP3 {
		t.Fatalf("sniff(frame sync) = %v, want formatMP3", got)
	}
	// The sync word alone isn't a decodable frame, so this must still fail —
	// but via a returned error, not a panic.
	if _, err := Decode(frameSync); err == nil {
		t.Fatal("Decode on a bare frame-sync with no frame body: want an error, got nil")
	}
}

func TestDecodeMP3_GarbageReturnsErrorNotPanic(t *testing.T) {
	inputs := [][]byte{
		nil,
		{},
		{0x00, 0x01, 0x02},
		bytes.Repeat([]byte{0xAB}, 64),
	}
	for _, in := range inputs {
		if _, err := DecodeMP3(in); err == nil {
			t.Fatalf("DecodeMP3(%v): want an error, got nil", in)
		}
	}
}
