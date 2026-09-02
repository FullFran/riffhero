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

	clock practice.Clock
	song  *practice.Song
	track int

	// head is what the rest of the app asks for the time. It is either the
	// audio engine's player, driven by the device callback, or a plain
	// Transport driven by the game loop — and nothing above this line can tell
	// which, which is the entire point of the Playhead interface.
	head      practice.Playhead
	transport *practice.Transport // set only when the game loop drives time

	host   *audio.Host
	engine *audio.Engine
	player *audio.Player
	det    *dsp.Detector

	runner *practice.Runner
	layout ui.Layout

	width, height int

	calibrated  bool
	loop        practice.Loop
	showHelp    bool
	lastNote    practice.DetectedNote
	hasNote     bool
	notice      string
	noticeTicks int
	warnings    []string
}

func build(o options, cfg config.Config) (*app, error) {
	clock := practice.Clock{SampleRate: o.sampleRate}

	song, err := loadSong(o, clock)
	if err != nil {
		return nil, err
	}
	track := o.track
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
		opts:   o,
		cfg:    cfg,
		clock:  clock,
		song:   song,
		track:  track,
		width:  screenWidth,
		height: screenHeight,
	}
	a.layout = ui.NewLayout(float64(a.width), float64(a.height), clock)

	if err := a.startAudio(); err != nil {
		// A missing or busy sound card is not a reason to refuse to start. The
		// scripted path still shows the score, the transport and the scoring,
		// and the warning says exactly what was lost.
		a.warnings = append(a.warnings, "audio unavailable: "+err.Error())
		a.stopAudio()
	}
	if a.head == nil {
		a.startScripted()
	}

	a.buildRunner()
	if err := a.applyInitialSettings(); err != nil {
		return nil, err
	}
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

// startAudio opens the device and everything hanging off it.
func (a *app) startAudio() error {
	if a.opts.noAudio {
		return nil
	}

	host, err := audio.OpenHost(backendList(a.opts, a.cfg))
	if err != nil {
		return err
	}
	a.host = host

	in, note, err := resolveDevice(host, audio.Input, a.opts.input, a.cfg.InputDevice)
	if err != nil {
		return err
	}
	a.note(note)

	var out *audio.Device
	playback := !a.opts.noBacking
	if playback {
		if out, note, err = resolveDevice(host, audio.Output, a.opts.output, a.cfg.OutputDevice); err != nil {
			return err
		}
		a.note(note)
	}

	backing, err := a.loadBacking()
	if err != nil {
		// A missing backing track is worth saying but not worth refusing over:
		// practising the part unaccompanied is still practising.
		a.warnings = append(a.warnings, err.Error())
	}

	a.player = audio.NewPlayer(a.clock, a.end(len(backing)/2))
	a.det = dsp.NewDetector(a.opts.sampleRate)
	a.det.LatencyOffset = a.latencyOffset()
	a.calibrated = a.det.LatencyOffset > 0

	engine, err := audio.Open(host, audio.Config{
		SampleRate: a.opts.sampleRate,
		Input:      in,
		Output:     out,
		Capture:    true,
		Playback:   playback,
		Backing:    backing,
		Volume:     a.volumeSetting(),
		Monitor:    a.monitorSetting(),
	}, a.player, a.det)
	if err != nil {
		return err
	}
	a.engine = engine

	if err := engine.Start(); err != nil {
		return err
	}

	// With no stored measurement, the device's own buffering is a far better
	// starting point than zero: it is a real lower bound on the round trip,
	// and the alternative is telling a player who is dead on time that they
	// are consistently early. It is not a substitute for measuring, and the
	// HUD goes on saying so.
	if !a.calibrated {
		a.det.LatencyOffset = engine.Latency()
	}

	a.head = a.player
	return nil
}

// startScripted is the no-hardware path: the game loop drives a Transport and
// a simulated player produces the note events.
func (a *app) startScripted() {
	a.transport = practice.NewTransport(a.clock, a.end(0))
	a.head = a.transport
}

func (a *app) stopAudio() {
	if a.engine != nil {
		a.engine.Close()
		a.engine = nil
	}
	if a.host != nil {
		_ = a.host.Close()
		a.host = nil
	}
	a.player = nil
	a.det = nil
	a.head = nil
}

// detector returns the note source: the real one when there is a device, and a
// scripted performance when there is not.
func (a *app) detector() practice.Detector {
	if a.det != nil {
		return a.det
	}
	return practice.NewScriptedDetector(practice.Perform(a.notes(), scriptedPerformance(a.clock, len(a.notes()))))
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
		Adaptive:    a.opts.adaptive,
	}
	if a.det != nil {
		cfg.Expecter = a.det
	}
	a.runner = practice.NewRunner(a.head, a.detector(), cfg)
}

func (a *app) applyInitialSettings() error {
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
	a.runner.SetLoop(a.loop)
	a.head.Restart()
	return nil
}

func (a *app) loadBacking() ([]float32, error) {
	if a.opts.backing == "" {
		return nil, nil
	}
	pcm, err := codec.DecodeFile(a.opts.backing)
	if err != nil {
		return nil, fmt.Errorf("backing track: %w", err)
	}
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

// live reports whether a real guitar is being listened to.
func (a *app) live() bool { return a.det != nil }

// Close releases the device.
func (a *app) Close() { a.stopAudio() }

// persist writes back the settings worth remembering across sessions.
func (a *app) persist() error {
	cfg := a.cfg
	cfg.Speed = a.head.Speed()
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
