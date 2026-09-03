package audio

import (
	"errors"
	"fmt"
	"math"
	"runtime"
	"sync/atomic"
	"unsafe"

	"github.com/gen2brain/malgo"

	"github.com/FullFran/riffhero/internal/dsp"
	"github.com/FullFran/riffhero/internal/practice"
)

// DefaultPeriodFrames is the device period RiffHero asks for. At 48 kHz it is
// 10 ms, which is short enough that the round trip stays playable and long
// enough that a Go callback is not being woken thousands of times a second.
const DefaultPeriodFrames = 480

// outRingFrames is the backing-audio buffer between the render goroutine and
// the callback. It is deliberately small: everything queued here is audio that
// has already been committed to, so a seek cannot take effect until it drains.
// Eighty-odd milliseconds is enough to ride out a GC pause and short enough
// that pressing a key feels immediate.
const outRingFrames = 4096

// chunkFrames bounds how much of a callback is converted at a time. The
// callback may not allocate, and a device is free to hand over a period we
// never asked for, so the scratch buffers are fixed and the block is walked in
// pieces that fit them.
const chunkFrames = 4096

// InputChannel says which of a multi-channel input the guitar is on.
//
// It matters on any interface with more than one input, which is most of
// them. A two-input box puts the guitar on input 1 and leaves input 2 open,
// and averaging the two halves the guitar's level while mixing in whatever
// the empty input is picking up. Averaging is right for a single microphone
// and wrong for an instrument in socket one, and nothing in the audio can
// tell the two apart — so it is asked rather than guessed.
type InputChannel uint8

const (
	// ChannelMix averages every input, which is what a mono source recorded
	// onto both channels wants.
	ChannelMix InputChannel = iota
	ChannelLeft
	ChannelRight
)

func (c InputChannel) String() string {
	switch c {
	case ChannelLeft:
		return "left"
	case ChannelRight:
		return "right"
	default:
		return "mix"
	}
}

// Next cycles the three, so one control covers them.
func (c InputChannel) Next() InputChannel {
	switch c {
	case ChannelMix:
		return ChannelLeft
	case ChannelLeft:
		return ChannelRight
	default:
		return ChannelMix
	}
}

// maxMeteredChannels is how many input levels are reported separately. Two
// covers the interfaces this is for; anything past that is folded into the
// mix and not metered.
const maxMeteredChannels = 2

// Config describes the stream to open.
type Config struct {
	SampleRate   int
	PeriodFrames int

	// Input and Output name the endpoints. Nil means the system default.
	Input  *Device
	Output *Device

	// Capture and Playback say which halves to open. Both is a duplex stream,
	// which is the point: one device, one clock, so what the player hears and
	// what the scorer sees cannot drift apart.
	Capture  bool
	Playback bool

	// Backing is the track to play, interleaved stereo at SampleRate. Nil
	// renders silence, which still drives the timeline.
	Backing []float32

	// Channel picks which input the guitar is on.
	Channel InputChannel

	// Volume is the backing level and Monitor is how much of the captured
	// guitar is mixed into the output, both 0..1. Zero means silence for both,
	// and is a setting rather than an omission — the caller states what it
	// wants. Monitoring is off by default: with an amp in the room it is an
	// echo, and it is only wanted when the guitar goes straight into an
	// interface.
	Volume  float64
	Monitor float64
}

func (c Config) withDefaults() Config {
	if c.SampleRate <= 0 {
		c.SampleRate = 48000
	}
	if c.PeriodFrames <= 0 {
		c.PeriodFrames = DefaultPeriodFrames
	}
	return c
}

// Engine owns the device and everything hanging off it.
//
// Three threads meet here and each has a different contract. The audio
// callback may not allocate, block or take a lock. The render goroutine may do
// anything except fall behind. The game loop only ever reads. What connects
// them are the two lock-free rings and the time map.
type Engine struct {
	cfg    Config
	player *Player
	det    *dsp.Detector

	device *malgo.Device
	ring   *outRing
	tmap   *TimeMap
	rend   *renderer

	stop chan struct{}

	volume  atomic.Uint64
	monitor atomic.Uint64
	channel atomic.Uint32

	// peaks is the loudest sample each input carried during the last
	// callback. It is what lets the device screen show which socket the guitar
	// is actually in, rather than leaving somebody to work it out by playing
	// and watching one number.
	peaks [maxMeteredChannels]atomic.Uint64

	// streamPos counts capture frames handed to the detector since the stream
	// opened. It is the coordinate the detector reports notes in and the key
	// the time map is indexed by.
	streamPos atomic.Int64

	// Callback scratch, sized once. mono holds the downmixed input, stereo the
	// backing audio before it is spread across however many outputs the device
	// turned out to have.
	mono   []float32
	stereo []float32

	started bool
}

