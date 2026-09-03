//go:build hardware

// These tests open a real audio device and are excluded from the default
// build. `make check` must stay runnable on a machine with no sound card and
// no display, so anything that needs hardware lives behind this tag and is run
// deliberately with `make check-audio`.
package audio

import (
	"math"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/FullFran/riffhero/internal/dsp"
	"github.com/FullFran/riffhero/internal/practice"
)

const hwRate = 48000

// One host for the whole package, on purpose.
//
// A failed ma_device_init leaves the process unable to create another audio
// context — the next InitContext segfaults inside miniaudio. That is upstream
// behaviour we have to live with, and a test binary that opened a fresh host
// per test would crash the moment any device could not be opened, hiding
// whichever test actually failed. One host, created once, keeps a failure a
// failure.
var (
	sharedHost *Host
	hostErr    error
	hostOnce   sync.Once
)

func openTestHost(t *testing.T) *Host {
	t.Helper()
	hostOnce.Do(func() { sharedHost, hostErr = OpenHost(nil) })
	if hostErr != nil {
		t.Skipf("no audio backend available: %v", hostErr)
	}
	return sharedHost
}

func TestMain(m *testing.M) {
	code := m.Run()
	if sharedHost != nil {
		_ = sharedHost.Close()
	}
	os.Exit(code)
}

func TestHostListsDevices(t *testing.T) {
	host := openTestHost(t)
	t.Logf("backend: %s", host.Backend())

	for _, kind := range []Kind{Input, Output} {
		devices, err := host.Devices(kind)
		if err != nil {
			t.Fatalf("listing %s devices: %v", kind, err)
		}
		if len(devices) == 0 {
			t.Fatalf("no %s devices", kind)
		}
		for _, d := range devices {
			t.Logf("  %s: %s", kind, d)
		}
	}
}

func TestFindDeviceFallsBackToTheDefault(t *testing.T) {
	host := openTestHost(t)
	d, err := host.FindDevice(Input, "")
	if err != nil {
		t.Fatalf("no default input: %v", err)
	}
	t.Logf("default input: %s", d)

	if _, err := host.FindDevice(Input, "no such device anywhere"); err == nil {
		t.Fatal("a nonsense name should not resolve to anything")
	}
}

// TestDuplexStreamDrivesTheClock is the Phase 2 exit criterion, measured
// rather than asserted from theory: the song position must advance in step
// with real time because it is the device that moves it.
func TestDuplexStreamDrivesTheClock(t *testing.T) {
	host := openTestHost(t)
	clock := practice.Clock{SampleRate: hwRate}

	// Six seconds of a quiet tone as the backing track, so the output is doing
	// real work rather than pushing zeros — and so the measurement window
	// stays well inside the song. Running past the end measures the transport
	// stopping, not the clock drifting.
	const songSeconds = 6
	backing := make([]float32, hwRate*songSeconds*2)
	for i := 0; i < hwRate*songSeconds; i++ {
		v := float32(0.05 * math.Sin(2*math.Pi*220*float64(i)/hwRate))
		backing[i*2], backing[i*2+1] = v, v
	}

	player := NewPlayer(clock, practice.Frame(hwRate*songSeconds))
	det := dsp.NewDetector(hwRate)

	engine, err := Open(host, Config{
		SampleRate: hwRate,
		Capture:    true,
		Playback:   true,
		Backing:    backing,
		Volume:     0.2,
	}, player, det)
	if err != nil {
		t.Skipf("could not open a duplex device: %v", err)
	}
	defer engine.Close()

	if err := engine.Start(); err != nil {
		t.Skipf("could not start the device: %v", err)
	}
	t.Logf("negotiated sample rate: %d", engine.SampleRate())

	player.Play()

	// Measure the slope between two points well inside the run rather than
	// from t=0. A device takes a moment to actually start streaming, and that
	// startup offset is not drift; drift is what this design exists to
	// prevent, and drift shows up in the slope.
	poll := func(d time.Duration) {
		deadline := time.Now().Add(d)
		for time.Now().Before(deadline) {
			// Poll the way the game loop does. Without it the detector's ring
			// fills in half a second and starts dropping, which would measure
			// the test harness rather than the engine.
			det.Poll(player.Position())
			time.Sleep(5 * time.Millisecond)
		}
	}

	// The window is three seconds because the measurement is quantised: the
	// position only moves when a callback fires, so each end of it carries up
	// to one device period of error — 21 ms on JACK, which is 2% of a single
	// second and 0.7% of three.
	poll(300 * time.Millisecond)
	t0, p0 := time.Now(), player.Position()
	poll(3000 * time.Millisecond)
	t1, p1 := time.Now(), player.Position()

	real := t1.Sub(t0).Seconds()
	moved := clock.Seconds(p1 - p0)
	t.Logf("over %.3fs of real time the song moved %.3fs (%.4fx); capture %d frames, underruns %d, dropped %d",
		real, moved, moved/real, engine.StreamPosition(), engine.Underruns(), det.Dropped())

	if p0 <= 0 {
		t.Fatal("the song did not advance; the device is not driving the clock")
	}
	if ratio := moved / real; ratio < 0.985 || ratio > 1.015 {
		t.Fatalf("the song moved at %.4f times real time: the clocks are drifting apart", ratio)
	}
	if engine.StreamPosition() == 0 {
		t.Fatal("nothing was captured")
	}
	if engine.Underruns() > 0 {
		t.Errorf("%d underruns: the render goroutine is not keeping up", engine.Underruns())
	}
	if det.Dropped() > 0 {
		t.Errorf("%d input samples dropped", det.Dropped())
	}
}

