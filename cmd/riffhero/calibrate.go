package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/FullFran/riffhero/internal/audio"
	"github.com/FullFran/riffhero/internal/i18n"
)

// Measuring the round trip, from inside the app.
//
// It is the one number that cannot be derived, and getting it wrong does not
// degrade gracefully: it shifts every detection by a constant, so a player who
// is dead on time is told they are consistently late. Asking somebody to quit
// and re-run with a flag to fix that is asking them not to fix it.

type calibOutcome struct {
	result audio.Calibration
	err    error
}

type calibState struct {
	running bool
	done    bool
	result  audio.Calibration
	err     string
	ch      chan calibOutcome
	ticks   int
}

func (a *app) openCalibration() {
	// Whatever was playing has to stop. The measurement takes the device away
	// and gives it back, and a transport left running would advance across the
	// gap against audio nobody heard.
	a.head.Pause()
	a.calib = calibState{}
	a.mode = inCalibration
}

func (a *app) calibrationButtons() []button {
	w, h := 220.0, 48.0
	x := (float64(a.width) - w) / 2
	y := float64(a.height) - 150

	switch {
	case a.calib.running:
		return nil
	case a.calib.done && a.calib.err == "":
		return []button{
			{x: x - w/2 - 8, y: y, w: w, h: h, label: i18n.T("USE IT"), key: "ENTER"},
			{x: x + w/2 + 8, y: y, w: w, h: h, label: i18n.T("DISCARD"), key: "ESC"},
		}
	default:
		return []button{{x: x, y: y, w: w, h: h, label: i18n.T("START"), key: "ENTER"}}
	}
}

func (a *app) updateCalibration() {
	if a.calib.running {
		a.calib.ticks++
		select {
		case out := <-a.calib.ch:
			a.finishCalibration(out)
		default:
		}
		return
	}

	if inpututil.IsKeyJustPressed(ebiten.KeyEscape) || a.backButton().clicked() {
		a.openSettings()
		return
	}

	buttons := a.calibrationButtons()
	start := inpututil.IsKeyJustPressed(ebiten.KeyEnter)
	if len(buttons) > 0 && buttons[0].clicked() {
		start = true
	}

	switch {
	case a.calib.done && a.calib.err == "" && start:
		a.acceptCalibration()
	case len(buttons) > 1 && buttons[1].clicked():
		a.openSettings()
	case start && (!a.calib.done || a.calib.err != ""):
		// A failed measurement draws a START button, and it has to work: this
		// is the first-attempt path for a feature whose first attempt normally
		// fails, and an inert retry button leaves ESC as the only way out.
		a.startCalibration()
	}
}

// startCalibration hands the device over to the measurement.
//
// The stream is closed first. Two duplex devices at once is asking the backend
// for trouble, and more to the point the backing track would be playing over
// the very clicks the measurement is listening for.
func (a *app) startCalibration() {
	if a.host == nil {
		a.calib.done, a.calib.err = true, i18n.T("no audio backend")
		return
	}
	a.closeStream()

	in, out := a.input, a.output
	rate := a.opts.sampleRate
	host := a.host

	a.calib = calibState{running: true, ch: make(chan calibOutcome, 1)}
	ch := a.calib.ch

	go func() {
		result, err := audio.Calibrate(host, audio.CalibrationOptions{
			SampleRate: rate,
			Input:      in,
			Output:     out,
		})
		ch <- calibOutcome{result: result, err: err}
	}()
}

func (a *app) finishCalibration(out calibOutcome) {
	a.calib.running = false
	a.calib.done = true
	a.calib.result = out.result
	if out.err != nil {
		a.calib.err = calibrationMessage(out.err)
	}

	// The stream comes back either way: a failed measurement must not leave
	// somebody with no sound card.
	if err := a.openStream(); err != nil {
		a.warnings = append(a.warnings, i18n.Tf("could not reopen the device after calibrating: %s", err))
	}
}

// calibrationMessage says why a measurement was refused, in the player's
// language.
//
// The reason travels from the measurement as a value rather than only as a
// sentence, which is what makes this possible: each case is a whole line
// written here, not an English error string picked apart. Anything that is not
// a refusal — a device that would not open, say — keeps its own words.
func calibrationMessage(err error) string {
	var refused *audio.CalibrationError
	if !errors.As(err, &refused) {
		return err.Error()
	}
	switch refused.Reason {
	case audio.HeardNothing:
		return i18n.Tf(audio.HeardNothingText, refused.PeakDB)
	case audio.NotRecording:
		return i18n.Tf(audio.NotRecordingText, refused.Captured, refused.Wanted)
	case audio.ClicksNotFound:
		return i18n.Tf(audio.ClicksNotFoundText, refused.Confidence*100, refused.PeakDB)
	}
	return err.Error()
}

