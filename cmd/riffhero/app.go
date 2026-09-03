package main

import (
	"fmt"

	"github.com/FullFran/riffhero/internal/audio"
	"github.com/FullFran/riffhero/internal/audio/codec"
	"github.com/FullFran/riffhero/internal/config"
	"github.com/FullFran/riffhero/internal/dsp"
	"github.com/FullFran/riffhero/internal/practice"
	"github.com/FullFran/riffhero/internal/ui"
)

// tailSeconds is how long the timeline runs past the last note, so the final
// note has its whole window to be played in rather than the song ending on top
// of it.
const tailSeconds = 1.5

// app is the whole running program: the song, whatever is driving time, the
// detector, the scoreboard and the view.
type app struct {
	opts options
	cfg  config.Config

	clock       practice.Clock
	song        *practice.Song
	track       int
	scorePath   string
	backingPath string

	// head is what the rest of the app asks for the time. It is either the
	// audio engine's player, driven by the device callback, or a plain
	// Transport driven by the game loop — and nothing above this line can tell
	// which, which is the entire point of the Playhead interface.
	head      practice.Playhead
	transport *practice.Transport // set only when the game loop drives time

	// The host is opened once and kept for the life of the process. It cannot
	// be reopened: a failed ma_device_init leaves miniaudio unable to create
	// another context, and the next InitContext segfaults. Devices, on the
	// other hand, can be closed and opened again as often as the settings
	// screen likes — measured, including after one that failed to open.
	host    *audio.Host
	engine  *audio.Engine
	player  *audio.Player
	det     *dsp.Detector
	backing []float32

	// input and output are the endpoints in use; inputs and outputs are what
	// the host last reported, for the picker.
	input, output   *audio.Device
	inputs, outputs []audio.Device

	runner *practice.Runner

	// layout is the whole reading area and the timeline geometry both readings
	// share; tab is the same thing squeezed into whatever vertical space is
	// left once the staff has had its share.
	layout ui.Layout
	tab    ui.Layout
	staff  ui.StaffLayout

	width, height int

	quitting       bool
	backingCleared bool
	calibrated     bool
	loop           practice.Loop
	showHelp       bool
	lastNote       practice.DetectedNote
	hasNote        bool
	notice         string
	noticeTicks    int
	warnings       []string

	// The menu side: which screen is up, and the state each of them keeps.
	mode     mode
	asking   string
	onYes    func()
	onNo     func()
	browse   browseState
	settings settingsState
	calib    calibState
	notation config.Notation
	spelling ui.Spelling
	channel  audio.InputChannel
}

func build(o options, cfg config.Config) (*app, error) {
	clock := practice.Clock{SampleRate: o.sampleRate}
	o.scorePath = scoreToOpen(o, cfg)

	song, err := loadSong(o, clock)
	if err != nil {
		return nil, err
	}
	track := o.track
	if track < 0 && cfg.Track > 0 && cfg.Track < len(song.Tracks) && o.scorePath == cfg.Score {
		track = cfg.Track
	}
	if track < 0 || track >= len(song.Tracks) {
		if o.track >= 0 {
			return nil, fmt.Errorf("track %d does not exist; the score has %d", o.track, len(song.Tracks))
		}
		track = song.GuitarTrack()
	}
	if track < 0 {
		return nil, fmt.Errorf("%s has no playable notes", song.Title)
	}

	a := &app{
		opts:      o,
		cfg:       cfg,
		clock:     clock,
		song:      song,
		track:     track,
		scorePath: o.scorePath,
		width:     screenWidth,
		height:    screenHeight,
	}
	a.layout = ui.NewLayout(float64(a.width), float64(a.height), clock)

	a.notation = cfg.Notation
	if o.notation != "" {
		a.notation = config.Notation(o.notation)
	}
	if !a.notation.Valid() {
		a.notation = config.NotationTab
	}
	a.spelling = spellingFor(cfg.Spelling)
	a.channel = channelFor(cfg.InputChannel)

	// The transport is settled before the device opens, not after. The render
	// goroutine fills its whole ring within milliseconds of the stream
	// starting, so a speed or a loop applied afterwards leaves eighty-five
	// milliseconds of the wrong bar at the wrong speed queued up to play on
	// the first press of the space bar.
	a.startScripted()
	if err := a.applyPlayheadSettings(); err != nil {
		return nil, err
	}

	// A missing or busy sound card is not a reason to refuse to start. The
	// scripted path still shows the score, the transport and the scoring, and
	// the warning says exactly what was lost.
	if err := a.startAudio(); err != nil {
		a.warnings = append(a.warnings, "audio unavailable: "+err.Error())
	}
	if a.head == nil {
		a.startScripted()
	}
	a.buildRunner()
	return a, nil
}

