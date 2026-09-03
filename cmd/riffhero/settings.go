package main

import (
	"fmt"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/FullFran/riffhero/internal/audio"
	"github.com/FullFran/riffhero/internal/config"
	"github.com/FullFran/riffhero/internal/practice"
	"github.com/FullFran/riffhero/internal/ui"
)

// Everything that used to be a command-line flag, reachable while the app is
// running — because the flags are set before the guitar is plugged in and the
// interesting ones only become answerable afterwards.

type settingsState struct {
	kind   audio.Kind // which list the device picker is showing
	cursor int
}

type settingKind uint8

const (
	settingAction settingKind = iota
	settingStep
	settingToggle
)

// settingRow is one line of the settings screen. Laying them out as data
// rather than as code is what keeps the update and the draw agreeing about
// what is on the screen and where.
type settingRow struct {
	label string
	key   string
	kind  settingKind

	value func(*app) string
	note  func(*app) string
	off   func(*app) bool

	less  func(*app)
	more  func(*app)
	atMin func(*app) bool
	atMax func(*app) bool

	on func(*app) bool
	do func(*app)
}

const (
	volumeStep  = 0.05
	monitorStep = 0.05
	speedStep   = 0.05
	latencyStep = 0.005 // five milliseconds, about half a device period
)

var settingRows = []settingRow{
	{
		label: "INPUT DEVICE", key: "1", kind: settingAction,
		value: func(a *app) string {
			if a.det == nil {
				return "none"
			}
			return deviceOrDefault(a.input)
		},
		note: func(a *app) string { return "where the guitar is plugged in" },
		off:  func(a *app) bool { return a.host == nil },
		do:   func(a *app) { a.openDevices(audio.Input) },
	},
	{
		label: "OUTPUT DEVICE", key: "2", kind: settingAction,
		value: func(a *app) string { return deviceOrDefault(a.output) },
		note:  func(a *app) string { return "where the backing track comes out" },
		off:   func(a *app) bool { return a.host == nil },
		do:    func(a *app) { a.openDevices(audio.Output) },
	},
	{
		label: "LATENCY", key: "3", kind: settingStep,
		value: func(a *app) string { return a.latencySummary() },
		note: func(a *app) string {
			return "how long the round trip takes; measure it rather than guess"
		},
		off:   func(a *app) bool { return a.det == nil },
		less:  func(a *app) { a.nudgeLatency(-latencyStep) },
		more:  func(a *app) { a.nudgeLatency(latencyStep) },
		atMin: func(a *app) bool { return a.det == nil || a.det.LatencyOffset <= 0 },
		atMax: func(a *app) bool { return a.det == nil },
	},
	{
		label: "MEASURE LATENCY", key: "4", kind: settingAction,
		value: func(a *app) string { return "run the click test" },
		off:   func(a *app) bool { return a.host == nil },
		do:    func(a *app) { a.openCalibration() },
	},
	{
		label: "READING", key: "5", kind: settingAction,
		value: func(a *app) string { return a.notation.Label() },
		note:  func(a *app) string { return "tablature, standard notation, or both at once" },
		do:    func(a *app) { a.setNotation(a.notation.Next()) },
	},
	{
		label: "ACCIDENTALS", key: "6", kind: settingAction,
		value: func(a *app) string {
			if a.spelling == ui.Flats {
				return "flats"
			}
			return "sharps"
		},
		note: func(a *app) string { return "how the black notes are named on the staff" },
		do: func(a *app) {
			if a.spelling == ui.Flats {
				a.spelling, a.cfg.Spelling = ui.Sharps, "sharps"
			} else {
				a.spelling, a.cfg.Spelling = ui.Flats, "flats"
			}
		},
	},
	{
		label: "BACKING VOLUME", key: "7", kind: settingStep,
		value: func(a *app) string { return percent(a.volumeSetting()) },
		off:   func(a *app) bool { return a.engine == nil },
		less:  func(a *app) { a.nudgeVolume(-volumeStep) },
		more:  func(a *app) { a.nudgeVolume(volumeStep) },
		atMin: func(a *app) bool { return a.engine == nil || a.engine.Volume() <= 0 },
		atMax: func(a *app) bool { return a.engine == nil || a.engine.Volume() >= 1 },
	},
	{
		label: "GUITAR MONITOR", key: "8", kind: settingStep,
		value: func(a *app) string { return percent(a.monitorSetting()) },
		note: func(a *app) string {
			return "mixes the guitar into the output; leave off if you have an amp"
		},
		off:   func(a *app) bool { return a.engine == nil },
		less:  func(a *app) { a.nudgeMonitor(-monitorStep) },
		more:  func(a *app) { a.nudgeMonitor(monitorStep) },
		atMin: func(a *app) bool { return a.engine == nil || a.engine.Monitor() <= 0 },
		atMax: func(a *app) bool { return a.engine == nil || a.engine.Monitor() >= 1 },
	},
	{
		label: "PRACTICE SPEED", key: "9", kind: settingStep,
		value: func(a *app) string { return ui.SpeedLabel(a.head.Speed()) },
		less:  func(a *app) { a.head.SetSpeed(a.head.Speed() - speedStep) },
		more:  func(a *app) { a.head.SetSpeed(a.head.Speed() + speedStep) },
		atMin: func(a *app) bool { return a.head.Speed() <= practice.SpeedMin },
		atMax: func(a *app) bool { return a.head.Speed() >= practice.SpeedMax },
	},
	{
		label: "TRACK", key: "", kind: settingStep,
		value: func(a *app) string {
			return fmt.Sprintf("%d/%d  %s", a.track+1, len(a.song.Tracks), a.song.Tracks[a.track].Name)
		},
		off:   func(a *app) bool { return len(a.song.Tracks) < 2 },
		less:  func(a *app) { a.useTrack(a.track - 1) },
		more:  func(a *app) { a.useTrack(a.track + 1) },
		atMin: func(a *app) bool { return a.track == 0 },
		atMax: func(a *app) bool { return a.track >= len(a.song.Tracks)-1 },
	},
	{
		label: "PROGRESSIVE PRACTICE", key: "P", kind: settingToggle,
		note: func(a *app) string { return "a clean lap of the loop speeds the next one up" },
		on:   func(a *app) bool { return a.runner.Adaptive() },
		do: func(a *app) {
			a.runner.SetAdaptive(!a.runner.Adaptive())
			a.cfg.Progressive = a.runner.Adaptive()
		},
	},
}

