// Command riffhero is the practice app: load a score, plug in a guitar, play
// along with a backing track and see whether the pitch and the timing were
// right.
//
// Everything the app does is available without any of it: with no score it
// runs the built-in phrase, with no backing track it keeps the timeline on the
// same clock and plays silence, and with --no-audio it replays a scripted
// performance so the whole loop can be seen working on a machine with no sound
// card at all.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/FullFran/riffhero/internal/audio"
	"github.com/FullFran/riffhero/internal/buildinfo"
	"github.com/FullFran/riffhero/internal/config"
	"github.com/FullFran/riffhero/internal/i18n"
	"github.com/FullFran/riffhero/internal/practice"
	"github.com/FullFran/riffhero/internal/score"
	"github.com/FullFran/riffhero/internal/ui"
)

const (
	screenWidth  = 1280
	screenHeight = 720
)

type options struct {
	scorePath  string
	backing    string
	track      int
	input      string
	output     string
	backend    string
	noAudio    bool
	noBacking  bool
	monitor    float64
	volume     float64
	speed      float64
	latencyMS  float64
	adaptive   bool
	loop       string
	notation   string
	lang       string
	sampleRate int

	listDevices bool
	listTracks  bool
	calibrate   bool
	dryRun      bool
	version     bool
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "riffhero:", err)
		os.Exit(1)
	}
}

func run() error {
	// The language is settled first, before anything that produces a word.
	//
	// flag copies a description into the Flag struct at registration, so a
	// help text translated after parseFlags would already have been frozen in
	// English - and the config has to be read to know the language at all,
	// which is why the load moved up here rather than the Use call moving
	// down. --lang is read straight out of os.Args for the same reason: the
	// flag package cannot tell us about it until after it has taken the
	// descriptions.
	cfg, cfgErr := config.Load()
	if cfgErr != nil {
		cfg = config.Default()
	}
	lang := cfg.LanguageOrDetected(os.Getenv)
	if chosen, ok := langFromArgs(os.Args[1:]); ok {
		lang = chosen
	}
	i18n.Use(lang)

	if cfgErr != nil {
		// A broken config must not stop someone practising; say so and carry
		// on with the defaults rather than refusing to start.
		fmt.Fprintln(os.Stderr, "riffhero:", i18n.Tf("ignoring the stored settings: %v", cfgErr))
	}

	opts, err := parseFlags()
	if err != nil {
		return err
	}
	if opts.lang != "" && !i18n.Lang(opts.lang).Valid() {
		return errors.New(i18n.Tf("no language called %q; there is %s and %s",
			opts.lang, string(i18n.English), string(i18n.Spanish)))
	}

	switch {
	case opts.version:
		fmt.Println(buildinfo.String())
		return nil
	case opts.listDevices:
		return listDevices(opts, cfg)
	case opts.calibrate:
		return runCalibration(opts, cfg)
	case opts.listTracks:
		return listTracks(opts)
	case opts.dryRun:
		return dryRun(opts, cfg)
	}

	app, err := build(opts, cfg)
	if err != nil {
		return err
	}
	defer app.Close()

	ebiten.SetWindowSize(windowSize())
	ebiten.SetWindowTitle(windowTitle(app.song.Title))
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)

	if err := ebiten.RunGame(app); err != nil {
		return err
	}
	return app.persist()
}

// windowTitle is what the window manager puts in the bar. The em dash is safe
// here and nowhere else in the app: this string is drawn by the desktop's own
// font, not by Ebiten's 256-glyph one.
func windowTitle(song string) string {
	return "RiffHero — " + song
}

// windowSize is the preferred size, shrunk to fit the monitor. Asking for a
// window larger than the screen leaves the window manager to decide, and on a
// tiling desktop what it decides is rarely what was wanted.
func windowSize() (int, int) {
	w, h := screenWidth, screenHeight
	if m := ebiten.Monitor(); m != nil {
		mw, mh := m.Size()
		if mw > 0 && mh > 0 {
			if limit := mw * 9 / 10; w > limit {
				w = limit
			}
			if limit := mh * 9 / 10; h > limit {
				h = limit
			}
		}
	}
	return w, h
}

