package main

import (
	"fmt"
	"path/filepath"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/FullFran/riffhero/internal/library"
	"github.com/FullFran/riffhero/internal/ui"
)

// The screens the app is made of, and the shape they share: an open, an update
// and a draw each, with the app's mode saying which one is running.
//
// Every button also has a number key on it. RiffHero is used with a guitar in
// both hands, and a control that can only be reached with the mouse is one
// that has to be reached by putting the instrument down.

type mode int

const (
	atTitle mode = iota
	practising
	inSongs
	inSettings
	inDevices
	inCalibration
	asking
)

// menuColumn lays a stack of buttons down the middle of whatever size the
// window happens to be. A tiling desktop hands this a tall half-screen without
// being asked, so nothing here is a fixed coordinate.
func (a *app) menuColumn(count int) (x, w, top, pitch float64) {
	w = float64(a.width) * 0.62
	if w > 520 {
		w = 520
	}
	if w < 260 {
		w = 260
	}
	x = (float64(a.width) - w) / 2

	pitch = 62
	top = float64(a.height)*0.30 - float64(count)*pitch/2
	if min := 110.0; top < min {
		top = min
	}
	return x, w, top, pitch
}

// ask puts a question up. A question that always did the same thing would only
// be a notice, so tell is the one-answer version of it.
func (a *app) ask(text string, yes, no func()) {
	a.asking, a.onYes, a.onNo = text, yes, no
	a.mode = asking
}

func (a *app) tell(text string, then func()) { a.ask(text, then, nil) }

func (a *app) questionButtons() []button {
	w, h := 200.0, 52.0
	y := float64(a.height)/2 + 40
	if a.onNo == nil {
		return []button{{x: (float64(a.width) - w) / 2, y: y, w: w, h: h, label: "OK", key: "ENTER"}}
	}
	return []button{
		{x: float64(a.width)/2 - w - 8, y: y, w: w, h: h, label: "YES", key: "Y"},
		{x: float64(a.width)/2 + 8, y: y, w: w, h: h, label: "NO", key: "N"},
	}
}

func (a *app) updateAsking() {
	buttons := a.questionButtons()
	answer := func(f func()) {
		a.asking, a.onYes, a.onNo = "", nil, nil
		if f != nil {
			f()
		}
	}
	switch {
	case buttons[0].clicked(),
		inpututil.IsKeyJustPressed(ebiten.KeyEnter),
		a.onNo != nil && inpututil.IsKeyJustPressed(ebiten.KeyY):
		answer(a.onYes)
	case len(buttons) > 1 && buttons[1].clicked(),
		a.onNo != nil && (inpututil.IsKeyJustPressed(ebiten.KeyN) || inpututil.IsKeyJustPressed(ebiten.KeyEscape)):
		answer(a.onNo)
	}
}

func (a *app) drawAsking(screen *ebiten.Image) {
	screen.Fill(menuFace)
	drawPanel(screen, float64(a.width)*0.1, float64(a.height)/2-90, float64(a.width)*0.8, 200)
	for i, line := range wrapText(a.asking, (a.width-120)/glyphW) {
		writeAt(screen, line, int(float64(a.width)*0.1)+30, a.height/2-50+i*lineH)
	}
	for _, b := range a.questionButtons() {
		b.draw(screen)
	}
}

// ---------------------------------------------------------------- title

func (a *app) openTitle() {
	a.head.Pause()
	a.mode = atTitle
}

type titleRow struct {
	label string
	key   string
	note  func(*app) string
	off   func(*app) bool
	do    func(*app)
}

var titleRows = []titleRow{
	{
		label: "PLAY", key: "1",
		note: func(a *app) string { return a.song.Title },
		do:   func(a *app) { a.startPractice() },
	},
	{
		label: "CHOOSE A SONG", key: "2",
		note: func(a *app) string { return shortPath(a.scorePath) },
		do:   func(a *app) { a.openBrowser(library.Score) },
	},
	{
		label: "BACKING TRACK", key: "3",
		note: func(a *app) string {
			if a.backingPath == "" {
				return "none"
			}
			return shortPath(a.backingPath)
		},
		do: func(a *app) { a.openBrowser(library.Backing) },
	},
	{
		label: "SETTINGS", key: "4",
		note: func(a *app) string { return a.inputSummary() },
		do:   func(a *app) { a.openSettings() },
	},
	{
		label: "CALIBRATE LATENCY", key: "5",
		note: func(a *app) string { return a.latencySummary() },
		off:  func(a *app) bool { return a.host == nil },
		do:   func(a *app) { a.openCalibration() },
	},
	{
		label: "QUIT", key: "6",
		do: func(a *app) { a.quitting = true },
	},
}

