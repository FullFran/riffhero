package practice

type Note struct {
	Start    Frame
	Duration Frame
	MIDI     uint8
	String   uint8
	Fret     uint8
}

type Event struct {
	Start Frame
	Notes []Note
}

type DetectedNote struct {
	Onset      Frame
	MIDI       uint8
	CentsError float64
	Confidence float64
}