func (a *app) openSettings() { a.mode = inSettings }

const settingRowHeight = 44

func (a *app) settingGeometry(i int) (x, y, w float64) {
	x, w = 40, float64(a.width)-80
	return x, 118 + float64(i)*settingRowHeight, w
}

func (a *app) settingButtons() []button {
	out := make([]button, 0, len(settingRows))
	for i, r := range settingRows {
		x, y, w := a.settingGeometry(i)
		b := button{x: x, y: y, w: w, h: settingRowHeight - 8, key: r.key}
		if r.off != nil {
			b.off = r.off(a)
		}
		out = append(out, b)
	}
	return out
}

func (a *app) backButton() button {
	return button{x: 40, y: float64(a.height) - 62, w: 180, h: 44, label: "BACK", key: "ESC"}
}

func (a *app) updateSettings() {
	if inpututil.IsKeyJustPressed(ebiten.KeyEscape) || a.backButton().clicked() {
		a.openTitle()
		return
	}

	buttons := a.settingButtons()
	for i, r := range settingRows {
		if buttons[i].off {
			continue
		}
		hit := buttons[i].clicked()
		if r.key != "" && keyNamed(r.key) != ebiten.KeyMax && inpututil.IsKeyJustPressed(keyNamed(r.key)) {
			hit = true
		}

		switch r.kind {
		case settingStep:
			x, y, w := a.settingGeometry(i)
			s := stepper{x: x, y: y, w: w, h: settingRowHeight - 8}
			if r.atMin != nil {
				s.atMin = r.atMin(a)
			}
			if r.atMax != nil {
				s.atMax = r.atMax(a)
			}
			if s.minus().clicked() && r.less != nil {
				r.less(a)
			}
			if s.plus().clicked() && r.more != nil {
				r.more(a)
			}
			// The keyboard shortcut steps up; with shift it steps down, so one
			// key covers both ends without a second row of them.
			if hit && !s.minus().contains(ebiten.CursorPosition()) && !s.plus().contains(ebiten.CursorPosition()) {
				if ebiten.IsKeyPressed(ebiten.KeyShift) && r.less != nil {
					r.less(a)
				} else if r.more != nil {
					r.more(a)
				}
			}
		default:
			if hit && r.do != nil {
				r.do(a)
			}
		}
	}
}

