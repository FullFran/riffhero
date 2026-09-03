package main

import (
	"fmt"

	"github.com/FullFran/riffhero/internal/audio"
	"github.com/FullFran/riffhero/internal/dsp"
	"github.com/FullFran/riffhero/internal/practice"
)

// The audio side of the app's lifetime.
//
// The host is opened once and never closed until the process ends. That is not
// tidiness: a failed ma_device_init leaves miniaudio unable to create another
// context, and the next InitContext segfaults, so there is exactly one chance
// at a host and it has to last. A *device* is a different matter — closing one
// and opening another on the same host works, and goes on working after one
// that failed to open, which is what makes the settings screen possible.

// openHost brings the audio backend up. It is called once.
func (a *app) openHost() error {
	if a.opts.noAudio || a.host != nil {
		return nil
	}
	host, err := audio.OpenHost(backendList(a.opts, a.cfg))
	if err != nil {
		return err
	}
	a.host = host
	a.inputs, _ = host.Devices(audio.Input)
	a.outputs, _ = host.Devices(audio.Output)
	return nil
}

// resolveInitialDevices picks the endpoints to start with, honouring the flags
// first and the remembered names second.
func (a *app) resolveInitialDevices() error {
	if a.host == nil {
		return nil
	}
	in, note, err := resolveDevice(a.host, audio.Input, a.opts.input, a.cfg.InputDevice)
	if err != nil {
		return err
	}
	a.note(note)
	a.input = in

	if a.opts.noBacking {
		return nil
	}
	out, note, err := resolveDevice(a.host, audio.Output, a.opts.output, a.cfg.OutputDevice)
	if err != nil {
		return err
	}
	a.note(note)
	a.output = out
	return nil
}

// openStream opens a device on the host and moves the run onto its clock.
//
// Everything that belongs to one stream — the engine, the detector, the
// player — is built fresh here, because a new device restarts the capture
// stream at zero and a detector carrying the old stream's positions would
// place every note somewhere it never happened.
func (a *app) openStream() error {
	if a.host == nil {
		return fmt.Errorf("no audio host")
	}
	a.closeStream()

	player := audio.NewPlayer(a.clock, a.end(len(a.backing)/2))
	det := dsp.NewDetector(a.opts.sampleRate)
	det.LatencyOffset = a.latencyOffset()
	a.calibrated = det.LatencyOffset > 0

	engine, err := audio.Open(a.host, audio.Config{
		SampleRate: a.opts.sampleRate,
		Input:      a.input,
		Output:     a.output,
		Capture:    true,
		Playback:   a.output != nil,
		Backing:    a.backing,
		Volume:     a.volumeSetting(),
		Monitor:    a.monitorSetting(),
	}, player, det)
	if err != nil {
		return err
	}
	if err := engine.Start(); err != nil {
		engine.Close()
		return err
	}

	// With no stored measurement, the device's own buffering is a far better
	// starting point than zero: it is a real lower bound on the round trip,
	// and the alternative is telling a player who is dead on time that they
	// are consistently early. It is not a substitute for measuring, and the
	// HUD goes on saying so.
	if !a.calibrated {
		det.LatencyOffset = engine.Latency()
	}

	previous := a.head
	a.engine, a.player, a.det = engine, player, det
	a.head, a.transport = player, nil
	adoptPlayhead(previous, player)
	a.buildRunner()
	return nil
}

// closeStream puts the run back on a game-loop clock and lets the device go.
func (a *app) closeStream() {
	if a.engine == nil {
		return
	}
	a.engine.Close()
	a.engine, a.player, a.det = nil, nil, nil

	previous := a.head
	a.startScripted()
	adoptPlayhead(previous, a.head)
	a.buildRunner()
}

// startScripted is the no-hardware path: the game loop drives a Transport and
// a simulated player produces the note events.
func (a *app) startScripted() {
	a.transport = practice.NewTransport(a.clock, a.end(len(a.backing)/2))
	a.head = a.transport
}

// adoptPlayhead moves a run onto a different clock, carrying the position, the
// region and the speed with it. Changing sound card should not lose the bar
// the player was working on.
func adoptPlayhead(from, to practice.Playhead) {
	if from == nil || to == nil {
		return
	}
	to.SetSpeed(from.Speed())
	to.SetLoop(from.Loop())
	to.Seek(from.Position())
	if from.Playing() {
		to.Play()
	} else {
		to.Pause()
	}
}

// useInput switches the capture device, and leaves the old one in place if the
// new one will not open.
func (a *app) useInput(d *audio.Device) error { return a.useDevices(d, a.output) }

// useOutput switches the playback device.
func (a *app) useOutput(d *audio.Device) error { return a.useDevices(a.input, d) }

// useDevices reopens the stream on a different pair.
//
// A device that will not open is put back the way it was rather than left in a
// state where nothing is running: the settings screen is exactly where someone
// tries the wrong thing, and being dropped into silence for it would be a poor
// answer.
func (a *app) useDevices(in, out *audio.Device) error {
	oldIn, oldOut := a.input, a.output
	a.input, a.output = in, out

	if err := a.openStream(); err != nil {
		a.input, a.output = oldIn, oldOut
		if back := a.openStream(); back != nil {
			// Both are gone. Say so once and carry on without a device rather
			// than leaving the app half-open.
			a.closeStream()
			return fmt.Errorf("%w (and the previous device did not come back: %v)", err, back)
		}
		return err
	}

	a.cfg.InputDevice, a.cfg.OutputDevice = deviceName(a.input), deviceName(a.output)
	return nil
}

// refreshDevices asks the host what is plugged in now, which is what somebody
// does after plugging an interface in with the app already open.
func (a *app) refreshDevices() {
	if a.host == nil {
		return
	}
	if in, err := a.host.Devices(audio.Input); err == nil {
		a.inputs = in
	}
	if out, err := a.host.Devices(audio.Output); err == nil {
		a.outputs = out
	}
}

// live reports whether a real guitar is being listened to.
func (a *app) live() bool { return a.det != nil }

func deviceName(d *audio.Device) string {
	if d == nil {
		return ""
	}
	return d.Name
}

// setBacking installs a decoded backing track and reopens whatever is running,
// because the renderer holds the samples and cannot be handed new ones.
func (a *app) setBacking(pcm []float32) error {
	a.backing = pcm
	if a.engine == nil {
		a.startScripted()
		a.buildRunner()
		return nil
	}
	return a.openStream()
}
