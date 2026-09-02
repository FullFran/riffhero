package ui

import (
	"fmt"
	"strings"

	"github.com/FullFran/riffhero/internal/practice"
)

// HUDInput is everything the heads-up display is built from. It is a plain
// value type so the whole HUD can be built and asserted on without a window.
type HUDInput struct {
	Clock  practice.Clock
	Grid   practice.Grid
	Tuning practice.Tuning

	Title  string
	Artist string
	Track  string

	Position practice.Frame
	End      practice.Frame
	Playing  bool
	Finished bool
	Speed    float64
	Loop     practice.Loop
	Adaptive bool

	Stats    practice.SessionStats
	LastLap  practice.SessionStats
	HasLap   bool
	Rating   practice.Rating
	HasRatng bool

	// Live is set when a real guitar is being listened to rather than a
	// scripted stand-in, and Backing when there is a track playing.
	Live    bool
	Backing bool

	Level       float64 // dBFS
	Present     bool
	Detected    practice.DetectedNote
	HasDetected bool

	Dropped   uint64
	Underruns uint64
	Latency   practice.Frame
	Device    string

	// Calibrated says the latency was measured rather than guessed from the
	// device's buffer sizes. The two are different claims and the second one
	// still needs the warning.
	Calibrated bool
}

// HUD is the formatted display, one string per region of the screen.
type HUD struct {
	Title     string
	Transport string
	Score     string
	Practice  string
	Input     string
	Rating    string

	// Warnings are the things the player must be told rather than left to
	// guess at. Dropped samples look exactly like missed notes from the
	// player's side of the screen, and silence looks exactly like a guitar
	// that is not plugged in.
	Warnings []string
}

// BuildHUD formats the display.
func BuildHUD(in HUDInput) HUD {
	var h HUD

	h.Title = strings.TrimSpace(in.Title)
	if in.Artist != "" {
		h.Title += " — " + in.Artist
	}
	if in.Track != "" {
		h.Title += "   [" + in.Track + "]"
	}
	if in.Tuning.Name != "" {
		h.Title += "   " + in.Tuning.Name
		if in.Tuning.Capo > 0 {
			h.Title += fmt.Sprintf(" capo %d", in.Tuning.Capo)
		}
	}
	if h.Title == "" {
		h.Title = "RiffHero"
	}

	h.Transport = fmt.Sprintf("%-8s %s / %s   %s",
		transportState(in),
		Timecode(in.Clock, in.Position),
		Timecode(in.Clock, in.End),
		barBeat(in),
	)

	st := in.Stats
	h.Score = fmt.Sprintf("accuracy %3.0f%%   combo x%-3d best x%-3d   perfect %-3d good %-3d miss %-3d   %d/%d",
		st.Accuracy*100, st.Combo, st.MaxCombo, st.Perfect, st.Good, st.Miss, st.Resolved, st.Total)

	h.Practice = fmt.Sprintf("speed %s   loop %s   progressive %s   backing %s",
		SpeedLabel(in.Speed), loopLabel(in), onOff(in.Adaptive), onOff(in.Backing))
	if in.HasLap {
		h.Practice += fmt.Sprintf("   last lap %.0f%%", in.LastLap.Accuracy*100)
	}

	h.Input = inputLine(in)

	if in.HasRatng {
		h.Rating = RatingLabel(in.Rating)
	}

	h.Warnings = warnings(in)
	return h
}

func transportState(in HUDInput) string {
	switch {
	case in.Playing:
		return "PLAYING"
	case in.Finished:
		return "FINISHED"
	default:
		return "PAUSED"
	}
}

func barBeat(in HUDInput) string {
	pos := in.Grid.Locate(in.Position)
	if !pos.Valid {
		return "bar -"
	}
	return fmt.Sprintf("bar %d.%d", pos.Bar, pos.Beat)
}

func loopLabel(in HUDInput) string {
	l := in.Loop
	if !l.Active() {
		if l.A > 0 || l.B > 0 {
			return "armed"
		}
		return "off"
	}
	// Bars read better than seconds: a player thinks "loop bars 9 to 12", not
	// "loop 17.3 s to 24.1 s".
	from, to := in.Grid.BarAt(l.A), in.Grid.BarAt(l.B-1)
	if from >= 0 && to >= 0 {
		return fmt.Sprintf("bars %d-%d", in.Grid[from].Number, in.Grid[to].Number)
	}
	return fmt.Sprintf("%s-%s", Timecode(in.Clock, l.A), Timecode(in.Clock, l.B))
}

func inputLine(in HUDInput) string {
	if !in.Live {
		return "input   scripted performance (no guitar)"
	}

	// A pitch is only shown while a string is actually sounding. Leaving the
	// last note up after it has died reads as a detector stuck on it, which is
	// the one thing a player would then go looking for.
	pitch := "—"
	if in.HasDetected && in.Present {
		pitch = fmt.Sprintf("%s %s (%.0f%%)",
			NoteName(in.Detected.MIDI), Cents(in.Detected.CentsError), in.Detected.Confidence*100)
	}
	line := fmt.Sprintf("input  [%s] %5.1f dB   %s", Bar(MeterLevel(in.Level), 16), in.Level, pitch)
	if in.Device != "" {
		line += "   " + in.Device
	}
	if in.Latency > 0 {
		line += fmt.Sprintf("   latency %.0f ms", in.Clock.Seconds(in.Latency)*1000)
		if !in.Calibrated {
			line += " (estimated)"
		}
	}
	return line
}

func warnings(in HUDInput) []string {
	var out []string
	if in.Dropped > 0 {
		// A dropped sample is a hole in the timeline, not just lost audio, and
		// from the player's side it is indistinguishable from their own
		// mistake. Saying so is the only honest option.
		out = append(out, fmt.Sprintf("dropped %d input samples — the analysis is falling behind", in.Dropped))
	}
	if in.Underruns > 0 {
		out = append(out, fmt.Sprintf("%d audio underruns — the backing track stuttered", in.Underruns))
	}
	if in.Live && !in.Calibrated {
		out = append(out, "latency not calibrated — run with --calibrate")
	}
	return out
}

func onOff(v bool) string {
	if v {
		return "on"
	}
	return "off"
}