func (a *app) drawSettings(screen *ebiten.Image) {
	screen.Fill(menuFace)
	drawHeading(screen, "SETTINGS", 40, 34)
	drawDim(screen, "click, or press the key on the left; SHIFT steps a value down", 40, 78)

	for i, r := range settingRows {
		x, y, w := a.settingGeometry(i)
		h := float64(settingRowHeight - 8)
		disabled := r.off != nil && r.off(a)

		switch r.kind {
		case settingStep:
			s := stepper{x: x, y: y, w: w, h: h, label: r.label, value: r.value(a)}
			if r.atMin != nil {
				s.atMin = disabled || r.atMin(a)
			}
			if r.atMax != nil {
				s.atMax = disabled || r.atMax(a)
			}
			s.draw(screen)
		case settingToggle:
			t := toggle{x: x, y: y, w: w, h: h, label: r.label, on: r.on(a), key: r.key}
			t.draw(screen)
		default:
			b := button{x: x, y: y, w: w, h: h, off: disabled, key: r.key}
			b.draw(screen)
			ink := menuInk
			if disabled {
				ink = buttonOffInk
			}
			drawTinted(screen, r.label, int(x)+30, int(y+(h-glyphH)/2), ink)
			drawTinted(screen, r.value(a), int(x+w-textWidth(r.value(a)))-14, int(y+(h-glyphH)/2), menuAccent)
		}

		if r.note != nil && !disabled {
			if note := r.note(a); note != "" {
				drawDim(screen, note, int(x)+30, int(y+h)+1)
			}
		}
	}

	a.backButton().draw(screen)
	if a.noticeTicks > 0 {
		drawTinted(screen, a.notice, 240, a.height-50, menuAccent)
	}
}

// ------------------------------------------------------------ device picker

func (a *app) openDevices(kind audio.Kind) {
	a.refreshDevices()
	a.settings.kind = kind
	a.mode = inDevices
}

func (a *app) deviceList() []audio.Device {
	if a.settings.kind == audio.Output {
		return a.outputs
	}
	return a.inputs
}

func (a *app) deviceRows() []row {
	devices := a.deviceList()
	current := a.input
	if a.settings.kind == audio.Output {
		current = a.output
	}

	out := make([]row, 0, len(devices)+2)
	x, w := 40.0, float64(a.width)-80
	y := 118.0
	add := func(label, note string, chosen bool) {
		out = append(out, row{x: x, y: y, w: w, h: browseRowHeight - 3, label: label, note: note, chosen: chosen})
		y += browseRowHeight
	}

	for i := range devices {
		note := ""
		if devices[i].IsDefault {
			note = "system default"
		}
		add(devices[i].Name, note, current != nil && current.ID() == devices[i].ID())
	}
	if a.settings.kind == audio.Input {
		add("NO DEVICE", "practise against the scripted performance", a.det == nil)
	}
	add("REFRESH", "look again — after plugging something in", false)
	return out
}

