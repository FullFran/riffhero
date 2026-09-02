package midi

import (
	"os"
	"testing"

	"github.com/FullFran/riffhero/internal/practice"
)

// --- SMF byte-builder helpers -----------------------------------------
//
// Tests build their own bytes rather than shipping .mid fixtures, so the
// exact input to every case is visible right next to the assertions on it.

func beU16(v uint16) []byte { return []byte{byte(v >> 8), byte(v)} }
func beU32(v uint32) []byte { return []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)} }

// vlq encodes a MIDI variable-length quantity, mirroring reader.readVLQ.
func vlq(v uint32) []byte {
	buf := []byte{byte(v & 0x7F)}
	v >>= 7
	for v > 0 {
		buf = append([]byte{byte(v&0x7F) | 0x80}, buf...)
		v >>= 7
	}
	return buf
}

func chunk(id string, data []byte) []byte {
	out := append([]byte(id), beU32(uint32(len(data)))...)
	return append(out, data...)
}

func header(format, ntrks, division uint16) []byte {
	return chunk("MThd", append(append(beU16(format), beU16(ntrks)...), beU16(division)...))
}

func smf(hdr []byte, tracks ...[]byte) []byte {
	out := append([]byte{}, hdr...)
	for _, t := range tracks {
		out = append(out, t...)
	}
	return out
}

// trackBuilder assembles one MTrk chunk's event stream.
type trackBuilder struct{ buf []byte }

func newTrack() *trackBuilder { return &trackBuilder{} }

func (tb *trackBuilder) event(delta uint32, data ...byte) *trackBuilder {
	tb.buf = append(tb.buf, vlq(delta)...)
	tb.buf = append(tb.buf, data...)
	return tb
}

func (tb *trackBuilder) meta(delta uint32, metaType byte, payload []byte) *trackBuilder {
	tb.buf = append(tb.buf, vlq(delta)...)
	tb.buf = append(tb.buf, 0xFF, metaType)
	tb.buf = append(tb.buf, vlq(uint32(len(payload)))...)
	tb.buf = append(tb.buf, payload...)
	return tb
}

func (tb *trackBuilder) tempo(delta uint32, microsPerQuarter uint32) *trackBuilder {
	return tb.meta(delta, 0x51, []byte{byte(microsPerQuarter >> 16), byte(microsPerQuarter >> 8), byte(microsPerQuarter)})
}

func (tb *trackBuilder) timeSig(delta uint32, beats, unit byte) *trackBuilder {
	// unit is stored as a power of two (2 -> quarter note gets the beat).
	var denomPower byte
	for u := unit; u > 1; u >>= 1 {
		denomPower++
	}
	return tb.meta(delta, 0x58, []byte{beats, denomPower, 24, 8})
}

func (tb *trackBuilder) trackName(delta uint32, name string) *trackBuilder {
	return tb.meta(delta, 0x03, []byte(name))
}

func (tb *trackBuilder) instrumentName(delta uint32, name string) *trackBuilder {
	return tb.meta(delta, 0x04, []byte(name))
}

func (tb *trackBuilder) text(delta uint32, s string) *trackBuilder {
	return tb.meta(delta, 0x01, []byte(s))
}

func (tb *trackBuilder) programChange(delta uint32, channel, program byte) *trackBuilder {
	return tb.event(delta, 0xC0|channel, program)
}

func (tb *trackBuilder) noteOn(delta uint32, channel, pitch, velocity byte) *trackBuilder {
	return tb.event(delta, 0x90|channel, pitch, velocity)
}

func (tb *trackBuilder) noteOff(delta uint32, channel, pitch byte) *trackBuilder {
	return tb.event(delta, 0x80|channel, pitch, 0)
}

// raw appends bytes with no delta-time VLQ of their own, for constructing
// running-status sequences where a data byte immediately follows the
// previous event's data with no status byte in between.
func (tb *trackBuilder) raw(delta uint32, data ...byte) *trackBuilder {
	tb.buf = append(tb.buf, vlq(delta)...)
	tb.buf = append(tb.buf, data...)
	return tb
}

