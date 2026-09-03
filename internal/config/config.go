// Package config persists the settings a practice session must not make the
// player re-enter every time: which input the guitar is plugged into, how many
// frames of latency that path costs, and the choices the settings screen puts
// in front of them.
//
// The audio ones are properties of the room and the hardware, the rest are
// properties of the player, and none of them is derivable from the song.
// Asking for them twice is the difference between an app someone practises
// with daily and one they open once.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/FullFran/riffhero/internal/i18n"
	"github.com/FullFran/riffhero/internal/practice"
)

// Notation is which reading the practice view shows.
type Notation string

const (
	NotationTab   Notation = "tab"
	NotationStaff Notation = "staff"
	NotationBoth  Notation = "both"
)

// DefaultNotation is what an unrecognised value reads as. Every method here
// resolves through it instead of inventing a fourth state, because the one
// thing the practice view must never do is render neither stave nor tab and
// leave the player looking at an empty pane.
const DefaultNotation = NotationTab

// Valid reports whether n is a reading the view knows how to draw.
func (n Notation) Valid() bool {
	switch n {
	case NotationTab, NotationStaff, NotationBoth:
		return true
	default:
		return false
	}
}

// orDefault is the single place an unknown reading becomes a known one, so
// Next and Label can never disagree about what a junk value means.
func (n Notation) orDefault() Notation {
	if n.Valid() {
		return n
	}
	return DefaultNotation
}

// Next cycles through the three, so one key can toggle them.
func (n Notation) Next() Notation {
	switch n.orDefault() {
	case NotationTab:
		return NotationStaff
	case NotationStaff:
		return NotationBoth
	default:
		return NotationTab
	}
}

// Label is the word for the reading. "notation" rather than "staff" because
// that is what a guitarist calls it when they are asking to see it.
func (n Notation) Label() string {
	switch n.orDefault() {
	case NotationStaff:
		return "notation"
	case NotationBoth:
		return "both"
	default:
		return "tablature"
	}
}

// The two ways of spelling an accidental. Which one a player wants follows the
// key of the song and their own habit, so it is remembered rather than derived
// from a score that often does not say.
const (
	SpellingSharps = "sharps"
	SpellingFlats  = "flats"
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

	// Language is the language the app speaks, "en" or "es".
	//
	// Empty is not a missing setting, it is a real answer: follow the desktop.
	// That is what a first run gets, and it is why this one is omitempty while
	// Volume is not — the player has said nothing yet, and reading LANG is a
	// better guess than English on a machine whose every other program is
	// already in Spanish. Choosing one from the menu writes it down and stops
	// the guessing.
	Language string `json:"language,omitempty"`

	// Notation and Spelling are how the practice view reads the score back.
	//
	// Both are omitempty, and that is not a contradiction of the rule above:
	// the empty string is not a reading and not an accidental, so it is never
	// an answer the player gave. It only ever means the file was written
	// before the setting existed, and Default() is the right thing to fall
	// back to in that case.
	// InputChannel is which socket of a multi-input interface the guitar is
	// in: "mix", "left" or "right". It is a setting rather than a guess
	// because nothing in the audio distinguishes a guitar in socket one from
	// a microphone recorded onto both.
	InputChannel string `json:"input_channel,omitempty"`

	Notation Notation `json:"notation,omitempty"`
	Spelling string   `json:"spelling,omitempty"`

	// Score and Backing are the last pair opened and ScoreDir is where the
	// browser last looked, so the title screen can offer to carry on instead
	// of making the player walk the filesystem again after every restart.
	// Empty means there is nothing to carry on with, which is exactly the case
	// omitempty is for.
	Score    string `json:"score,omitempty"`
	Backing  string `json:"backing,omitempty"`
	ScoreDir string `json:"score_dir,omitempty"`

	// Track is the last track practised and Progressive whether the
	// progressive rule was left on. Neither is omitempty for the same reason
	// Volume is not: track 0 is the first track and false is the rule switched
	// off, so both zero values are decisions. Dropping them would hand the
	// next run to Default(), and the day a default changes is the day a
	// setting someone turned off comes back on by itself.
	Track       int  `json:"track"`
	Progressive bool `json:"progressive"`
}

// Default is what a first run gets.
func Default() Config {
	return Config{
		Speed:    1,
		Volume:   0.8,
		Monitor:  0,
		Notation: DefaultNotation,
		Spelling: SpellingSharps,
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

// LanguageOrDetected resolves the language to speak: the stored choice if
// there is one, else whatever the desktop is set to.
func (c Config) LanguageOrDetected(lookup func(string) string) i18n.Lang {
	if l := i18n.Lang(c.Language); l.Valid() {
		return l
	}
	return i18n.Detect(lookup)
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

	// A reading or an accidental nobody implements is repaired here rather
	// than carried around: keeping it means every screen that switches on the
	// value needs a fourth branch, and the one that forgets draws nothing.
	if !c.Notation.Valid() {
		c.Notation = DefaultNotation
	}
	if c.Spelling != SpellingSharps && c.Spelling != SpellingFlats {
		c.Spelling = SpellingSharps
	}
	// A language nobody speaks is cleared rather than replaced with English:
	// empty means "follow the desktop", which is the better answer of the two
	// and the one a first run already gets.
	if c.Language != "" && !i18n.Lang(c.Language).Valid() {
		c.Language = ""
	}

	// A negative track index is a panic waiting for whoever indexes the score
	// with it, and there is no sensible reading of "track -1" to preserve.
	if c.Track < 0 {
		c.Track = 0
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
