# Architecture

## Dependency direction

```text
                       cmd/riffhero
                   (Ebitengine, flags)
                            │
        ┌───────────────────┼───────────────────┐
        ▼                   ▼                   ▼
   internal/ui        internal/audio      internal/score
   layout, HUD        device, engine,     gp / musicxml / midi
                      render, timemap,           │
                      calibration                │
        │                   │                    │
        │      ┌────────────┴────────┐           │
        │      ▼                     ▼           │
        │  internal/stretch   internal/audio/codec│
        │  (WSOLA)            (wav, mp3, flac)   │
        │      │                     │           │
        └──────┴──────────┬──────────┴───────────┘
                          ▼
                  internal/practice
              score timeline, matcher,
              scoring, transport, runner
                          ▲
                          │ implements practice.Detector
                          │
                   internal/dsp
          ring, gate, onset, MPM/YIN, tracker,
                   FFT, chord verifier
```

`internal/practice` imports nothing but the standard library. Everything else
depends on it, never the other way round, which is what keeps a practice run
reproducible in a test with no display, no sound card and no files.

`internal/config` sits to the side: the app reads it, nothing else does.

## Time model

Do not use `time.Now()` to score notes.

```go
type Frame int64

type Clock struct {
    SampleRate int
}
```

At 48 kHz, `2160 frames = 45 ms`.

There are two clocks in the running app and conflating them is the central
mistake this design exists to avoid.

**The capture stream** counts every sample the device has ever delivered. It is
monotonic and never goes back.

**The song position** jumps at a seek, wraps at an A-B loop boundary, and moves
at practice speed rather than at real time.

`audio.TimeMap` is the seam between them. The audio callback records one entry
per invocation — during this many capture frames, the song moved from here to
there — and a lookup is a search through that history. Inside one invocation
the relationship is linear, which is what makes the whole thing a list of line
segments rather than a model. A capture position that maps to nothing (the
transport was paused, or the renderer starved) resolves to nothing rather than
to a guess.

## Audio real-time boundary

```text
callback (real-time thread)
    input PCM ──► downmix to mono ──► detector ring
    output ring ──► spread to N channels ──► device
    record (stream position → song position) in the time map

render goroutine
    backing PCM ──► time stretch ──► output ring, one song
                                     position stamped per frame

game loop
    detector ring ──► onset, pitch, chord ──► DetectedNote
    DetectedNote + song position ──► Session ──► scoreboard
```

Forbidden in the callback: allocation, FFT or any other unbounded DSP, the
filesystem, logging, blocking channels, long mutexes. What is left is a copy, a
few multiply-adds and some atomic stores.

Three consequences worth stating, because each was arrived at the hard way:

- **The song only moves for audio the device actually received.** The callback
  advances the position from the stamp on the last frame it handed over. A
  starved renderer therefore stalls the score rather than letting it run ahead
  of what the player can hear.
- **The renderer works while the transport is paused, and the callback does
  not.** Pressing play has to be instant, so the ring is kept full of the audio
  that starts at the playhead; respecting the pause is the consumer's job.
- **Rendered audio carries a seek generation.** Audio for the old position is
  already queued when the user jumps somewhere else. Without the generation it
  would drag the playhead backwards for as long as it takes to drain.

## Expected-event matching

Single expected note:

```text
onset ──► MPM and YIN over several staggered windows ──► quorum ──► note
```

Expected chord:

```text
window ──► FFT ──► harmonically weighted evidence per expected pitch
      ──► lowest pitch claims each partial ──► present / absent
      ──► unexplained energy penalty
```

The score has already narrowed the hypothesis space to one set of pitches, so
the question is "are these present?", which a magnitude spectrum can answer.
"What is being played?" is unconstrained polyphonic transcription and is not
attempted.

The two paths have different latencies — 65 ms and 183 ms — and they are kept
apart deliberately. Frequency resolution is time, and making every single note
wait for the chord window would triple the latency of most of practice to pay
for an analysis it never runs.

## Score model

Every importer normalizes into the same thing:

```go
type Note struct {
    Start    Frame
    Duration Frame
    MIDI     uint8
    String   uint8 // 1 is the highest-sounding string
    Fret     uint8
}

type Track struct { Name, Instrument string; Tuning Tuning; Notes []Note }
type Song  struct { Title, Artist, Source string; Clock Clock; Grid Grid; Tracks []Track }
```

`Grid` is the bar and beat map, which is what A-B selection snaps to and what
the HUD counts in. Importers that carry tablature keep it; those that do not
get their notes placed on the neck by `practice.Fretboard`, which remembers
where the hand was so a phrase stays in one position.

Repeats are expanded at import, in both the Guitar Pro and MusicXML importers:
a practice timeline is linear. Direction jumps — Da Capo, Dal Segno, Coda,
Fine — are read and deliberately not followed; a score using them plays
straight through, and each importer says so in its package documentation.