func (tb *trackBuilder) endOfTrack(delta uint32) *trackBuilder {
	return tb.meta(delta, 0x2F, nil)
}

func (tb *trackBuilder) chunk() []byte { return chunk("MTrk", tb.buf) }

// --- tests --------------------------------------------------------------

func TestParseFormat0(t *testing.T) {
	const division = 480
	clock := practice.Clock{SampleRate: 48000}

	track := newTrack().
		tempo(0, 500000). // 120 BPM
		trackName(0, "Lead").
		programChange(0, 0, 27). // Electric Guitar (clean)
		noteOn(0, 0, 60, 100).
		noteOff(division, 0, 60). // one quarter note later
		endOfTrack(0).
		chunk()

	data := smf(header(0, 1, division), track)

	song, err := Parse(data, clock)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if song.Title != "Lead" {
		t.Fatalf("Title = %q, want %q", song.Title, "Lead")
	}
	if len(song.Tracks) != 1 {
		t.Fatalf("len(Tracks) = %d, want 1", len(song.Tracks))
	}
	tr := song.Tracks[0]
	if tr.Name != "Lead" {
		t.Fatalf("track Name = %q, want %q", tr.Name, "Lead")
	}
	if tr.Instrument != "Electric Guitar (clean)" {
		t.Fatalf("track Instrument = %q, want %q", tr.Instrument, "Electric Guitar (clean)")
	}
	if len(tr.Notes) != 1 {
		t.Fatalf("len(Notes) = %d, want 1", len(tr.Notes))
	}
	n := tr.Notes[0]
	if n.MIDI != 60 {
		t.Fatalf("MIDI = %d, want 60", n.MIDI)
	}
	if n.String != 2 || n.Fret != 1 {
		t.Fatalf("position = string %d fret %d, want string 2 fret 1", n.String, n.Fret)
	}
	if n.Start != 0 {
		t.Fatalf("Start = %d, want 0", n.Start)
	}
	if want := clock.Frames(0.5); n.Duration != want {
		t.Fatalf("Duration = %d, want %d", n.Duration, want)
	}
}

func TestParseFormat1TwoTracksAndRunningStatus(t *testing.T) {
	const division = 96
	clock := practice.Clock{SampleRate: 48000}

	// Track 0: the tempo track. No notes of its own, so it must not become a
	// practice.Track, but its name becomes the song title.
	tempoTrack := newTrack().
		trackName(0, "My Song").
		tempo(0, 500000). // 120 BPM
		endOfTrack(0).
		chunk()

	// Track 1: a single 0x90 status byte, then three more note events that
	// omit it entirely and rely on running status — including a Note On
	// velocity 0 standing in for a Note Off.
	notes := newTrack().
		event(0, 0x90, 64, 90). // note on, explicit status
		raw(48, 67, 90).        // note on, running status
		raw(48, 64, 0).         // note off (velocity 0), running status
		raw(48, 67, 0).         // note off (velocity 0), running status
		endOfTrack(0).
		chunk()

	data := smf(header(1, 2, division), tempoTrack, notes)

	song, err := Parse(data, clock)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if song.Title != "My Song" {
		t.Fatalf("Title = %q, want %q", song.Title, "My Song")
	}
	if len(song.Tracks) != 1 {
		t.Fatalf("len(Tracks) = %d, want 1 (tempo track has no notes)", len(song.Tracks))
	}
	tr := song.Tracks[0]
	if tr.Name != "Track 2" {
		t.Fatalf("track Name = %q, want %q (fallback naming, 1-based)", tr.Name, "Track 2")
	}
	if len(tr.Notes) != 2 {
		t.Fatalf("len(Notes) = %d, want 2", len(tr.Notes))
	}

	quarter := clock.Frames(0.5) // 120 BPM, one quarter note
	half := quarter / 2

	byMIDI := map[uint8]practice.Note{}
	for _, n := range tr.Notes {
		byMIDI[n.MIDI] = n
	}
	n64, ok := byMIDI[64]
	if !ok {
		t.Fatalf("missing note MIDI 64")
	}
	if n64.Start != 0 || n64.Duration != quarter {
		t.Fatalf("note 64: Start=%d Duration=%d, want Start=0 Duration=%d", n64.Start, n64.Duration, quarter)
	}
	n67, ok := byMIDI[67]
	if !ok {
		t.Fatalf("missing note MIDI 67")
	}
	if n67.Start != half || n67.Duration != quarter {
		t.Fatalf("note 67: Start=%d Duration=%d, want Start=%d Duration=%d", n67.Start, n67.Duration, half, quarter)
	}
}

