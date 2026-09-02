package midi

import (
	"encoding/binary"
	"errors"
)

// errTruncated marks any read that ran off the end of the buffer. Every
// parsing function bottoms out here instead of indexing directly, which is
// what keeps a chopped-off file an error instead of a panic.
var errTruncated = errors.New("midi: unexpected end of data")

// reader is a bounds-checked cursor over an in-memory chunk of SMF bytes.
type reader struct {
	data []byte
	pos  int
}

func (r *reader) remaining() int { return len(r.data) - r.pos }

func (r *reader) peekByte() (byte, error) {
	if r.pos >= len(r.data) {
		return 0, errTruncated
	}
	return r.data[r.pos], nil
}

func (r *reader) readByte() (byte, error) {
	b, err := r.peekByte()
	if err != nil {
		return 0, err
	}
	r.pos++
	return b, nil
}

// readBytes returns the next n bytes and advances past them. The bound is
// computed as a subtraction rather than an addition so a huge or negative n
// (the kind a corrupted length field hands us) cannot overflow into a
// false-positive bounds check.
func (r *reader) readBytes(n int) ([]byte, error) {
	if n < 0 || n > len(r.data)-r.pos {
		return nil, errTruncated
	}
	b := r.data[r.pos : r.pos+n]
	r.pos += n
	return b, nil
}

func (r *reader) readUint16() (uint16, error) {
	b, err := r.readBytes(2)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(b), nil
}

func (r *reader) readUint32() (uint32, error) {
	b, err := r.readBytes(4)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(b), nil
}

// readVLQ reads a MIDI variable-length quantity: seven data bits per byte,
// most significant group first, continuation signalled by the top bit.
// Delta-times and meta/sysex payload lengths both use this encoding.
//
// A well-formed file never needs more than four bytes for one; capping the
// loop there turns a corrupt or hostile continuation stream (every byte
// setting the continuation bit) into an error instead of a read that never
// terminates on its own.
func (r *reader) readVLQ() (uint32, error) {
	var value uint32
	for i := 0; i < 4; i++ {
		b, err := r.readByte()
		if err != nil {
			return 0, err
		}
		value = value<<7 | uint32(b&0x7F)
		if b&0x80 == 0 {
			return value, nil
		}
	}
	return 0, errors.New("midi: variable-length quantity too long")
}