func parseFlags() (options, error) {
	var o options

	flag.StringVar(&o.backing, "backing", "", i18n.T("backing track to play along with (wav, mp3 or flac)"))
	flag.IntVar(&o.track, "track", -1, i18n.T("which track of the score to practise (default: the first guitar part)"))
	flag.StringVar(&o.input, "input", "", i18n.T("capture device, by name or id (default: the stored or system default)"))
	flag.StringVar(&o.output, "output", "", i18n.T("playback device, by name or id"))
	flag.StringVar(&o.backend, "backend", "", i18n.Tf("audio backend: %s, %s or %s", "jack", "pulseaudio", "alsa"))
	flag.BoolVar(&o.noAudio, "no-audio", false, i18n.T("run with no audio device, replaying a scripted performance"))
	flag.BoolVar(&o.noBacking, "no-playback", false, i18n.T("capture the guitar but open no output"))
	flag.Float64Var(&o.monitor, "monitor", -1, i18n.T("how much of the guitar to mix into the output, 0..1"))
	flag.Float64Var(&o.volume, "volume", -1, i18n.T("backing track level, 0..1"))
	flag.Float64Var(&o.speed, "speed", 0, i18n.T("initial practice speed, 0.25..1"))
	flag.Float64Var(&o.latencyMS, "latency", -1, i18n.T("round-trip latency in milliseconds, overriding the stored measurement"))
	flag.BoolVar(&o.adaptive, "progressive", false, i18n.T("start with the progressive practice rule switched on"))
	flag.StringVar(&o.loop, "loop", "", i18n.T("practise a bar range, e.g. 9-12"))
	flag.StringVar(&o.notation, "reading", "", i18n.Tf("what to show: %s, %s or %s", config.NotationTab, config.NotationStaff, config.NotationBoth))
	flag.IntVar(&o.sampleRate, "rate", 48000, i18n.T("sample rate to ask the device for"))
	flag.StringVar(&o.lang, "lang", "", i18n.Tf("language: %s or %s (default: the stored choice, else the desktop)", string(i18n.English), string(i18n.Spanish)))

	flag.BoolVar(&o.listDevices, "list-devices", false, i18n.T("print the audio devices and exit"))
	flag.BoolVar(&o.listTracks, "list-tracks", false, i18n.T("print the score's tracks and exit"))
	flag.BoolVar(&o.calibrate, "calibrate", false, i18n.T("measure the round-trip audio latency, store it and exit"))
	flag.BoolVar(&o.dryRun, "dry-run", false, i18n.T("run the practice loop with no window and no device, print the scoreboard and exit"))
	flag.BoolVar(&o.version, "version", false, i18n.T("print the version and exit"))

	flag.Usage = usage
	flag.Parse()

	// `riffhero song.gp --backing song.flac` is the documented invocation and
	// the one anyone would type, and Go's flag package stops parsing at the
	// first non-flag argument — so everything after the score would be
	// silently treated as more file names. Taking the score out and parsing
	// what is left puts the flags back in play wherever they were written.
	rest := flag.Args()
	if len(rest) > 0 {
		o.scorePath = rest[0]
		if err := flag.CommandLine.Parse(rest[1:]); err != nil {
			return o, err
		}
		if extra := flag.Args(); len(extra) > 0 {
			return o, fmt.Errorf(i18n.T("expected at most one score file, also got %q"), extra[0])
		}
	}
	return o, nil
}

// langFromArgs reads --lang before the flag package has run.
//
// It has to: every flag description is itself translated, and flag takes a
// copy of the string when the flag is registered, so by the time flag.Parse
// could report --lang the help text has already been written in the wrong
// language. Only the spellings the flag package itself accepts are honoured,
// so this cannot disagree with what --lang finally parses to.
func langFromArgs(args []string) (i18n.Lang, bool) {
	for i, arg := range args {
		name, value, hasValue := strings.Cut(arg, "=")
		if name != "-lang" && name != "--lang" {
			continue
		}
		if !hasValue {
			if i+1 >= len(args) {
				return "", false
			}
			value = args[i+1]
		}
		if l := i18n.Lang(value); l.Valid() {
			return l, true
		}
		return "", false
	}
	return "", false
}

// parseBarRange reads a "9-12" bar range. Bars are one-based, as a musician
// counts them, and the range includes both ends.
func parseBarRange(spec string) (from, to int, err error) {
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf(i18n.T("a loop is written as first-last, for example 9-12; got %q"), spec)
	}
	if from, err = strconv.Atoi(strings.TrimSpace(parts[0])); err != nil {
		return 0, 0, fmt.Errorf(i18n.T("first bar of %q: %w"), spec, err)
	}
	if to, err = strconv.Atoi(strings.TrimSpace(parts[1])); err != nil {
		return 0, 0, fmt.Errorf(i18n.T("last bar of %q: %w"), spec, err)
	}
	if from < 1 || to < from {
		return 0, 0, fmt.Errorf(i18n.T("%q is not a bar range"), spec)
	}
	return from, to, nil
}