func TestTempoChangeMidSong(t *testing.T) {
	const division = 480
	clock := practice.Clock{SampleRate: 48000}

	// 125 BPM (480000 us/quarter -> exactly 1000 us/tick) for the first two
	// quarter notes, then 62.5 BPM (960000 us/quarter -> exactly 2000
	// us/tick) after. Numbers are chosen so every conversion below lands on
	// an exact frame count, with no rounding ambiguity to paper over.
	track := newTrack().
		tempo(0, 480000).
		noteOn(2*division, 0, 64, 100). // tick 960: 960*1000us = 0.96s
		tempo(0, 960000).               // tempo change exactly at the note's start
		noteOff(division, 0, 64).       // tick 1440: +480*2000us = 0.96s more
		endOfTrack(0).
		chunk()

	data := smf(header(0, 1, division), track)

	song, err := Parse(data, clock)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(song.Tracks) != 1 || len(song.Tracks[0].Notes) != 1 {
		t.Fatalf("unexpected track/note shape: %+v", song.Tracks)
	}
	n := song.Tracks[0].Notes[0]

	wantStart := clock.Frames(0.96)
	wantDuration := clock.Frames(0.96)
	if n.Start != wantStart {
		t.Fatalf("Start = %d, want %d", n.Start, wantStart)
	}
	if n.Duration != wantDuration {
		t.Fatalf("Duration = %d, want %d", n.Duration, wantDuration)
	}
}

func TestTimeSignature3_4ProducesThreeBeatBars(t *testing.T) {
	const division = 480
	clock := practice.Clock{SampleRate: 48000}

	track := newTrack().
		tempo(0, 500000).
		timeSig(0, 3, 4).
		noteOn(3*division, 0, 60, 100). // start of bar 2 (one 3/4 bar = 3 quarters)
		noteOff(division, 0, 60).
		endOfTrack(0).
		chunk()

	data := smf(header(0, 1, division), track)

	song, err := Parse(data, clock)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(song.Grid) == 0 {
		t.Fatal("Grid is empty")
	}
	for i, bar := range song.Grid {
		if bar.Sig.Beats != 3 || bar.Sig.Unit != 4 {
			t.Fatalf("bar %d Sig = %+v, want {Beats:3 Unit:4}", i, bar.Sig)
		}
		if len(bar.Beats) != 3 {
			t.Fatalf("bar %d has %d beats, want 3", i, len(bar.Beats))
		}
	}
	note := song.Tracks[0].Notes[0]
	if idx := song.Grid.BarAt(note.Start); idx < 0 {
		t.Fatalf("note start %d falls outside the grid", note.Start)
	}
}

func TestNoteOnVelocityZeroIsNoteOff(t *testing.T) {
	const division = 480
	clock := practice.Clock{SampleRate: 48000}

	track := newTrack().
		tempo(0, 500000).
		noteOn(0, 0, 60, 100).
		noteOn(division, 0, 60, 0). // velocity 0 note-on == note-off
		endOfTrack(0).
		chunk()

	data := smf(header(0, 1, division), track)

	song, err := Parse(data, clock)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(song.Tracks) != 1 || len(song.Tracks[0].Notes) != 1 {
		t.Fatalf("unexpected shape: %+v", song.Tracks)
	}
	n := song.Tracks[0].Notes[0]
	if want := clock.Frames(0.5); n.Duration != want {
		t.Fatalf("Duration = %d, want %d", n.Duration, want)
	}
}