// Open initializes a stream. It does not start it: Start does, so the caller
// can wire up the UI first and not lose the opening bars.
func Open(host *Host, cfg Config, player *Player, det *dsp.Detector) (*Engine, error) {
	if host == nil || host.ctx == nil {
		return nil, errors.New("audio host is closed")
	}
	if player == nil {
		return nil, errors.New("audio engine needs a player")
	}
	cfg = cfg.withDefaults()
	if !cfg.Capture && !cfg.Playback {
		return nil, errors.New("audio engine needs capture, playback or both")
	}

	e := &Engine{
		cfg:    cfg,
		player: player,
		det:    det,
		ring:   newOutRing(outRingFrames),
		tmap:   NewTimeMap(256),
		stop:   make(chan struct{}),
		mono:   make([]float32, chunkFrames),
		stereo: make([]float32, chunkFrames*2),
	}
	e.volume.Store(math.Float64bits(cfg.Volume))
	e.monitor.Store(math.Float64bits(cfg.Monitor))
	e.channel.Store(uint32(cfg.Channel))
	e.rend = newRenderer(player, e.ring, cfg.Backing, cfg.SampleRate, &e.volume)

	if det != nil {
		det.Timeline = e.tmap
	}

	deviceCfg, cleanup, err := e.deviceConfig()
	if err != nil {
		return nil, err
	}
	defer cleanup()

	device, err := malgo.InitDevice(host.ctx.Context, deviceCfg, malgo.DeviceCallbacks{Data: e.onData})
	if err != nil {
		return nil, fmt.Errorf("opening audio device: %w", err)
	}
	e.device = device
	return e, nil
}

func (e *Engine) deviceConfig() (malgo.DeviceConfig, func(), error) {
	kind := malgo.Duplex
	switch {
	case e.cfg.Capture && !e.cfg.Playback:
		kind = malgo.Capture
	case !e.cfg.Capture && e.cfg.Playback:
		kind = malgo.Playback
	}

	cfg := deviceConfigFor(kind, e.cfg.SampleRate, e.cfg.PeriodFrames)

	var in, out *Device
	if e.cfg.Capture {
		in = e.cfg.Input
	}
	if e.cfg.Playback {
		out = e.cfg.Output
	}
	return cfg, pinDevices(&cfg, in, out), nil
}

// deviceConfigFor is the configuration RiffHero asks every device for. It is
// shared by the engine and the calibrator, because the two diverging is
// exactly how the calibrator kept a fatal channel count after the engine's was
// fixed.
//
// The format is pinned and the channel counts deliberately are not. Asking for
// one capture channel seems obviously right — the detector is monophonic — and
// it is what JACK refuses outright: its ports are the system's, and a duplex
// device that does not match them fails to initialize. Worse, a failed
// ma_device_init leaves the process unable to create another context at all,
// so there is no second attempt to fall back to. Taking whatever the device
// has and folding it down in the callback costs a handful of adds and works on
// every backend.
func deviceConfigFor(kind malgo.DeviceType, sampleRate, periodFrames int) malgo.DeviceConfig {
	cfg := malgo.DefaultDeviceConfig(kind)
	cfg.SampleRate = uint32(sampleRate)
	cfg.PeriodSizeInFrames = uint32(periodFrames)
	cfg.PerformanceProfile = malgo.LowLatency
	cfg.Capture.Format = malgo.FormatF32
	cfg.Capture.Channels = 0
	cfg.Playback.Format = malgo.FormatF32
	cfg.Playback.Channels = 0
	return cfg
}

// pinDevices points the config at the chosen endpoints and returns the release
// function to call once the device has been initialized.
//
// The IDs are handed to C as raw pointers into Go memory, so they have to be
// pinned for the duration of the call. malgo's own DeviceID.Pointer() copies
// them onto the C heap instead, which works but leaks: it has no matching
// free. Pinning is the same thing without the leak, and it is safe because
// miniaudio copies the ID into the device before ma_device_init returns.
//
// The IDs are copied into a bare array first, and that is not tidiness. Go may
// only hand C a pointer to memory that contains no Go pointers, and the check
// is made against the whole enclosing object — so pointing at Device.id, in a
// struct that also holds a Name string, panics with "Go pointer to unpinned Go
// pointer" the moment a device is chosen by name instead of defaulted. The
// array holds nothing but bytes.
func pinDevices(cfg *malgo.DeviceConfig, in, out *Device) func() {
	pinner := &runtime.Pinner{}
	ids := new([2]malgo.DeviceID)
	pinner.Pin(ids)

	if in != nil {
		ids[0] = in.id
		cfg.Capture.DeviceID = unsafe.Pointer(&ids[0])
	}
	if out != nil {
		ids[1] = out.id
		cfg.Playback.DeviceID = unsafe.Pointer(&ids[1])
	}
	return func() {
		runtime.KeepAlive(ids)
		pinner.Unpin()
	}
}