// waitForCalibration blocks until a measurement in flight has finished with
// the device.
//
// The measurement runs on its own goroutine and holds the audio host. Closing
// the app while it runs — the window's own close button is always there, and
// the screen deliberately ignores ESC — would free the context out from under
// it. The channel is buffered, so the goroutine cannot be left blocked on a
// send nobody is reading.
func (a *app) waitForCalibration() {
	if !a.calib.running {
		return
	}
	select {
	case <-a.calib.ch:
	case <-time.After(15 * time.Second):
		// Long past anything the measurement should take. Carrying on is worse
		// than leaking a goroutine that is about to end anyway.
	}
	a.calib.running = false
}

func (a *app) acceptCalibration() {
	r := a.calib.result
	a.cfg.SetLatency(r.Frames, r.SampleRate)
	if a.det != nil {
		a.det.LatencyOffset = a.cfg.Latency(a.opts.sampleRate)
	}
	a.calibrated = true
	a.showNotice(i18n.Tf("latency %.1f ms", r.Millis))
	a.openSettings()
}

// calibrationHelp is worked out at draw time, not at init: a package-level var
// would be built once, before the language is known, and stay in English for
// the rest of the run.
func calibrationHelp() []string {
	return []string{
		i18n.T("RiffHero plays a train of clicks and listens for them coming back. The gap is the round trip: your output buffer, the converter, the cable or the air, and the input buffer."),
		"",
		i18n.T("The clean way is a loop: run a cable from the headphone or line output back into an input, or pick your card's own monitor as the input. Played out loud into a microphone it also works, less precisely, and includes the speaker and the room along with the buffers."),
		"",
		i18n.T("It takes about five seconds. Keep the room quiet."),
	}
}

func (a *app) drawCalibration(screen *ebiten.Image) {
	screen.Fill(menuFace)
	drawHeading(screen, i18n.T("MEASURE LATENCY"), 40, 34)

	var help []string
	for _, paragraph := range calibrationHelp() {
		if paragraph == "" {
			help = append(help, "")
			continue
		}
		help = append(help, wrapText(paragraph, (a.width-80)/glyphW)...)
	}
	for i, line := range help {
		drawDim(screen, line, 40, 92+i*lineH)
	}

	y := 92 + len(help)*lineH + 24
	drawDim(screen, fmt.Sprintf("out: %s", deviceOrDefault(a.output)), 40, y)
	drawDim(screen, fmt.Sprintf("in:  %s", deviceOrDefault(a.input)), 40, y+lineH)

	switch {
	case a.calib.running:
		a.drawCalibrationProgress(screen, y+56)
	case a.calib.done && a.calib.err != "":
		drawTinted(screen, i18n.T("could not measure it"), 40, y+56, menuBad)
		for i, line := range wrapText(a.calib.err, (a.width-80)/glyphW) {
			drawDim(screen, line, 40, y+56+(i+1)*lineH)
		}
	case a.calib.done:
		a.drawCalibrationResult(screen, y+56)
	}

	for _, b := range a.calibrationButtons() {
		b.draw(screen)
	}
	if !a.calib.running {
		a.backButton().draw(screen)
	}
}

func (a *app) drawCalibrationProgress(screen *ebiten.Image, y int) {
	writeAt(screen, i18n.T("listening..."), 40, y)
	// The measurement takes about five seconds; the bar is honest about that
	// rather than pretending to know how far along it is.
	const expected = 60 * 6
	drawMeter(screen, 40, float64(y)+22, float64(a.width)-80, 8,
		float64(a.calib.ticks)/expected, menuAccent)
}

func (a *app) drawCalibrationResult(screen *ebiten.Image, y int) {
	r := a.calib.result
	drawScaled(screen, fmt.Sprintf("%.1f ms", r.Millis), 40, y, 2, menuGood)
	drawDim(screen, i18n.Tf("%d frames at %d Hz", r.Frames, r.SampleRate), 40, y+40)
	drawDim(screen, i18n.Tf("confidence %.0f%%, peak %.0f dBFS", r.Confidence*100, r.PeakDB), 40, y+40+lineH)

	if r.Confidence < 0.6 {
		drawTinted(screen, i18n.T("low confidence: try a loopback, or turn the output up"), 40, y+40+2*lineH, menuAccent)
	}
}
