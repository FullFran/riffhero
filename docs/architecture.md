# Architecture

## Dependency direction

```text
             adapters
  ┌──────────────────────────────┐
  │ Ebitengine UI                │
  │ malgo/miniaudio              │
  │ GP / MusicXML / MIDI import  │
  │ audio codecs                 │
  └──────────────┬───────────────┘
                 │ translates to / implements
                 ▼
  ┌──────────────────────────────┐
  │ practice domain              │
  │ score timeline               │
  │ matcher / scoring            │
  └──────────────┬───────────────┘
                 │ consumes events from
                 ▼
  ┌──────────────────────────────┐
  │ pitch DSP                    │
  │ onset / MPM / YIN / chroma   │
  └──────────────────────────────┘
```

The practice domain must be testable headlessly.

## Time model

Do not use `time.Now()` to score notes.

```go
type Frame int64

type Clock struct {
    SampleRate int
}

func (c Clock) Seconds(f Frame) float64 {
    return float64(f) / float64(c.SampleRate)
}
```

At 48 kHz:

```text
2160 frames = 45 ms
```

Backing position, captured guitar and expected note positions must ultimately be expressed in this coordinate system.

## Audio real-time boundary

Callback responsibilities:

```text
input PCM  -> write ring buffer
backing PCM -> output PCM
advance authoritative frame counter
return
```

Forbidden in callback:

- allocations;
- FFT/DSP;
- filesystem;
- logging;
- blocking channels;
- long mutexes.

Analysis goroutine:

```text
ring -> windows -> onset/pitch -> DetectedNote events -> matcher
```

## Expected-event matching

Single expected note:

```text
MPM/YIN -> pitch + confidence + onset frame
```

Chord expected:

```text
spectral evidence -> expected-note verification
```

The score already constrains the hypothesis space. We do not need general music transcription in the hot path.