func TestPercussionChannelDropped(t *testing.T) {
	const division = 480
	clock := practice.Clock{SampleRate: 48000}

	t.Run("mixed with a playable channel", func(t *testing.T) {
		track := newTrack().
			tempo(0, 500000).
			noteOn(0, 9, 38, 100). // percussion (channel 10): snare
			noteOn(0, 0, 60, 100). // channel 1: playable
			noteOff(240, 9, 38).
			noteOff(240, 0, 60).
			endOfTrack(0).
			chunk()

		data := smf(header(0, 1, division), track)
		song, err := Parse(data, clock)
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		if len(song.Tracks) != 1 {
			t.Fatalf("len(Tracks) = %d, want 1", len(song.Tracks))
		}
		notes := song.Tracks[0].Notes
		if len(notes) != 1 {
			t.Fatalf("len(Notes) = %d, want 1", len(notes))
		}
		if notes[0].MIDI != 60 {
			t.Fatalf("surviving note MIDI = %d, want 60 (percussion note should be dropped)", notes[0].MIDI)
		}
	})

	t.Run("percussion-only track produces no practice.Track", func(t *testing.T) {
		track := newTrack().
			tempo(0, 500000).
			noteOn(0, 9, 38, 100).
			noteOff(240, 9, 38).
			endOfTrack(0).
			chunk()

		data := smf(header(0, 1, division), track)
		song, err := Parse(data, clock)
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		if len(song.Tracks) != 0 {
			t.Fatalf("len(Tracks) = %d, want 0", len(song.Tracks))
		}
	})
}

func TestTruncatedInputReturnsError(t *testing.T) {
	const division = 480
	valid := smf(header(0, 1, division), newTrack().
		tempo(0, 500000).
		noteOn(0, 0, 60, 100).
		noteOff(division, 0, 60).
		endOfTrack(0).
		chunk())

	tests := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"only magic", valid[:4]},
		{"header length cut short", valid[:6]},
		{"header cut before division", valid[:12]},
		{"track chunk header only, no body", valid[:22]},
		{"track body missing its last byte", valid[:len(valid)-1]},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Parse(tt.data, practice.Clock{SampleRate: 48000}); err == nil {
				t.Fatal("Parse returned no error for truncated input")
			}
		})
	}

	t.Run("bad magic", func(t *testing.T) {
		bad := append([]byte{}, valid...)
		copy(bad[:4], "XyZw")
		if _, err := Parse(bad, practice.Clock{SampleRate: 48000}); err == nil {
			t.Fatal("Parse returned no error for bad MThd magic")
		}
	})
}

func TestSMPTEDivision(t *testing.T) {
	// 25 fps, 40 ticks per frame => 1000 ticks per second, independent of any
	// tempo event.
	fps := int8(-25)
	division := uint16(uint8(fps))<<8 | uint16(40)
	clock := practice.Clock{SampleRate: 48000}

	track := newTrack().
		noteOn(500, 0, 60, 100). // 500 ticks @ 1000 ticks/s = 0.5s
		noteOff(500, 0, 60).     // +500 ticks = another 0.5s
		endOfTrack(0).
		chunk()

	data := smf(header(0, 1, division), track)
	song, err := Parse(data, clock)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(song.Tracks) != 1 || len(song.Tracks[0].Notes) != 1 {
		t.Fatalf("unexpected shape: %+v", song.Tracks)
	}
	n := song.Tracks[0].Notes[0]
	if want := clock.Frames(0.5); n.Start != want {
		t.Fatalf("Start = %d, want %d", n.Start, want)
	}
	if want := clock.Frames(0.5); n.Duration != want {
		t.Fatalf("Duration = %d, want %d", n.Duration, want)
	}
}

