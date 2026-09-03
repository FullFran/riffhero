package ui

import (
	"fmt"
	"strings"

	"github.com/FullFran/riffhero/internal/i18n"
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
		h.Title += " - " + in.Artist
	}
	if in.Track != "" {
		h.Title += "   [" + in.Track + "]"
	}
	if in.Tuning.Name != "" {
		h.Title += "   " + in.Tuning.Name
		if in.Tuning.Capo > 0 {
			h.Title += " " + i18n.Tf("capo %d", in.Tuning.Capo)
		}
	}
	if h.Title == "" {
		h.Title = "RiffHero"
	}

	// The state column is padded to the widest word in the language actually
	// in use. Eight was the widest English one, and "REPRODUCIENDO" is
	// thirteen: a width measured from one string and drawn with another shoves
	// the timecode out of its column every time the transport changes.
	h.Transport = fmt.Sprintf("%-*s %s / %s   %s",
		stateWidth(),
		transportState(in),
		Timecode(in.Clock, in.Position),
		Timecode(in.Clock, in.End),
		barBeat(in),
	)

	// Explicit argument indexes, because five of these verbs are the identical
	// %-3d: a translation that reorders "perfect good miss" would otherwise
	// print the right numbers under the wrong words, and no test could see it.
	st := in.Stats
	h.Score = i18n.Tf("accuracy %3.0[1]f%%   combo x%-3[2]d best x%-3[3]d   perfect %-3[4]d good %-3[5]d miss %-3[6]d   %[7]d/%[8]d",
		st.Accuracy*100, st.Combo, st.MaxCombo, st.Perfect, st.Good, st.Miss, st.Resolved, st.Total)

	h.Practice = i18n.Tf("speed %s   loop %s   progressive %s   backing %s",
		SpeedLabel(in.Speed), loopLabel(in), onOff(in.Adaptive), onOff(in.Backing))
	if in.HasLap {
		h.Practice += i18n.Tf("   last lap %.0f%%", in.LastLap.Accuracy*100)
	}

	h.Input = inputLine(in)

	if in.HasRatng {
		h.Rating = RatingLabel(in.Rating)
	}

	h.Warnings = warnings(in)
	return h
}

// stateWidth is the widest transport word in the current language, in runes.
func stateWidth() int {
	w := 0
	for _, s := range []string{i18n.T("PLAYING"), i18n.T("FINISHED"), i18n.T("PAUSED")} {
		if n := len([]rune(s)); n > w {
			w = n
		}
	}
	return w
}

func transportState(in HUDInput) string {
	switch {
	case in.Playing:
		return i18n.T("PLAYING")
	case in.Finished:
		return i18n.T("FINISHED")
	default:
		return i18n.T("PAUSED")
	}
}

func barBeat(in HUDInput) string {
	pos := in.Grid.Locate(in.Position)
	if !pos.Valid {
		return i18n.T("bar -")
	}
	return i18n.Tf("bar %d.%d", pos.Bar, pos.Beat)
}

func loopLabel(in HUDInput) string {
	l := in.Loop
	if !l.Active() {
		if l.A > 0 || l.B > 0 {
			return i18n.T("armed")
		}
		return i18n.T("off")
	}
	// Bars read better than seconds: a player thinks "loop bars 9 to 12", not
	// "loop 17.3 s to 24.1 s".
	from, to := in.Grid.BarAt(l.A), in.Grid.BarAt(l.B-1)
	if from >= 0 && to >= 0 {
		return i18n.Tf("bars %d-%d", in.Grid[from].Number, in.Grid[to].Number)
	}
	return fmt.Sprintf("%s-%s", Timecode(in.Clock, l.A), Timecode(in.Clock, l.B))
}

func inputLine(in HUDInput) string {
	if !in.Live {
		return i18n.T("input   scripted performance (no guitar)")
	}

	// A pitch is only shown while a string is actually sounding. Leaving the
	// last note up after it has died reads as a detector stuck on it, which is
	// the one thing a player would then go looking for.
	pitch := "-"
	if in.HasDetected && in.Present {
		pitch = fmt.Sprintf("%s %s (%.0f%%)",
			NoteName(in.Detected.MIDI), Cents(in.Detected.CentsError), in.Detected.Confidence*100)
	}
	line := i18n.Tf("input  [%s] %5.1f dB   %s", Bar(MeterLevel(in.Level), 16), in.Level, pitch)
	if in.Device != "" {
		line += "   " + in.Device
	}
	if in.Latency > 0 {
		ms := in.Clock.Seconds(in.Latency) * 1000
		if in.Calibrated {
			line += i18n.Tf("   latency %.0f ms", ms)
		} else {
			line += i18n.Tf("   latency %.0f ms (estimated)", ms)
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
		out = append(out, i18n.Tf(i18n.Plural(int(in.Dropped),
			"dropped %d input sample - the analysis is falling behind",
			"dropped %d input samples - the analysis is falling behind"), in.Dropped))
	}
	if in.Underruns > 0 {
		out = append(out, i18n.Tf(i18n.Plural(int(in.Underruns),
			"%d audio underrun - the backing track stuttered",
			"%d audio underruns - the backing track stuttered"), in.Underruns))
	}
	if in.Live && !in.Calibrated {
		out = append(out, i18n.T("latency not calibrated - measure it from the settings screen"))
	}
	return out
}

func onOff(v bool) string {
	if v {
		return i18n.T("on")
	}
	return i18n.T("off")
}