// Start begins the stream and the render goroutine.
func (e *Engine) Start() error {
	if e.started {
		return nil
	}
	go e.rend.run(e.stop)
	if err := e.device.Start(); err != nil {
		close(e.stop)
		return fmt.Errorf("starting audio device: %w", err)
	}
	e.started = true

	// Everything above this layer — the score, the backing track, the timing
	// windows — is measured in frames at one rate. A device that agreed to a
	// different one would silently stretch the whole song, so it is refused
	// rather than worked around.
	if got := e.SampleRate(); got != e.cfg.SampleRate {
		e.Close()
		return fmt.Errorf("the device runs at %d Hz, not the requested %d; rerun with --rate %d", got, e.cfg.SampleRate, got)
	}
	return nil
}

// Close stops the stream and releases the device.
func (e *Engine) Close() {
	if e == nil {
		return
	}
	if e.started {
		_ = e.device.Stop()
		close(e.stop)
		e.started = false
	}
	if e.device != nil {
		e.device.Uninit()
		e.device = nil
	}
	if e.det != nil {
		e.det.Timeline = nil
	}
}

// TimeMap is the capture-stream-to-song mapping, for the detector.
func (e *Engine) TimeMap() *TimeMap { return e.tmap }

// Underruns counts callbacks the render goroutine could not keep up with. A
// non-zero value is the honest explanation for audio that stuttered, and the
// UI shows it rather than letting the player wonder.
func (e *Engine) Underruns() uint64 { return e.ring.Underruns() }

// StreamPosition is how many capture frames have been delivered.
func (e *Engine) StreamPosition() int64 { return e.streamPos.Load() }

// SampleRate is the rate the device negotiated. It can differ from the one
// asked for, and everything downstream must use this one.
func (e *Engine) SampleRate() int {
	if e.device == nil {
		return e.cfg.SampleRate
	}
	if r := e.device.SampleRate(); r > 0 {
		return int(r)
	}
	return e.cfg.SampleRate
}

// Latency is the device's own buffering, in frames — one period times the
// number of periods, doubled for a duplex stream because the sound goes out
// and comes back. It is a lower bound on the round trip, not a measurement:
// Calibrate measures.
func (e *Engine) Latency() practice.Frame {
	n := practice.Frame(e.cfg.PeriodFrames)
	if e.cfg.Capture && e.cfg.Playback {
		return 2 * n
	}
	return n
}

// SetVolume sets the backing level, 0..1.
func (e *Engine) SetVolume(v float64) { e.volume.Store(math.Float64bits(clamp01(v))) }

// Volume is the backing level.
func (e *Engine) Volume() float64 { return math.Float64frombits(e.volume.Load()) }

// SetMonitor sets how much captured guitar is mixed into the output.
func (e *Engine) SetMonitor(v float64) { e.monitor.Store(math.Float64bits(clamp01(v))) }

// Monitor is the current monitoring level.
func (e *Engine) Monitor() float64 { return math.Float64frombits(e.monitor.Load()) }

// onData is the real-time callback. Everything it does is a copy, an add or an
// atomic store; there is no allocation, no lock and no branch that can block.
//
// The channel counts are derived from the buffer lengths rather than
// remembered, because the device decides them and deriving cannot go stale.
func (e *Engine) onData(outBytes, inBytes []byte, frames uint32) {
	n := int(frames)
	if n <= 0 {
		return
	}
	inCh := channelsOf(inBytes, n)
	outCh := channelsOf(outBytes, n)

	in := asFloat32(inBytes, n*inCh)
	out := asFloat32(outBytes, n*outCh)
	monitor := float32(math.Float64frombits(e.monitor.Load()))
	playing := e.player.Playing()

	streamStart := e.streamPos.Load()
	if inCh > 0 {
		e.streamPos.Store(streamStart + int64(n))
	}

	var first, last stamp
	got := 0

	for off := 0; off < n; {
		size := n - off
		if size > chunkFrames {
			size = chunkFrames
		}

		mono := e.mono[:size]
		if inCh > 0 {
			block := in[off*inCh : (off+size)*inCh]
			pick(mono, block, inCh, InputChannel(e.channel.Load()))
			e.meter(block, inCh, off == 0)
			if e.det != nil {
				e.det.Write(mono)
			}
		}

		// The song only moves for audio the device actually received, so the
		// ring is drained even when there is no output to send it to. A
		// starved renderer then stalls the score instead of letting it run
		// ahead of what the player can hear.
		//
		// A stopped transport is the one case where the ring is left alone.
		// The renderer keeps working while paused so that play is instant, and
		// consuming that work here would advance the song while the player
		// thinks nothing is happening.
		stereo := e.stereo[:size*2]
		if playing {
			c, f, l, ok := e.ring.Pop(stereo, size)
			if ok && c > 0 {
				if got == 0 {
					first = f
				}
				last = l
				got += c
			}
		} else {
			for i := range stereo {
				stereo[i] = 0
			}
		}

		if outCh > 0 {
			if monitor > 0 && inCh > 0 {
				for i := 0; i < size; i++ {
					s := mono[i] * monitor
					stereo[i*2] += s
					stereo[i*2+1] += s
				}
			}
			spread(out[off*outCh:(off+size)*outCh], stereo, outCh)
		}
		off += size
	}

	if got > 0 {
		e.player.observe(last)
		// Only the frames the ring actually supplied moved the song; anything
		// after them was silence the callback filled in. Claiming the whole
		// block advanced linearly would map input captured during an underrun
		// to a position that was never played.
		//
		// songEnd is exclusive: the block covered up to one frame past the
		// last stamp.
		e.tmap.Push(streamStart, int64(got), first.pos, last.pos+1, true)
		if got < n {
			e.tmap.Push(streamStart+int64(got), int64(n-got), 0, 0, false)
		}
		return
	}
	e.tmap.Push(streamStart, int64(n), 0, 0, false)
}

