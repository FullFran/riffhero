package midi

import (
	"fmt"
	"sort"

	"github.com/FullFran/riffhero/internal/practice"
)

// rawNote is one Note On/Note Off pair from a track, still expressed in SMF
// ticks; frame conversion happens once at song assembly, after the whole
// file's tempo map is known.
type rawNote struct {
	startTick int64
	endTick   int64
	midi      uint8
}

// parsedTrack is everything one MTrk chunk contributed, before it becomes a
// practice.Track (or is dropped for having no playable notes).
type parsedTrack struct {
	name         string
	instrument   string
	text         string // first generic Text meta event, for Title's fallback
	firstProgram byte
	hasProgram   bool
	notes        []rawNote
}

// Meta event types this importer gives distinct meaning to. Everything else
// is skipped by its declared length, which the SMF format guarantees is
// always safe: every meta event carries its own size.
const (
	metaText           = 0x01
	metaTrackName      = 0x03
	metaInstrumentName = 0x04
	metaTempo          = 0x51
	metaTimeSignature  = 0x58
	metaEndOfTrack     = 0x2F
)

// percussionChannel is MIDI channel 10 as musicians count it, channel index 9
// as every status byte's low nibble carries it. Nothing on it is playable on
// a guitar, so its notes never reach the score.
const percussionChannel = 9

// parseTrack walks one MTrk chunk's event stream and reports its notes and
// metadata. Tempo and time-signature meta events are appended to the
// song-wide maps as they are found, because the SMF format allows either to
// appear on any track, not only a conventional "track 0".
func parseTrack(data []byte, tempo *[]tempoChange, sig *[]sigChange) (parsedTrack, error) {
	r := &reader{data: data}
	var pt parsedTrack
	var tick int64
	var runningStatus byte
	// active tracks still-sounding Note Ons, keyed by channel and pitch so
	// two channels (or two pitches) can overlap without colliding.
	active := map[uint16]int64{}

	for r.remaining() > 0 {
		delta, err := r.readVLQ()
		if err != nil {
			return pt, fmt.Errorf("delta-time: %w", err)
		}
		tick += int64(delta)

		first, err := r.peekByte()
		if err != nil {
			return pt, fmt.Errorf("event status: %w", err)
		}

		var status byte
		if first&0x80 != 0 {
			status = first
			r.pos++
			// Running status only ever carries a channel message forward:
			// meta and sysex events are one-offs and must not leak their
			// "status" into whatever data byte happens to follow them.
			if status < 0xF0 {
				runningStatus = status
			} else {
				runningStatus = 0
			}
		} else {
			if runningStatus == 0 {
				return pt, fmt.Errorf("data byte 0x%02X with no running status in effect", first)
			}
			status = runningStatus
			// first is the message's first data byte, not a status byte:
			// leave it unconsumed so the channel-message branch reads it.
		}

		switch {
		case status == 0xFF:
			ended, err := parseMeta(r, &pt, tempo, sig, tick)
			if err != nil {
				return pt, err
			}
			if ended {
				closeDangling(&pt, active, tick)
				return pt, nil
			}
		case status == 0xF0 || status == 0xF7:
			length, err := r.readVLQ()
			if err != nil {
				return pt, fmt.Errorf("sysex length: %w", err)
			}
			if _, err := r.readBytes(int(length)); err != nil {
				return pt, fmt.Errorf("sysex data: %w", err)
			}
		case status >= 0x80 && status <= 0xEF:
			if err := parseChannelMessage(r, status, tick, active, &pt); err != nil {
				return pt, err
			}
		default:
			// System common/realtime bytes (0xF1-0xF6, 0xF8-0xFE) have no
			// business in an SMF track; there is no safe length to skip by.
			return pt, fmt.Errorf("unsupported status byte 0x%02X", status)
		}
	}
	closeDangling(&pt, active, tick)
	return pt, nil
}

// closeDangling ends every note still sounding when the track's event stream
// stops, at endTick — the End of Track tick, or the last event's tick when the
// chunk simply runs out of bytes.
//
// This used to be a silent drop: the active map went out of scope with the
// function and whatever was in it was gone. A file whose last note never gets
// its Note Off is common enough (a truncated export, a sequencer killed
// mid-write, a writer that leans on End of Track to cut everything off) that
// losing the note without a word is the wrong answer. We know where it
// started and we know where the track stops; that is a note.
//
// Map iteration order is random, so the survivors are ordered before they are
// appended. Two parses of the same bytes have to produce the same score.
func closeDangling(pt *parsedTrack, active map[uint16]int64, endTick int64) {
	if len(active) == 0 {
		return
	}
	keys := make([]uint16, 0, len(active))
	for k := range active {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if active[keys[i]] != active[keys[j]] {
			return active[keys[i]] < active[keys[j]]
		}
		return keys[i] < keys[j]
	})
	for _, k := range keys {
		start := active[k]
		delete(active, k)
		// A Note On on the very last tick has nowhere to sound; a zero-length
		// note is dropped here exactly as handleNoteEvent drops one.
		if endTick > start {
			pt.notes = append(pt.notes, rawNote{startTick: start, endTick: endTick, midi: uint8(k)})
		}
	}
}