func (a *app) titleButtons() []button {
	x, w, top, pitch := a.menuColumn(len(titleRows))
	out := make([]button, 0, len(titleRows))
	for i, r := range titleRows {
		b := button{x: x, y: top + float64(i)*pitch, w: w, h: 48, label: r.label, key: r.key}
		if r.off != nil {
			b.off = r.off(a)
		}
		if r.label == "QUIT" {
			b.danger = true
		}
		out = append(out, b)
	}
	return out
}

var numberKeys = []ebiten.Key{ebiten.Key1, ebiten.Key2, ebiten.Key3, ebiten.Key4, ebiten.Key5, ebiten.Key6, ebiten.Key7, ebiten.Key8, ebiten.Key9}

// pickedRow reports which of a stack of buttons was chosen, by mouse or by the
// number key drawn on it. -1 for none.
func pickedRow(buttons []button) int {
	for i, b := range buttons {
		if b.off {
			continue
		}
		if b.clicked() {
			return i
		}
		if i < len(numberKeys) && inpututil.IsKeyJustPressed(numberKeys[i]) {
			return i
		}
	}
	return -1
}

func (a *app) updateTitle() {
	if inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
		a.quitting = true
		return
	}
	if i := pickedRow(a.titleButtons()); i >= 0 {
		titleRows[i].do(a)
	}
}

func (a *app) drawTitle(screen *ebiten.Image) {
	screen.Fill(menuFace)
	drawHeading(screen, "RIFFHERO", 40, 40)
	drawDim(screen, "guitar practice — plug in, play, see what landed", 40, 76)

	buttons := a.titleButtons()
	for i, b := range buttons {
		b.draw(screen)
		if titleRows[i].note != nil {
			if note := titleRows[i].note(a); note != "" {
				drawDim(screen, clip(note, int(b.w/glyphW)), int(b.x)+2, int(b.y+b.h)+3)
			}
		}
	}

	a.drawTitleFooter(screen, buttons)
}

func (a *app) drawTitleFooter(screen *ebiten.Image, buttons []button) {
	y := a.height - 58
	for _, w := range a.warnings {
		drawTinted(screen, "! "+clip(w, (a.width-80)/glyphW), 40, y, menuBad)
		y -= lineH
	}

	last := buttons[len(buttons)-1]
	y = int(last.y+last.h) + 26
	if y > a.height-40 {
		y = a.height - 40
	}
	if a.det != nil {
		drawDim(screen, "input", 40, y)
		drawMeter(screen, 90, float64(y)+3, 140, 7, ui.MeterLevel(a.det.Level()), menuGood)
		drawDim(screen, fmt.Sprintf("%5.1f dB", a.det.Level()), 244, y)
	}
}

// startPractice leaves the menus and puts the playhead at the top of whatever
// is being practised.
func (a *app) startPractice() {
	a.mode = practising
	a.runner.Restart()
	a.head.Play()
}

func (a *app) inputSummary() string {
	switch {
	case a.opts.noAudio:
		return "no audio (scripted performance)"
	case a.det == nil:
		return "no device"
	case a.input != nil:
		return a.input.Name
	}
	return "default device"
}

func (a *app) latencySummary() string {
	if a.det == nil {
		return "needs a device"
	}
	ms := a.clock.Seconds(a.det.LatencyOffset) * 1000
	if !a.calibrated {
		return fmt.Sprintf("%.0f ms, estimated", ms)
	}
	return fmt.Sprintf("%.0f ms, measured", ms)
}

// shortPath is a file name with just enough of its directory to tell two songs
// with the same name apart.
func shortPath(path string) string {
	if path == "" {
		return "built-in phrase"
	}
	return filepath.Join(filepath.Base(filepath.Dir(path)), filepath.Base(path))
}

// wrapText breaks a line at spaces to fit a number of characters.
func wrapText(text string, cells int) []string {
	if cells < 8 {
		cells = 8
	}
	var out []string
	line := ""
	for _, word := range splitWords(text) {
		switch {
		case line == "":
			line = word
		case len(line)+1+len(word) <= cells:
			line += " " + word
		default:
			out = append(out, line)
			line = word
		}
	}
	if line != "" {
		out = append(out, line)
	}
	return out
}

func splitWords(text string) []string {
	var out []string
	start := -1
	for i, r := range text {
		if r == ' ' {
			if start >= 0 {
				out = append(out, text[start:i])
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, text[start:])
	}
	return out
}
