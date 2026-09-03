package practice

// Adjustment is what the progressive practice rule decided to do with the
// speed after a lap of the loop.
type Adjustment uint8

const (
	Repeat Adjustment = iota
	SpeedUp
	SlowDown
)

func (a Adjustment) String() string {
	switch a {
	case SpeedUp:
		return "faster"
	case SlowDown:
		return "slower"
	default:
		return "repeat"
	}
}

// Progression is the progressive practice rule from PLAN.md: play a section
// clean and it speeds up, struggle and it slows down, anything between and it
// repeats. Deliberately dumb — the point is that the player never has to stop
// and decide, not that the schedule is optimal.
type Progression struct {
	Min   float64
	Max   float64
	Step  float64
	Raise float64 // accuracy at or above which the speed goes up
	Lower float64 // accuracy below which it comes down
}

var DefaultProgression = Progression{
	Min:   SpeedMin,
	Max:   SpeedMax,
	Step:  0.05,
	Raise: 0.95,
	Lower: 0.75,
}

// Evaluate reports the speed to practise the next lap at, and why.
//
// A lap that resolved nothing is not evidence: it means the player paused or
// the section was empty, and treating that as a failure would drag the speed
// down while nobody was playing.
func (p Progression) Evaluate(speed float64, stats SessionStats) (float64, Adjustment) {
	if stats.Resolved == 0 {
		return speed, Repeat
	}
	acc := stats.Accuracy
	switch {
	case acc >= p.Raise:
		next := clampRange(speed+p.Step, p.Min, p.Max)
		if next > speed {
			return next, SpeedUp
		}
		return speed, Repeat
	case acc < p.Lower:
		next := clampRange(speed-p.Step, p.Min, p.Max)
		if next < speed {
			return next, SlowDown
		}
		return speed, Repeat
	default:
		return speed, Repeat
	}
}

func clampRange(v, lo, hi float64) float64 {
	switch {
	case v < lo:
		return lo
	case v > hi:
		return hi
	default:
		return v
	}
}