func usage() {
	out := flag.CommandLine.Output()
	fmt.Fprintln(out, i18n.T("usage: riffhero [score] [flags]"))
	fmt.Fprintln(out)
	fmt.Fprintln(out, i18n.Tf("  score   a Guitar Pro 7/8, MusicXML or MIDI file: %s", strings.Join(score.Extensions, " ")))
	fmt.Fprintln(out, i18n.T("          omitted, the built-in pentatonic phrase is used"))
	fmt.Fprintln(out)
	flag.PrintDefaults()
}

// listDevices prints what the machine has, which is the first thing anyone
// needs when the guitar is not being heard.
func listDevices(o options, cfg config.Config) error {
	host, err := openHost(o, cfg)
	if err != nil {
		return err
	}
	defer host.Close()

	fmt.Println(i18n.Tf("backend: %s", host.Backend()))
	for _, kind := range []audio.Kind{audio.Input, audio.Output} {
		devices, err := host.Devices(kind)
		if err != nil {
			return err
		}
		heading := i18n.T("output devices:")
		if kind == audio.Input {
			heading = i18n.T("input devices:")
		}
		fmt.Printf("\n%s\n", heading)
		if len(devices) == 0 {
			fmt.Println(i18n.T("  (none)"))
			continue
		}
		for _, d := range devices {
			marker := " "
			if d.IsDefault {
				marker = "*"
			}
			fmt.Printf(" %s %s\n     id %s\n", marker, d.Name, d.ID())
		}
	}
	return nil
}

func listTracks(o options) error {
	clock := practice.Clock{SampleRate: o.sampleRate}
	song, err := loadSong(o, clock)
	if err != nil {
		return err
	}

	fmt.Printf("%s\n", song.Title)
	if song.Artist != "" {
		fmt.Println(i18n.Tf("by %s", song.Artist))
	}
	fmt.Printf("%s\n\n", i18n.Tf("%d bars, %s", len(song.Grid), formatSeconds(clock.Seconds(song.End()))))

	def := song.GuitarTrack()
	for i, tr := range song.Tracks {
		marker := " "
		if i == def {
			marker = "*"
		}
		fmt.Printf(" %s %d  %-28s %-20s %-14s %s\n",
			marker, i, truncate(tr.Name, 28), truncate(tr.Instrument, 20), tr.Tuning.Name,
			i18n.Tf(i18n.Plural(len(tr.Notes), "%d note", "%d notes"), len(tr.Notes)))
	}
	return nil
}

func runCalibration(o options, cfg config.Config) error {
	host, err := openHost(o, cfg)
	if err != nil {
		return err
	}
	defer host.Close()

	in, note, err := resolveDevice(host, audio.Input, o.input, cfg.InputDevice)
	if err != nil {
		return err
	}
	if note != "" {
		fmt.Println(note)
	}
	out, note, err := resolveDevice(host, audio.Output, o.output, cfg.OutputDevice)
	if err != nil {
		return err
	}
	if note != "" {
		fmt.Println(note)
	}

	fmt.Println(i18n.Tf("Measuring the round trip on %s -> %s.", out.Name, in.Name))
	fmt.Println(i18n.T("Play the clicks out loud where the input can hear them, or patch the output back into the input."))
	fmt.Println(i18n.T("Keep the room quiet for a few seconds..."))

	result, err := audio.Calibrate(host, audio.CalibrationOptions{
		SampleRate: o.sampleRate,
		Input:      in,
		Output:     out,
	})
	if err != nil {
		return err
	}

	fmt.Printf("\n%s\n", i18n.Tf("Round trip: %s", result))
	cfg.SetLatency(result.Frames, result.SampleRate)
	cfg.InputDevice, cfg.OutputDevice = in.Name, out.Name
	if err := cfg.Save(); err != nil {
		return fmt.Errorf(i18n.T("storing the measurement: %w"), err)
	}

	path, _ := config.Path()
	fmt.Println(i18n.Tf("Stored in %s", path))
	return nil
}

