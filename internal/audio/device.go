// Package audio is the adapter between RiffHero's practice timeline and a real
// audio device. It is the only package that knows a device exists.
//
// The shape of it follows one rule from docs/architecture.md: the callback
// copies bytes and advances a counter, and nothing else. Decoding, stretching,
// loop bookkeeping and pitch analysis all live on other goroutines, reached
// through lock-free rings.
package audio

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gen2brain/malgo"
)

// Kind distinguishes a capture device from a playback one.
type Kind uint8

const (
	Input Kind = iota
	Output
)

func (k Kind) String() string {
	if k == Output {
		return "output"
	}
	return "input"
}

// Device is one selectable audio endpoint.
type Device struct {
	Name      string
	IsDefault bool
	Kind      Kind

	id malgo.DeviceID
}

// ID is the backend's own identifier, stable enough to store in a config file.
func (d Device) ID() string { return d.id.String() }

func (d Device) String() string {
	if d.IsDefault {
		return d.Name + " (default)"
	}
	return d.Name
}

// Backends lists the miniaudio backends RiffHero will try, in order.
//
// On Linux this order is a latency decision. JACK — which on a modern desktop
// usually means PipeWire's JACK interface — hands us the graph's own quantum.
// PulseAudio, likewise usually PipeWire, is the compatible fallback and is what
// works on a machine that has never been configured for audio work. Raw ALSA is
// last because it takes the device exclusively, which on a laptop means nothing
// else can make a sound while practising.
var DefaultBackends = []string{"jack", "pulseaudio", "alsa"}

var backendsByName = map[string]malgo.Backend{
	"jack":       malgo.BackendJack,
	"pulseaudio": malgo.BackendPulseaudio,
	"pulse":      malgo.BackendPulseaudio,
	"alsa":       malgo.BackendAlsa,
}

// Host is an initialized audio backend. Opening one is what tells us whether
// this machine can do audio at all, so it is separate from opening a device:
// the UI can list devices, and fail gracefully, before committing to a stream.
type Host struct {
	ctx     *malgo.AllocatedContext
	backend string
}

// OpenHost initializes the first backend in the list that works. An empty list
// means DefaultBackends.
func OpenHost(names []string) (*Host, error) {
	if len(names) == 0 {
		names = DefaultBackends
	}

	var errs []string
	for _, name := range names {
		backend, ok := backendsByName[strings.ToLower(name)]
		if !ok {
			errs = append(errs, fmt.Sprintf("%s: unknown backend", name))
			continue
		}
		// One backend at a time, so the name we report is the one in use.
		// Handing miniaudio the whole list would leave us guessing.
		ctx, err := malgo.InitContext([]malgo.Backend{backend}, malgo.ContextConfig{}, nil)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		return &Host{ctx: ctx, backend: strings.ToLower(name)}, nil
	}
	return nil, fmt.Errorf("no audio backend available (%s)", strings.Join(errs, "; "))
}

// Backend is the name of the backend that was initialized.
func (h *Host) Backend() string { return h.backend }

// Close releases the backend.
func (h *Host) Close() error {
	if h == nil || h.ctx == nil {
		return nil
	}
	err := h.ctx.Uninit()
	h.ctx.Free()
	h.ctx = nil
	return err
}

// Devices lists the endpoints of one kind.
func (h *Host) Devices(kind Kind) ([]Device, error) {
	if h == nil || h.ctx == nil {
		return nil, errors.New("audio host is closed")
	}
	which := malgo.Capture
	if kind == Output {
		which = malgo.Playback
	}

	infos, err := h.ctx.Devices(which)
	if err != nil {
		return nil, fmt.Errorf("listing %s devices: %w", kind, err)
	}

	out := make([]Device, 0, len(infos))
	for i := range infos {
		out = append(out, Device{
			Name:      infos[i].Name(),
			IsDefault: infos[i].IsDefault != 0,
			Kind:      kind,
			id:        infos[i].ID,
		})
	}
	return out, nil
}

// FindDevice resolves a device by exact ID, then by case-insensitive substring
// of its name, then falls back to the default. The substring match exists
// because a device's ID is unreadable and its name changes when it is plugged
// into a different USB port; letting the user write "Scarlett" is the only
// selection that survives real life.
func (h *Host) FindDevice(kind Kind, want string) (*Device, error) {
	devices, err := h.Devices(kind)
	if err != nil {
		return nil, err
	}
	if len(devices) == 0 {
		return nil, fmt.Errorf("no %s devices found", kind)
	}
	if want == "" {
		return defaultDevice(devices), nil
	}

	for i := range devices {
		if devices[i].ID() == want {
			return &devices[i], nil
		}
	}
	lower := strings.ToLower(want)
	for i := range devices {
		if strings.Contains(strings.ToLower(devices[i].Name), lower) {
			return &devices[i], nil
		}
	}
	return nil, fmt.Errorf("no %s device matching %q", kind, want)
}

func defaultDevice(devices []Device) *Device {
	for i := range devices {
		if devices[i].IsDefault {
			return &devices[i]
		}
	}
	return &devices[0]
}