// parseMeta reads one 0xFF meta event, already past its status byte, and
// folds it into pt or the song-wide tempo/time-signature maps. ended reports
// an End of Track event, which is where the track's byte stream must stop
// even if the chunk's declared length leaves bytes unread.
func parseMeta(r *reader, pt *parsedTrack, tempo *[]tempoChange, sig *[]sigChange, tick int64) (ended bool, err error) {
	metaType, err := r.readByte()
	if err != nil {
		return false, fmt.Errorf("meta type: %w", err)
	}
	length, err := r.readVLQ()
	if err != nil {
		return false, fmt.Errorf("meta length: %w", err)
	}
	payload, err := r.readBytes(int(length))
	if err != nil {
		return false, fmt.Errorf("meta payload: %w", err)
	}

	switch metaType {
	case metaTempo:
		// 3 bytes, microseconds per quarter note. A zero value is
		// meaningless (infinite BPM) and would poison every conversion
		// downstream, so it is treated as absent rather than honoured.
		if len(payload) >= 3 {
			micros := uint32(payload[0])<<16 | uint32(payload[1])<<8 | uint32(payload[2])
			if micros > 0 {
				*tempo = append(*tempo, tempoChange{tick: tick, micros: micros})
			}
		}
	case metaTimeSignature:
		// numerator, denominator as a power of two, clocks/click, 32nds/quarter.
		// Only the first two bytes carry anything the grid needs.
		if len(payload) >= 2 {
			beats := int(payload[0])
			unit := 1 << payload[1]
			if beats > 0 && unit > 0 {
				*sig = append(*sig, sigChange{tick: tick, sig: practice.TimeSignature{Beats: beats, Unit: unit}})
			}
		}
	case metaTrackName:
		if pt.name == "" {
			pt.name = string(payload)
		}
	case metaInstrumentName:
		if pt.instrument == "" {
			pt.instrument = string(payload)
		}
	case metaText:
		if pt.text == "" {
			pt.text = string(payload)
		}
	case metaEndOfTrack:
		return true, nil
	}
	return false, nil
}

// parseChannelMessage reads the data bytes of one channel-voice message
// (status already consumed by the caller, which may have been running
// status rather than a byte just read) and updates note tracking or program
// state accordingly.
func parseChannelMessage(r *reader, status byte, tick int64, active map[uint16]int64, pt *parsedTrack) error {
	hi := status & 0xF0
	channel := status & 0x0F

	switch hi {
	case 0x80, 0x90, 0xA0, 0xB0, 0xE0:
		// Note Off, Note On, Polyphonic Key Pressure, Control Change and
		// Pitch Bend all carry exactly two data bytes.
		b1, err := r.readByte()
		if err != nil {
			return fmt.Errorf("channel message data: %w", err)
		}
		b2, err := r.readByte()
		if err != nil {
			return fmt.Errorf("channel message data: %w", err)
		}
		if hi == 0x80 || hi == 0x90 {
			handleNoteEvent(hi, channel, b1, b2, tick, active, pt)
		}
	case 0xC0:
		program, err := r.readByte()
		if err != nil {
			return fmt.Errorf("program change data: %w", err)
		}
		if !pt.hasProgram {
			pt.firstProgram, pt.hasProgram = program, true
		}
	case 0xD0:
		if _, err := r.readByte(); err != nil {
			return fmt.Errorf("channel pressure data: %w", err)
		}
	}
	return nil
}

// handleNoteEvent turns a Note On/Note Off pair into a rawNote. A Note On
// with velocity 0 is defined by the SMF spec to mean Note Off — many writers
// use it exclusively, to keep every event in a track using running status
// under a single 0x9n byte.
func handleNoteEvent(hi, channel, pitch, velocity byte, tick int64, active map[uint16]int64, pt *parsedTrack) {
	if channel == percussionChannel {
		return
	}
	key := uint16(channel)<<8 | uint16(pitch)
	if hi == 0x90 && velocity > 0 {
		// A retrigger without an intervening Note Off simply restarts the
		// note rather than stacking a second one nothing will ever close.
		active[key] = tick
		return
	}
	if start, ok := active[key]; ok {
		delete(active, key)
		if tick > start {
			pt.notes = append(pt.notes, rawNote{startTick: start, endTick: tick, midi: pitch})
		}
	}
	// A Note Off with no matching Note On has nothing to close; ignore it.
}
