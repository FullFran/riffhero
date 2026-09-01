# RiffHero — implementation plan

## Product constraint

RiffHero is a practice tool, not a music workstation. Every feature must improve this loop:

1. open song;
2. select guitar input;
3. press play;
4. play along with the backing track;
5. see whether pitch and timing were correct;
6. loop difficult bars and raise speed progressively.

If a feature does not improve that loop, it is not MVP.

---

## Architectural rules

1. `internal/practice` owns the domain model and scoring rules.
2. Domain packages do not import Ebitengine, malgo/miniaudio, PipeWire/JACK, codecs or file formats.
3. Time is represented as sample frames, not wall-clock time.
4. The real-time audio callback does no FFT, allocation, logging or blocking I/O.
5. DSP runs outside the audio callback over a bounded ring buffer.
6. Pitch/chord detection is conditioned by the expected score whenever possible.
7. Prefer simple DSP over ML in the real-time path.

---

# Phase 0 — proof of life

Goal: validate that the UI/game loop and domain clock are boring and deterministic.

- [ ] Ebitengine window.
- [ ] `practice.Frame` type and sample-rate conversion.
- [ ] Synthetic score of ~20 notes.
- [ ] Moving playhead / scrolling tab mock.
- [ ] Fake detector producing note events.
- [ ] Perfect / Good / Miss matcher.
- [ ] Unit tests around timing windows.

Exit criterion: a synthetic note stream can score a synthetic song deterministically.

---

# Phase 1 — real guitar input

Goal: guitar -> detector -> note event.

Audio backend:

- evaluate `gen2brain/malgo` / miniaudio duplex capture;
- prefer one duplex device/clock for input and backing playback;
- Linux first: PipeWire through Pulse/JACK/ALSA backend depending measured latency.

DSP:

- [ ] lock-free or bounded SPSC ring buffer;
- [ ] RMS/energy gate;
- [ ] onset detector;
- [ ] McLeod Pitch Method (primary);
- [ ] YIN/YIN-FFT cross-check;
- [ ] confidence + cents error;
- [ ] stable note tracker to avoid frame-by-frame jitter.

Tests:

- generated sine waves E2..E6;
- harmonically rich synthetic guitar-like tones;
- recorded DI fixtures later.

Exit criterion: stable monophonic detection over normal guitar range with useful latency.

---

# Phase 2 — backing playback and shared clock

Goal: what you hear and what the scorer sees use the same clock.

- [ ] decode WAV first;
- [ ] MP3/FLAC after architecture is stable;
- [ ] audio output from the same duplex callback as capture;
- [ ] transport: play/pause/seek/restart;
- [ ] `Frame` is the authoritative timeline;
- [ ] latency calibration offset stored as frames;
- [ ] A/B loop.

Exit criterion: backing loop remains sample-accurate enough that repeated scoring does not drift.

---

# Phase 3 — real scores

Goal: stop using synthetic songs.

Preferred ingestion order:

1. Guitar Pro 7/8 (`.gp`, GPIF/XML inside container) — narrow parser/adapt existing MIT implementation if worthwhile.
2. MusicXML — broad interoperability and easy test fixtures.
3. MIDI — useful fallback.
4. GP3/4/5 only if real usage justifies it; otherwise document conversion workflow.

Normalize every importer into:

```go
type Note struct {
    Start    Frame
    Duration Frame
    MIDI     uint8
    String   uint8
    Fret     uint8
}

type Event struct {
    Start Frame
    Notes []Note
}
```

Exit criterion: load a real song and render its guitar part against the internal timeline.

---

# Phase 4 — practice UX

- [ ] six-string horizontal tab;
- [ ] current-position cursor;
- [ ] expected note highlight;
- [ ] detected pitch + cents;
- [ ] Perfect / Good / Miss feedback;
- [ ] accuracy and combo;
- [ ] keyboard-first controls;
- [ ] bar/beat A-B loop selection;
- [ ] speed control;
- [ ] progressive practice rule.

Initial adaptive rule:

```text
accuracy >= 95% -> +5 percentage points speed
accuracy < 75%  -> -5 percentage points speed
otherwise       -> repeat
```

No complex gamification until the practice loop itself is fun.

---

# Phase 5 — chords

Do not solve unconstrained polyphonic transcription.

Given score event `E = {expected pitches}`, compute spectral evidence for E.

Pipeline candidate:

```text
window -> FFT/CQT-like bins -> harmonic weighting -> chroma/HPCP
       -> expected-note mask -> score + unexpected-energy penalty
```

Use monophonic detector for single-note events and chord verifier only for simultaneous-note events.

Exit criterion: power chords and common open/barre triads score reliably enough for practice.

---

# Phase 6 — quality slowdown

v0.x can alter playback rate even if pitch changes while prototyping.

Proper solution:

- bind Signalsmith Stretch (MIT, C++) through a minimal cgo wrapper;
- keep this behind an internal interface so the core remains pure Go;
- target 0.5x–1.0x practice rates without pitch shift.

Exit criterion: backing at 70–90% sounds good enough to practice against for long sessions.

---

# Explicit non-goals until after v0.3

- neural transcription;
- amp/cab modelling;
- VST/LV2 host;
- cloud storage;
- social features;
- user accounts;
- song marketplace;
- automatic stem separation;
- 3D note highway;
- generalized chord naming.

---

# Immediate next task

Implement Phase 0 fully before touching an audio driver.

Reason: scoring/timeline bugs and audio bugs are painful to distinguish when introduced together.
