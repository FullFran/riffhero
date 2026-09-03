package midi

import "fmt"

// parseHeader reads the mandatory MThd chunk and returns the format (0, 1 or
// 2) and the division word untouched — newTickClock is what knows how to
// read a division word, since interpreting it depends on nothing else here.
func parseHeader(r *reader) (format uint16, division uint16, err error) {
	magic, err := r.readBytes(4)
	if err != nil {
		return 0, 0, fmt.Errorf("midi: truncated header: %w", err)
	}
	if string(magic) != "MThd" {
		return 0, 0, fmt.Errorf("midi: not a Standard MIDI File (bad MThd magic)")
	}

	length, err := r.readUint32()
	if err != nil {
		return 0, 0, fmt.Errorf("midi: truncated header: %w", err)
	}
	if length < 6 {
		return 0, 0, fmt.Errorf("midi: header chunk too short (%d bytes)", length)
	}

	format, err = r.readUint16()
	if err != nil {
		return 0, 0, fmt.Errorf("midi: truncated header: %w", err)
	}
	// ntrks is informational only: the MTrk chunks that actually follow, not
	// this count, decide how many tracks we parse.
	if _, err = r.readUint16(); err != nil {
		return 0, 0, fmt.Errorf("midi: truncated header: %w", err)
	}
	division, err = r.readUint16()
	if err != nil {
		return 0, 0, fmt.Errorf("midi: truncated header: %w", err)
	}
	if format > 2 {
		return 0, 0, fmt.Errorf("midi: unsupported SMF format %d", format)
	}

	// Some writers pad the header chunk past the standard 6 content bytes for
	// future compatibility; the spec says to skip whatever is left rather
	// than treat it as a new chunk.
	if extra := int(length) - 6; extra > 0 {
		if _, err := r.readBytes(extra); err != nil {
			return 0, 0, fmt.Errorf("midi: truncated header: %w", err)
		}
	}
	return format, division, nil
}