// channelsOf recovers a buffer's channel count from its length.
func channelsOf(buf []byte, frames int) int {
	if frames <= 0 || len(buf) == 0 {
		return 0
	}
	return len(buf) / (4 * frames)
}

// pick folds an interleaved block down to the one channel the detector wants.
//
// Taking a single channel rather than the average is the difference between
// hearing a guitar and hearing a guitar at half level with the next socket's
// hum on top of it.
func pick(dst, src []float32, channels int, which InputChannel) {
	switch {
	case channels == 1:
		copy(dst, src)
	case which == ChannelLeft:
		for i := range dst {
			dst[i] = src[i*channels]
		}
	case which == ChannelRight:
		for i := range dst {
			dst[i] = src[i*channels+1]
		}
	case channels == 2:
		for i := range dst {
			dst[i] = (src[i*2] + src[i*2+1]) * 0.5
		}
	default:
		scale := float32(1) / float32(channels)
		for i := range dst {
			var sum float32
			for c := 0; c < channels; c++ {
				sum += src[i*channels+c]
			}
			dst[i] = sum * scale
		}
	}
}

// meter records the loudest sample on each input. Two multiplications and a
// comparison per sample, which is what the callback's budget allows.
func (e *Engine) meter(src []float32, channels int, reset bool) {
	n := maxMeteredChannels
	if channels < n {
		n = channels
	}
	for c := 0; c < n; c++ {
		peak := float32(0)
		for i := c; i < len(src); i += channels {
			v := src[i]
			if v < 0 {
				v = -v
			}
			if v > peak {
				peak = v
			}
		}
		if !reset {
			if was := math.Float64frombits(e.peaks[c].Load()); was > float64(peak) {
				peak = float32(was)
			}
		}
		e.peaks[c].Store(math.Float64bits(float64(peak)))
	}
}

// InputPeaks is the loudest sample each input carried during the last
// callback, one per channel, 0..1.
func (e *Engine) InputPeaks() [maxMeteredChannels]float64 {
	var out [maxMeteredChannels]float64
	for c := range out {
		out[c] = math.Float64frombits(e.peaks[c].Load())
	}
	return out
}

// Channel is the input the detector is listening to.
func (e *Engine) Channel() InputChannel { return InputChannel(e.channel.Load()) }

// SetChannel switches inputs without reopening the device, so somebody can try
// each socket while playing.
func (e *Engine) SetChannel(c InputChannel) { e.channel.Store(uint32(c)) }

// spread writes stereo frames out to however many channels the device has.
func spread(dst, stereo []float32, channels int) {
	switch channels {
	case 1:
		for i := range dst {
			dst[i] = (stereo[i*2] + stereo[i*2+1]) * 0.5
		}
	case 2:
		copy(dst, stereo)
	default:
		frames := len(dst) / channels
		for i := 0; i < frames; i++ {
			base := i * channels
			dst[base] = stereo[i*2]
			dst[base+1] = stereo[i*2+1]
			for c := 2; c < channels; c++ {
				dst[base+c] = 0
			}
		}
	}
}

// asFloat32 reinterprets the device's byte buffer as the float32 samples it
// already holds. Converting would mean allocating, and allocating is the one
// thing this callback may not do.
func asFloat32(b []byte, n int) []float32 {
	if len(b) < n*4 || n <= 0 {
		return nil
	}
	return unsafe.Slice((*float32)(unsafe.Pointer(&b[0])), n)
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
