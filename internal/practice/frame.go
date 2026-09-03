package practice

// Frame is an absolute sample-frame position in the practice timeline.
type Frame int64

// Clock converts between sample frames and seconds.
type Clock struct {
	SampleRate int
}

func (c Clock) Seconds(f Frame) float64 {
	if c.SampleRate <= 0 {
		return 0
	}
	return float64(f) / float64(c.SampleRate)
}

func (c Clock) Frames(seconds float64) Frame {
	if c.SampleRate <= 0 || seconds <= 0 {
		return 0
	}
	return Frame(seconds * float64(c.SampleRate))
}
