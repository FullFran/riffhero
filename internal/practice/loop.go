package practice

// Loop is the A-B practice region. It is a value type on purpose: the audio
// callback reads it as a snapshot, and stepping the playhead through it is a
// pure function, so the same rule drives the real-time thread and the tests.
type Loop struct {
	A, B    Frame
	Enabled bool
}

// Active reports whether the loop should influence playback at all.
func (l Loop) Active() bool { return l.Enabled && l.A >= 0 && l.B > l.A }

// Length is the number of frames one lap covers, or zero when inactive.
func (l Loop) Length() Frame {
	if !l.Active() {
		return 0
	}
	return l.B - l.A
}

// Contains reports whether f is inside the region.
func (l Loop) Contains(f Frame) bool { return l.Active() && f >= l.A && f < l.B }

// Step advances pos by n frames and returns the new position plus how many
// times the loop wrapped. An inactive loop just adds.
//
// The playhead is only pulled back once it reaches B, so a run that starts
// before A plays into the region normally and only then starts looping. n
// larger than the region is handled rather than assumed away: a stalled render
// thread can hand the callback more frames than one lap holds.
func (l Loop) Step(pos, n Frame) (next Frame, laps int) {
	next = pos + n
	if !l.Active() || n <= 0 || next < l.B {
		return next, 0
	}
	// Everything from B onwards folds back into [A,B).
	length := l.Length()
	over := next - l.B
	laps = 1 + int(over/length)
	return l.A + over%length, laps
}