func (a *app) updateDevices() {
	if inpututil.IsKeyJustPressed(ebiten.KeyEscape) || a.backButton().clicked() {
		a.openSettings()
		return
	}

	rows := a.deviceRows()
	devices := a.deviceList()
	for i, r := range rows {
		if !r.clicked() {
			continue
		}
		switch {
		case i < len(devices):
			a.pickDevice(&devices[i])
		case i == len(rows)-1:
			a.refreshDevices()
			a.showNotice("device list refreshed")
		default:
			a.closeStream()
			a.showNotice("no input device — scripted performance")
		}
		return
	}
}

func (a *app) pickDevice(d *audio.Device) {
	var err error
	if a.settings.kind == audio.Output {
		err = a.useOutput(d)
	} else {
		err = a.useInput(d)
	}
	if err != nil {
		a.tell(d.Name+" would not open: "+err.Error(), func() { a.mode = inDevices })
		return
	}
	a.showNotice("using " + d.Name)
	a.openSettings()
}

func (a *app) drawDevices(screen *ebiten.Image) {
	screen.Fill(menuFace)
	heading := "INPUT DEVICE"
	if a.settings.kind == audio.Output {
		heading = "OUTPUT DEVICE"
	}
	drawHeading(screen, heading, 40, 34)
	drawDim(screen, "backend: "+a.backendName()+"    ESC back", 40, 78)

	rows := a.deviceRows()
	for _, r := range rows {
		r.draw(screen)
	}
	if len(a.deviceList()) == 0 {
		drawTinted(screen, "no devices — is anything plugged in?", 44, 124, menuBad)
	}
	a.backButton().draw(screen)
}

func (a *app) backendName() string {
	if a.host == nil {
		return "none"
	}
	return a.host.Backend()
}

// ------------------------------------------------------------ small helpers

func (a *app) setNotation(n config.Notation) {
	a.notation, a.cfg.Notation = n, n
	a.showNotice("reading: " + n.Label())
}

func (a *app) nudgeVolume(by float64) {
	if a.engine == nil {
		return
	}
	a.engine.SetVolume(a.engine.Volume() + by)
	a.cfg.Volume = a.engine.Volume()
}

func (a *app) nudgeMonitor(by float64) {
	if a.engine == nil {
		return
	}
	a.engine.SetMonitor(a.engine.Monitor() + by)
	a.cfg.Monitor = a.engine.Monitor()
}

func (a *app) nudgeLatency(bySeconds float64) {
	if a.det == nil {
		return
	}
	next := a.det.LatencyOffset + a.clock.Frames(bySeconds)
	if bySeconds < 0 {
		next = a.det.LatencyOffset - a.clock.Frames(-bySeconds)
	}
	if next < 0 {
		next = 0
	}
	a.det.LatencyOffset = next
	// A figure somebody typed in by hand is a measurement of a sort: they
	// nudged it until their playing scored straight, which is the same test
	// the click train runs.
	a.calibrated = true
	a.cfg.SetLatency(next, a.opts.sampleRate)
}

func (a *app) useTrack(i int) {
	if i < 0 || i >= len(a.song.Tracks) || i == a.track {
		return
	}
	a.track, a.cfg.Track = i, i
	a.buildRunner()
	a.runner.SetLoop(a.loop)
	a.showNotice("track: " + a.song.Tracks[i].Name)
}

func deviceOrDefault(d *audio.Device) string {
	if d == nil {
		return "system default"
	}
	return d.Name
}

func percent(v float64) string { return fmt.Sprintf("%.0f%%", v*100) }

// keyNamed maps the single-character shortcut printed on a button to the key
// that presses it. ebiten.KeyMax means there is none.
func keyNamed(name string) ebiten.Key {
	switch name {
	case "1":
		return ebiten.Key1
	case "2":
		return ebiten.Key2
	case "3":
		return ebiten.Key3
	case "4":
		return ebiten.Key4
	case "5":
		return ebiten.Key5
	case "6":
		return ebiten.Key6
	case "7":
		return ebiten.Key7
	case "8":
		return ebiten.Key8
	case "9":
		return ebiten.Key9
	case "P":
		return ebiten.KeyP
	}
	return ebiten.KeyMax
}
