package midi

import "fmt"

// gmGuitarPrograms names the General MIDI guitar family: programs 24-31, the
// values actually carried by a Program Change event (General MIDI numbers
// them 1-128 in the spec text but writes 0-127 on the wire). That is the
// range RiffHero's users overwhelmingly care about; every other program gets
// a generic label instead of a 128-entry table nobody asked for.
var gmGuitarPrograms = map[byte]string{
	24: "Acoustic Guitar (nylon)",
	25: "Acoustic Guitar (steel)",
	26: "Electric Guitar (jazz)",
	27: "Electric Guitar (clean)",
	28: "Electric Guitar (muted)",
	29: "Overdriven Guitar",
	30: "Distortion Guitar",
	31: "Guitar Harmonics",
}

func gmProgramName(program byte) string {
	if name, ok := gmGuitarPrograms[program]; ok {
		return name
	}
	return fmt.Sprintf("program %d", program)
}