// notes is the part being practised.
func (a *app) notes() []practice.Note { return a.song.Tracks[a.track].Notes }

// tuning is the tuning of that part.
func (a *app) tuning() practice.Tuning { return a.song.Tracks[a.track].Tuning }

// end is where the timeline stops: the later of the score and the backing
// track, plus a tail so the last note has its whole window.
func (a *app) end(backingFrames int) practice.Frame {
	end := a.song.End() + a.clock.Frames(tailSeconds)
	if f := practice.Frame(backingFrames); f > end {
		end = f
	}
	return end
}

// detector returns the note source: the real one when there is a device, and a
// scripted performance when there is not.
func (a *app) detector() practice.Detector {
	if a.det != nil {
		return a.det
	}
	return newReplayingDetector(practice.Perform(a.notes(), scriptedPerformance(a.clock, len(a.notes()))))
}

// replayingDetector is the stand-in guitarist, playing the section again every
// time round the loop.
//
// A ScriptedDetector emits each detection once and then goes quiet, so the
// second lap of an A-B region scored nothing — not because the scoring was
// wrong but because nobody was playing. A real player repeats the section,
// and the demo has to as well or it shows the opposite of what it is for.
type replayingDetector struct {
	inner *practice.ScriptedDetector
	last  practice.Frame
	begun bool
}

func newReplayingDetector(events []practice.DetectedNote) *replayingDetector {
	return &replayingDetector{inner: practice.NewScriptedDetector(events)}
}

func (d *replayingDetector) Poll(upTo practice.Frame) []practice.DetectedNote {
	// The playhead going backwards is a wrap or a seek — the same signal the
	// audio engine uses. Either way the performance starts again from there,
	// with everything before it thrown away rather than dumped at once.
	if d.begun && upTo < d.last {
		d.inner.Reset()
		if upTo > 0 {
			d.inner.Poll(upTo - 1)
		}
	}
	d.last, d.begun = upTo, true
	return d.inner.Poll(upTo)
}

func (a *app) buildRunner() {
	cfg := practice.RunnerConfig{
		Notes: a.notes(),
		Session: practice.SessionConfig{
			Windows: practice.TimingWindows{
				Perfect: a.clock.Frames(0.050),
				Good:    a.clock.Frames(0.110),
			},
			// A real guitar is never as in tune as a synthesized one, and a
			// bend or vibrato moves a held note by more than this. Thirty-five
			// cents is a third of a semitone: wrong notes are still wrong,
			// right ones are not punished for being played on a guitar.
			MaxCents: 35,
		},
		Progression: practice.DefaultProgression,
		Adaptive:    a.opts.adaptive || a.cfg.Progressive,
	}
	if a.det != nil {
		cfg.Expecter = a.det
	}
	a.runner = practice.NewRunner(a.head, a.detector(), cfg)
}

// applyPlayheadSettings puts the speed and the A-B region on the playhead.
//
// It runs before the device opens, and that ordering is the point. The render
// goroutine fills its whole output ring within milliseconds of the stream
// starting; a speed or a region applied afterwards leaves eighty-five
// milliseconds of the wrong bar at the wrong speed already queued to play on
// the first press of the space bar. openStream carries all of it across.
func (a *app) applyPlayheadSettings() error {
	speed := a.opts.speed
	if speed <= 0 {
		speed = a.cfg.Speed
	}
	if speed > 0 {
		a.head.SetSpeed(speed)
	}

	if a.opts.loop == "" {
		return nil
	}
	from, to, err := parseBarRange(a.opts.loop)
	if err != nil {
		return err
	}
	if len(a.song.Grid) == 0 {
		return fmt.Errorf("this score has no bar grid to loop over")
	}
	if to > len(a.song.Grid) {
		return fmt.Errorf("bar %d does not exist; the score has %d", to, len(a.song.Grid))
	}
	start, end := a.song.Grid.Span(from-1, to-1)
	a.loop = practice.Loop{A: start, B: end, Enabled: true}
	a.head.SetLoop(a.loop)
	a.head.Restart()
	return nil
}