// TestCaptureFeedsTheTimeMap proves the other half of the wiring: a position
// in the capture stream resolves to a position in the song, which is what lets
// a detected note be scored at all.
func TestCaptureFeedsTheTimeMap(t *testing.T) {
	host := openTestHost(t)
	clock := practice.Clock{SampleRate: hwRate}

	player := NewPlayer(clock, practice.Frame(hwRate*10))
	det := dsp.NewDetector(hwRate)

	engine, err := Open(host, Config{
		SampleRate: hwRate,
		Capture:    true,
		Playback:   true,
	}, player, det)
	if err != nil {
		t.Skipf("could not open a duplex device: %v", err)
	}
	defer engine.Close()

	if err := engine.Start(); err != nil {
		t.Skipf("could not start the device: %v", err)
	}
	player.Play()
	time.Sleep(500 * time.Millisecond)

	stream := engine.StreamPosition()
	if stream == 0 {
		t.Fatal("nothing was captured")
	}
	// A little way back from the newest frame, which may not have been
	// recorded yet.
	probe := stream - hwRate/10
	song, ok := engine.TimeMap().Lookup(probe)
	if !ok {
		t.Fatalf("capture frame %d maps to no song position", probe)
	}
	t.Logf("capture frame %d is song frame %d (%.3fs)", probe, song, clock.Seconds(song))
	if song <= 0 {
		t.Fatalf("song position %d", song)
	}
}

// TestPausedCaptureMapsToNothing is the case that would otherwise score a
// note against whatever the playhead happened to be sitting on.
func TestPausedCaptureMapsToNothing(t *testing.T) {
	host := openTestHost(t)
	clock := practice.Clock{SampleRate: hwRate}

	player := NewPlayer(clock, practice.Frame(hwRate*10))
	det := dsp.NewDetector(hwRate)

	engine, err := Open(host, Config{SampleRate: hwRate, Capture: true, Playback: true}, player, det)
	if err != nil {
		t.Skipf("could not open a duplex device: %v", err)
	}
	defer engine.Close()
	if err := engine.Start(); err != nil {
		t.Skipf("could not start the device: %v", err)
	}

	// Never played: everything captured belongs to no song position.
	time.Sleep(300 * time.Millisecond)
	stream := engine.StreamPosition()
	if stream == 0 {
		t.Fatal("nothing was captured while paused")
	}
	if _, ok := engine.TimeMap().Lookup(stream / 2); ok {
		t.Fatal("audio captured while paused resolved to a song position")
	}
	if got := player.Position(); got != 0 {
		t.Fatalf("the playhead moved to %d while paused", got)
	}
}

// TestLoopWrapsOnTheDeviceClock is the fix for the bug that inverted the
// progressive practice rule, checked against a real device rather than a
// simulated one: the lap must be counted with the playhead already back
// inside the region, not while it is still approaching the end.
func TestLoopWrapsOnTheDeviceClock(t *testing.T) {
	host := openTestHost(t)
	clock := practice.Clock{SampleRate: hwRate}

	backing := make([]float32, hwRate*4*2)
	for i := 0; i < hwRate*4; i++ {
		v := float32(0.05 * math.Sin(2*math.Pi*220*float64(i)/hwRate))
		backing[i*2], backing[i*2+1] = v, v
	}

	player := NewPlayer(clock, practice.Frame(hwRate*4))
	det := dsp.NewDetector(hwRate)

	engine, err := Open(host, Config{
		SampleRate: hwRate,
		Capture:    true,
		Playback:   true,
		Backing:    backing,
		Volume:     0.15,
	}, player, det)
	if err != nil {
		t.Skipf("could not open a duplex device: %v", err)
	}
	defer engine.Close()
	if err := engine.Start(); err != nil {
		t.Skipf("could not start the device: %v", err)
	}

	a := practice.Frame(hwRate / 2)
	b := a + practice.Frame(hwRate*3/10) // a 300 ms region
	player.SetLoop(practice.Loop{A: a, B: b, Enabled: true})
	player.Restart()
	player.Play()

	// Sample the way the game loop does, and record where the playhead was on
	// the turn each new lap appeared.
	var seenAt []practice.Frame
	laps := player.Laps()
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if n := player.Laps(); n != laps {
			laps = n
			seenAt = append(seenAt, player.Position())
		}
		det.Poll(player.Position())
		time.Sleep(2 * time.Millisecond)
	}

	t.Logf("%d laps of a %.0f ms region; positions seen at %v",
		len(seenAt), clock.Seconds(b-a)*1000, seenAt)

	if len(seenAt) < 3 {
		t.Fatalf("only %d laps in 1.5 s of a 300 ms region", len(seenAt))
	}
	for i, pos := range seenAt {
		if pos < a || pos >= b {
			t.Fatalf("lap %d was counted at %d, outside the region [%d,%d)", i+1, pos, a, b)
		}
		// The lap has to be visible while the playhead is near the top of the
		// region. Seeing it near the end means it was counted by whatever runs
		// ahead of the device, and the scoreboard would reset too early.
		if past := pos - a; past > (b-a)/2 {
			t.Fatalf("lap %d was counted %d frames into the region, over halfway", i+1, past)
		}
	}
	if engine.Underruns() > 0 {
		t.Errorf("%d underruns", engine.Underruns())
	}
}
