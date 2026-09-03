package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/FullFran/riffhero/internal/practice"
)

func TestLoadFromAMissingFileIsAFirstRunNotAnError(t *testing.T) {
	cfg, err := LoadFrom(filepath.Join(t.TempDir(), "nothing.json"))
	if err != nil {
		t.Fatalf("a missing config must not be an error: %v", err)
	}
	if cfg != Default() {
		t.Fatalf("got %+v, want the defaults", cfg)
	}
}

func TestLoadFromACorruptFileIsAnError(t *testing.T) {
	// Silently overwriting settings someone typed by hand is worse than
	// telling them their file is broken.
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFrom(path); err == nil {
		t.Fatal("expected an error for a corrupt config")
	}
}

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.json")

	want := Config{
		InputDevice:       "Scarlett",
		OutputDevice:      "Built-in",
		Backend:           "pulseaudio",
		LatencyFrames:     1234,
		LatencySampleRate: 48000,
		Speed:             0.75,
		Volume:            0.6,
		Monitor:           0.2,
		Tuning:            "drop-d",
	}
	if err := want.SaveTo(path); err != nil {
		t.Fatalf("saving: %v", err)
	}

	got, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestSaveCreatesTheDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "config.json")
	if err := Default().SaveTo(path); err != nil {
		t.Fatalf("saving: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the file was not written: %v", err)
	}
}

func TestSaveLeavesNoTemporaryFileBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := Default().SaveTo(path); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("directory holds %d entries, want only the config: %v", len(entries), entries)
	}
}

func TestLoadClampsAHandEditedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{"speed": 9, "volume": 5, "monitor": -1, "latency_frames": -20}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Speed != practice.SpeedMax {
		t.Fatalf("speed %v, want %v", cfg.Speed, practice.SpeedMax)
	}
	if cfg.Volume != 1 {
		t.Fatalf("volume %v, want 1", cfg.Volume)
	}
	if cfg.Monitor != 0 {
		t.Fatalf("monitor %v, want 0", cfg.Monitor)
	}
	if cfg.LatencyFrames != 0 {
		t.Fatalf("latency %v, want 0", cfg.LatencyFrames)
	}
}

func TestLatencyScalesBetweenSampleRates(t *testing.T) {
	// A measurement is a property of the buffers in the path, so scaling it is
	// closer to the truth than assuming zero when the rate changes.
	var cfg Config
	cfg.SetLatency(480, 48000)

	if got := cfg.Latency(48000); got != 480 {
		t.Fatalf("same rate gave %d, want 480", got)
	}
	if got := cfg.Latency(24000); got != 240 {
		t.Fatalf("half the rate gave %d, want 240", got)
	}
	if got := cfg.Latency(0); got != 0 {
		t.Fatalf("a nonsense rate gave %d", got)
	}
	if got := (Config{}).Latency(48000); got != 0 {
		t.Fatalf("an unmeasured config gave %d, want 0", got)
	}
}

func TestLatencyWithoutARecordedRateIsTakenAsIs(t *testing.T) {
	cfg := Config{LatencyFrames: 500}
	if got := cfg.Latency(44100); got != practice.Frame(500) {
		t.Fatalf("got %d, want 500", got)
	}
}

func TestTuningOrDefault(t *testing.T) {
	cases := map[string]practice.Tuning{
		"":            practice.StandardTuning,
		"drop-d":      practice.DropDTuning,
		"eb":          practice.HalfStepDown,
		"nonsense":    practice.StandardTuning,
		"Eb Standard": practice.HalfStepDown,
	}
	for name, want := range cases {
		if got := (Config{Tuning: name}).TuningOrDefault(); got != want {
			t.Fatalf("%q resolved to %q, want %q", name, got.Name, want.Name)
		}
	}
}

func TestPathFollowsXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-example")
	got, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if want := "/tmp/xdg-example/riffhero/config.json"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAMutedBackingStaysMuted(t *testing.T) {
	// `omitempty` dropped a zero volume, so LoadFrom started from the default
	// and the backing came back at full level. Turning it all the way down is
	// a decision, not an omission.
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := Default()
	cfg.Volume = 0
	cfg.Monitor = 0
	if err := cfg.SaveTo(path); err != nil {
		t.Fatal(err)
	}

	got, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Volume != 0 {
		t.Fatalf("volume came back as %v", got.Volume)
	}
	if got.Monitor != 0 {
		t.Fatalf("monitor came back as %v", got.Monitor)
	}
}