// resolveDevice picks an endpoint, treating a flag and a stored setting very
// differently.
//
// A name given on the command line is an instruction: if it matches nothing,
// that is an error, because silently using something else is not what was
// asked for. A name remembered from last time is only a preference, and it
// stops matching for ordinary reasons — a different backend names the same
// hardware differently, an interface was unplugged. Refusing to start over a
// stale preference is how an app becomes something people stop opening.
func resolveDevice(host *audio.Host, kind audio.Kind, flagValue, stored string) (*audio.Device, string, error) {
	if flagValue != "" {
		d, err := host.FindDevice(kind, flagValue)
		return d, "", err
	}
	if stored != "" {
		if d, err := host.FindDevice(kind, stored); err == nil {
			return d, "", nil
		}
	}
	d, err := host.FindDevice(kind, "")
	if err != nil {
		return nil, "", err
	}
	if stored != "" {
		if kind == audio.Input {
			return d, i18n.Tf("the remembered input device %q is not here; using %q", stored, d.Name), nil
		}
		return d, i18n.Tf("the remembered output device %q is not here; using %q", stored, d.Name), nil
	}
	return d, "", nil
}

// dryRun plays the whole song against the scripted performer with neither a
// window nor a device, and prints what it scored.
//
// It exists because the loop it drives — importer, timeline, detector,
// scoring, the progressive rule — is the entire product, and everything else
// needed to see it working is a window on somebody's desk. This is the same
// code path the app runs, minus the drawing.
func dryRun(o options, cfg config.Config) error {
	o.noAudio = true

	app, err := build(o, cfg)
	if err != nil {
		return err
	}
	defer app.Close()

	fmt.Printf("%s\n", app.song.Title)
	fmt.Println(i18n.Tf("track %d of %d: %s, %s, %d notes",
		app.track+1, len(app.song.Tracks), app.song.Tracks[app.track].Name,
		app.tuning().Name, len(app.notes())))
	fmt.Printf("%s\n\n", i18n.Tf("%d bars, %s, %d notes in scope",
		len(app.song.Grid), formatSeconds(app.clock.Seconds(app.head.End())), len(app.runner.Scope())))

	app.head.Play()
	const tps = 60
	steps := int(app.clock.Seconds(app.head.End())*tps/app.head.Speed()) + 2*tps

	laps := 0
	for i := 0; i < steps; i++ {
		app.transport.AdvanceSeconds(1.0 / tps)
		u := app.runner.Update()
		if u.LapDone {
			laps++
			fmt.Println(i18n.Tf("lap %d: %.0f%% accuracy, %s -> %s",
				laps, u.LapStats.Accuracy*100, u.Adjustment, ui.SpeedLabel(u.Speed)))
		}
		if app.head.Finished() {
			break
		}
	}

	st := app.runner.Session().Stats()
	fmt.Printf("\n%s\n", i18n.Tf("perfect %d  good %d  miss %d  extra %d", st.Perfect, st.Good, st.Miss, st.Extra))
	fmt.Println(i18n.Tf("accuracy %.0f%%  best combo x%d  %d of %d resolved",
		st.Accuracy*100, st.MaxCombo, st.Resolved, st.Total))

	if st.Resolved == 0 {
		return errors.New(i18n.T("nothing was scored; the practice loop is not running"))
	}
	return nil
}

func openHost(o options, cfg config.Config) (*audio.Host, error) {
	var backends []string
	if b := pick(o.backend, cfg.Backend); b != "" {
		backends = []string{b}
	}
	return audio.OpenHost(backends)
}

func loadSong(o options, clock practice.Clock) (*practice.Song, error) {
	if o.scorePath == "" {
		return practice.SyntheticScore(clock), nil
	}
	return score.Load(o.scorePath, clock)
}

// scoreToOpen is the file the app should start on: the one named on the
// command line, else the one it was last left on. A score that has since been
// moved or deleted is not an error — the built-in phrase stands in, and the
// title screen still opens.
func scoreToOpen(o options, cfg config.Config) string {
	if o.scorePath != "" {
		return o.scorePath
	}
	if cfg.Score == "" {
		return ""
	}
	if _, err := os.Stat(cfg.Score); err != nil {
		return ""
	}
	return cfg.Score
}

// pick returns the first non-empty of the two, so a flag always beats a stored
// setting.
func pick(flagValue, stored string) string {
	if flagValue != "" {
		return flagValue
	}
	return stored
}

func formatSeconds(s float64) string {
	return fmt.Sprintf("%d:%04.1f", int(s)/60, s-float64(int(s)/60*60))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}