// A MIDI file written for another instrument routinely goes below or above a
// guitar. Both notes here are outside StandardTuning's MIDI 40-88.
func TestOutOfRangeNotesMoveByWholeOctaves(t *testing.T) {
	const division = 480
	clock := practice.Clock{SampleRate: 48000}

	// 20 is far below the open low E (40) and 110 far above the top fretted
	// note (88, string 1 fret 24).
	track := newTrack().
		tempo(0, 500000).
		noteOn(0, 0, 20, 100).
		noteOff(division, 0, 20).
		noteOn(0, 0, 110, 100).
		noteOff(division, 0, 110).
		endOfTrack(0).
		chunk()

	data := smf(header(0, 1, division), track)
	song, err := Parse(data, clock)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(song.Tracks) != 1 || len(song.Tracks[0].Notes) != 2 {
		t.Fatalf("unexpected shape: %+v", song.Tracks)
	}

	// 20 comes up two octaves to 44, 110 comes down two to 86. Moving by
	// octaves keeps a bass line a bass line; clamping to the nearest end of
	// the range would flatten it into a drone on one note.
	got := map[uint8]bool{}
	for _, n := range song.Tracks[0].Notes {
		got[n.MIDI] = true
	}
	for _, want := range []uint8{44, 86} {
		if !got[want] {
			t.Fatalf("expected a note at MIDI %d, got %+v", want, song.Tracks[0].Notes)
		}
	}
}

// The tablature and the expected pitch must agree. They did not once: an
// out-of-range note kept its original MIDI number while its string and fret
// came from a clamped one, so the tab showed a position that sounds something
// else and the scorer waited for a pitch no guitar can produce.
func TestEveryNoteSoundsWhatItsTabSays(t *testing.T) {
	const division = 480
	clock := practice.Clock{SampleRate: 48000}

	b := newTrack().tempo(0, 500000)
	for _, midi := range []uint8{20, 33, 40, 55, 64, 88, 100, 110} {
		b = b.noteOn(0, 0, midi, 100).noteOff(division, 0, midi)
	}
	data := smf(header(0, 1, division), b.endOfTrack(0).chunk())

	song, err := Parse(data, clock)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(song.Tracks) != 1 {
		t.Fatalf("unexpected shape: %+v", song.Tracks)
	}

	tuning := song.Tracks[0].Tuning
	for i, n := range song.Tracks[0].Notes {
		if got := tuning.MIDI(n.String, n.Fret); got != n.MIDI {
			t.Fatalf("note %d says MIDI %d but string %d fret %d sounds %d",
				i, n.MIDI, n.String, n.Fret, got)
		}
	}
}

func TestDefaultsWhenFileSaysNothing(t *testing.T) {
	const division = 480
	clock := practice.Clock{SampleRate: 48000}

	// No tempo or time-signature meta events anywhere.
	track := newTrack().
		noteOn(0, 0, 60, 100).
		noteOff(division, 0, 60).
		endOfTrack(0).
		chunk()

	data := smf(header(0, 1, division), track)
	song, err := Parse(data, clock)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(song.Grid) == 0 {
		t.Fatal("Grid is empty")
	}
	first := song.Grid[0]
	if first.BPM != 120 {
		t.Fatalf("BPM = %v, want 120", first.BPM)
	}
	if first.Sig.Beats != 4 || first.Sig.Unit != 4 {
		t.Fatalf("Sig = %+v, want 4/4", first.Sig)
	}
}

func TestTitleFallsBackToTextEvent(t *testing.T) {
	const division = 480
	clock := practice.Clock{SampleRate: 48000}

	track := newTrack().
		text(0, "Untitled Session").
		tempo(0, 500000).
		noteOn(0, 0, 60, 100).
		noteOff(division, 0, 60).
		endOfTrack(0).
		chunk()

	data := smf(header(0, 1, division), track)
	song, err := Parse(data, clock)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if song.Title != "Untitled Session" {
		t.Fatalf("Title = %q, want %q", song.Title, "Untitled Session")
	}
}

func TestParseFileRoundTrip(t *testing.T) {
	const division = 480
	clock := practice.Clock{SampleRate: 48000}

	track := newTrack().
		tempo(0, 500000).
		noteOn(0, 0, 60, 100).
		noteOff(division, 0, 60).
		endOfTrack(0).
		chunk()
	data := smf(header(0, 1, division), track)

	path := t.TempDir() + "/song.mid"
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}

	song, err := ParseFile(path, clock)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if song.Source != path {
		t.Fatalf("Source = %q, want %q", song.Source, path)
	}
	if len(song.Tracks) != 1 || len(song.Tracks[0].Notes) != 1 {
		t.Fatalf("unexpected shape: %+v", song.Tracks)
	}
}
