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
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/FullFran/riffhero/internal/audio"
	"github.com/FullFran/riffhero/internal/config"
	"github.com/FullFran/riffhero/internal/practice"
	"github.com/FullFran/riffhero/internal/score"
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
	sampleRate int

	listDevices bool
	listTracks  bool
	calibrate   bool
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "riffhero:", err)
		os.Exit(1)
	}
}

func run() error {
	opts, err := parseFlags()
	if err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		// A broken config must not stop someone practising; say so and carry
		// on with the defaults rather than refusing to start.
		fmt.Fprintln(os.Stderr, "riffhero: ignoring the stored settings:", err)
		cfg = config.Default()
	}

	switch {
	case opts.listDevices:
		return listDevices(opts, cfg)
	case opts.calibrate:
		return runCalibration(opts, cfg)
	case opts.listTracks:
		return listTracks(opts)
	}

	app, err := build(opts, cfg)
	if err != nil {
		return err
	}
	defer app.Close()

	ebiten.SetWindowSize(windowSize())
	ebiten.SetWindowTitle("RiffHero — " + app.song.Title)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)

	if err := ebiten.RunGame(app); err != nil {
		return err
	}
	return app.persist()
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

	flag.StringVar(&o.backing, "backing", "", "backing track to play along with (wav, mp3 or flac)")
	flag.IntVar(&o.track, "track", -1, "which track of the score to practise (default: the first guitar part)")
	flag.StringVar(&o.input, "input", "", "capture device, by name or id (default: the stored or system default)")
	flag.StringVar(&o.output, "output", "", "playback device, by name or id")
	flag.StringVar(&o.backend, "backend", "", "audio backend: jack, pulseaudio or alsa")
	flag.BoolVar(&o.noAudio, "no-audio", false, "run with no audio device, replaying a scripted performance")
	flag.BoolVar(&o.noBacking, "no-playback", false, "capture the guitar but open no output")
	flag.Float64Var(&o.monitor, "monitor", -1, "how much of the guitar to mix into the output, 0..1")
	flag.Float64Var(&o.volume, "volume", -1, "backing track level, 0..1")
	flag.Float64Var(&o.speed, "speed", 0, "initial practice speed, 0.25..1")
	flag.Float64Var(&o.latencyMS, "latency", -1, "round-trip latency in milliseconds, overriding the stored measurement")
	flag.BoolVar(&o.adaptive, "progressive", false, "start with the progressive practice rule switched on")
	flag.StringVar(&o.loop, "loop", "", "practise a bar range, e.g. 9-12")
	flag.IntVar(&o.sampleRate, "rate", 48000, "sample rate to ask the device for")

	flag.BoolVar(&o.listDevices, "list-devices", false, "print the audio devices and exit")
	flag.BoolVar(&o.listTracks, "list-tracks", false, "print the score's tracks and exit")
	flag.BoolVar(&o.calibrate, "calibrate", false, "measure the round-trip audio latency, store it and exit")

	flag.Usage = usage
	flag.Parse()

	if args := flag.Args(); len(args) > 1 {
		return o, fmt.Errorf("expected at most one score file, got %d", len(args))
	} else if len(args) == 1 {
		o.scorePath = args[0]
	}
	return o, nil
}

// parseBarRange reads a "9-12" bar range. Bars are one-based, as a musician
// counts them, and the range includes both ends.
func parseBarRange(spec string) (from, to int, err error) {
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("a loop is written as first-last, for example 9-12; got %q", spec)
	}
	if from, err = strconv.Atoi(strings.TrimSpace(parts[0])); err != nil {
		return 0, 0, fmt.Errorf("first bar of %q: %w", spec, err)
	}
	if to, err = strconv.Atoi(strings.TrimSpace(parts[1])); err != nil {
		return 0, 0, fmt.Errorf("last bar of %q: %w", spec, err)
	}
	if from < 1 || to < from {
		return 0, 0, fmt.Errorf("%q is not a bar range", spec)
	}
	return from, to, nil
}

func usage() {
	out := flag.CommandLine.Output()
	fmt.Fprintln(out, "usage: riffhero [score] [flags]")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "  score   a Guitar Pro 7/8, MusicXML or MIDI file:", strings.Join(score.Extensions, " "))
	fmt.Fprintln(out, "          omitted, the built-in pentatonic phrase is used")
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

	fmt.Printf("backend: %s\n", host.Backend())
	for _, kind := range []audio.Kind{audio.Input, audio.Output} {
		devices, err := host.Devices(kind)
		if err != nil {
			return err
		}
		fmt.Printf("\n%s devices:\n", kind)
		if len(devices) == 0 {
			fmt.Println("  (none)")
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
		fmt.Printf("by %s\n", song.Artist)
	}
	fmt.Printf("%d bars, %s\n\n", len(song.Grid), formatSeconds(clock.Seconds(song.End())))

	def := song.GuitarTrack()
	for i, tr := range song.Tracks {
		marker := " "
		if i == def {
			marker = "*"
		}
		fmt.Printf(" %s %d  %-28s %-20s %-14s %d notes\n",
			marker, i, truncate(tr.Name, 28), truncate(tr.Instrument, 20), tr.Tuning.Name, len(tr.Notes))
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

	fmt.Printf("Measuring the round trip on %s -> %s.\n", out.Name, in.Name)
	fmt.Println("Play the clicks out loud where the input can hear them, or patch the output back into the input.")
	fmt.Println("Keep the room quiet for a few seconds...")

	result, err := audio.Calibrate(host, audio.CalibrationOptions{
		SampleRate: o.sampleRate,
		Input:      in,
		Output:     out,
	})
	if err != nil {
		return err
	}

	fmt.Printf("\nRound trip: %s\n", result)
	cfg.SetLatency(result.Frames, result.SampleRate)
	cfg.InputDevice, cfg.OutputDevice = in.Name, out.Name
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("storing the measurement: %w", err)
	}

	path, _ := config.Path()
	fmt.Printf("Stored in %s\n", path)
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
		return d, fmt.Sprintf("the remembered %s device %q is not here; using %q", kind, stored, d.Name), nil
	}
	return d, "", nil
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