func (a *app) loadBacking() ([]float32, error) {
	path := a.opts.backing
	if path == "" {
		path = a.cfg.Backing
	}
	if path == "" {
		return nil, nil
	}
	pcm, err := codec.DecodeFile(path)
	if err != nil {
		return nil, fmt.Errorf("backing track: %w", err)
	}
	a.backingPath = path
	return pcm.Conform(a.opts.sampleRate, 2).Data, nil
}

// latencyOffset resolves the round trip: the flag wins, then the stored
// measurement, then nothing.
func (a *app) latencyOffset() practice.Frame {
	if a.opts.latencyMS >= 0 {
		return a.clock.Frames(a.opts.latencyMS / 1000)
	}
	return a.cfg.Latency(a.opts.sampleRate)
}

func (a *app) volumeSetting() float64 {
	if a.opts.volume >= 0 {
		return a.opts.volume
	}
	return a.cfg.Volume
}

func (a *app) monitorSetting() float64 {
	if a.opts.monitor >= 0 {
		return a.opts.monitor
	}
	return a.cfg.Monitor
}

// note records something the player should see once, without it being an
// error.
func (a *app) note(text string) {
	if text != "" {
		a.warnings = append(a.warnings, text)
	}
}

// Close releases the device and the backend.
func (a *app) Close() {
	// A measurement in flight is holding the host. Freeing the context under
	// it is a use-after-free in C, and the window's close button is always
	// there even on the screen that ignores every key.
	a.waitForCalibration()
	a.closeStream()
	if a.host != nil {
		_ = a.host.Close()
		a.host = nil
	}
}

// startAudio brings the whole audio side up: the host, the endpoints, the
// backing track and a stream over all three.
func (a *app) startAudio() error {
	if a.opts.noAudio {
		return nil
	}
	if err := a.openHost(); err != nil {
		return err
	}
	if err := a.resolveInitialDevices(); err != nil {
		return err
	}

	backing, err := a.loadBacking()
	if err != nil {
		// A missing backing track is worth saying but not worth refusing over:
		// practising the part unaccompanied is still practising.
		a.warnings = append(a.warnings, err.Error())
	}
	a.backing = backing

	return a.openStream()
}

func channelFor(name string) audio.InputChannel {
	switch name {
	case "left":
		return audio.ChannelLeft
	case "right":
		return audio.ChannelRight
	default:
		return audio.ChannelMix
	}
}

func spellingFor(name string) ui.Spelling {
	if name == "flats" {
		return ui.Flats
	}
	return ui.Sharps
}

// persist writes back the settings worth remembering across sessions.
//
// Everything the settings screen can change is in here, because a setting that
// has to be found again every time is one somebody stops changing.
func (a *app) persist() error {
	cfg := a.cfg
	cfg.Speed = a.head.Speed()
	cfg.Notation = a.notation
	cfg.Track = a.track
	// Only overwrite a remembered path when this run actually had one. A run
	// with no sound card never loads the backing track, and writing the empty
	// result back would forget it — every other device-derived field here is
	// already guarded the same way.
	if a.scorePath != "" || a.opts.scorePath != "" {
		cfg.Score = a.scorePath
	}
	if a.backingPath != "" || a.backingCleared {
		cfg.Backing = a.backingPath
	}
	if a.runner != nil {
		cfg.Progressive = a.runner.Adaptive()
	}
	if a.input != nil {
		cfg.InputDevice = a.input.Name
	}
	if a.output != nil {
		cfg.OutputDevice = a.output.Name
	}
	if a.engine != nil {
		cfg.Volume = a.engine.Volume()
		cfg.Monitor = a.engine.Monitor()
	}
	return cfg.Save()
}

func backendList(o options, cfg config.Config) []string {
	if b := pick(o.backend, cfg.Backend); b != "" {
		return []string{b}
	}
	return nil
}

// scriptedPerformance is the stand-in guitarist: mostly accurate, sometimes
// late, and it drops one note in four so all three ratings are visible. It is
// what --no-audio plays, and what proves the scoring loop works on a machine
// with no sound card.
func scriptedPerformance(clock practice.Clock, n int) []practice.Deviation {
	plan := make([]practice.Deviation, n)
	for i := range plan {
		switch i % 4 {
		case 0:
			plan[i] = practice.Deviation{Offset: clock.Frames(0.012)}
		case 1:
			plan[i] = practice.Deviation{Offset: clock.Frames(0.085), Cents: 9}
		case 2:
			plan[i] = practice.Deviation{Skip: true}
		case 3:
			plan[i] = practice.Deviation{Offset: -clock.Frames(0.030), Cents: -11}
		}
	}
	return plan
}
