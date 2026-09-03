// Package config persists the handful of settings a practice session must not
// make the player re-enter every time: which input the guitar is plugged into,
// and how many frames of latency that path costs.
//
// Everything else in RiffHero is derivable from the song and the flags. These
// two are properties of the room and the hardware, and asking for them twice
// is the difference between an app someone practises with daily and one they
// open once.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/FullFran/riffhero/internal/practice"
)

// Config is the whole persisted state. Zero values are valid: a missing file
// and a default-constructed Config must behave identically, which is what lets
// the app start on a machine that has never run it.
type Config struct {
	// InputDevice and OutputDevice are matched by exact backend ID first and
	// by name substring second, so a config survives being plugged into a
	// different USB port.
	InputDevice  string `json:"input_device,omitempty"`
	OutputDevice string `json:"output_device,omitempty"`

	// Backend is the miniaudio backend to prefer, empty for automatic.
	Backend string `json:"backend,omitempty"`

	// LatencyFrames is the measured round trip of the audio path, at
	// LatencySampleRate. Storing the rate alongside it is what stops a
	// measurement taken at 44.1 kHz from being applied at 48 kHz.
	LatencyFrames     int64 `json:"latency_frames,omitempty"`
	LatencySampleRate int   `json:"latency_sample_rate,omitempty"`

	// Speed, Volume and Monitor are the practice settings worth remembering.
	//
	// None of them is omitempty, and Volume is why: turning the backing all
	// the way down is a decision, and omitempty drops the field, so the next
	// run started from the default and came back at full volume.
	Speed   float64 `json:"speed"`
	Volume  float64 `json:"volume"`
	Monitor float64 `json:"monitor"`

	// Tuning names the tuning to assume for scores that do not carry one.
	Tuning string `json:"tuning,omitempty"`
}

// Default is what a first run gets.
func Default() Config {
	return Config{
		Speed:   1,
		Volume:  0.8,
		Monitor: 0,
	}
}

// Path is where the config lives, following the XDG base directory spec.
func Path() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locating the home directory: %w", err)
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "riffhero", "config.json"), nil
}

// Load reads the config, returning defaults when there is nothing to read.
//
// A missing file is not an error — it is a first run. A corrupt one is:
// silently overwriting settings the player entered by hand would be worse than
// telling them the file is broken.
func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Default(), err
	}
	return LoadFrom(path)
}

// LoadFrom reads a config from an explicit path.
func LoadFrom(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Default(), fmt.Errorf("reading %s: %w", path, err)
	}

	cfg := Default()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Default(), fmt.Errorf("parsing %s: %w", path, err)
	}
	return cfg.sane(), nil
}

// Save writes the config, creating the directory if it is not there.
func (c Config) Save() error {
	path, err := Path()
	if err != nil {
		return err
	}
	return c.SaveTo(path)
}

// SaveTo writes the config to an explicit path.
//
// It writes to a temporary file and renames, because the alternative is a
// truncated config file when the app is killed mid-write, and a truncated
// config file is one the next run refuses to start with.
func (c Config) SaveTo(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating the config directory: %w", err)
	}

	data, err := json.MarshalIndent(c.sane(), "", "  ")
	if err != nil {
		return fmt.Errorf("encoding the config: %w", err)
	}
	data = append(data, '\n')

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replacing %s: %w", path, err)
	}
	return nil
}

// Latency returns the stored round trip converted to the rate actually in use.
// A measurement taken at another rate is still worth having: the delay is a
// property of the buffers in the path, and scaling it is closer than assuming
// zero.
func (c Config) Latency(sampleRate int) practice.Frame {
	if c.LatencyFrames <= 0 || sampleRate <= 0 {
		return 0
	}
	if c.LatencySampleRate <= 0 || c.LatencySampleRate == sampleRate {
		return practice.Frame(c.LatencyFrames)
	}
	scaled := float64(c.LatencyFrames) * float64(sampleRate) / float64(c.LatencySampleRate)
	return practice.Frame(scaled)
}

// SetLatency records a measurement together with the rate it was taken at.
func (c *Config) SetLatency(frames practice.Frame, sampleRate int) {
	c.LatencyFrames = int64(frames)
	c.LatencySampleRate = sampleRate
}

// TuningOrDefault resolves the configured tuning name.
func (c Config) TuningOrDefault() practice.Tuning {
	switch c.Tuning {
	case "drop-d", "dropd", "Drop D":
		return practice.DropDTuning
	case "eb", "half-step-down", "Eb Standard":
		return practice.HalfStepDown
	default:
		return practice.StandardTuning
	}
}

// sane clamps the values that a hand-edited file could put out of range.
func (c Config) sane() Config {
	if c.Speed == 0 {
		c.Speed = 1
	}
	c.Speed = practice.ClampSpeed(c.Speed)
	c.Volume = clamp01(c.Volume)
	c.Monitor = clamp01(c.Monitor)
	if c.LatencyFrames < 0 {
		c.LatencyFrames = 0
	}
	return c
}

func clamp01(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	default:
		return v
	}
}
