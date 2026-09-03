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

- [x] Ebitengine window.
- [x] `practice.Frame` type and sample-rate conversion.
- [x] Synthetic score of ~20 notes.
- [x] Moving playhead / scrolling tab mock.
- [x] Fake detector producing note events.
- [x] Perfect / Good / Miss matcher.
- [x] Unit tests around timing windows.

Exit criterion: a synthetic note stream can score a synthetic song deterministically.

Status: met. `TestPhase0ExitCriterion` drives transport + scripted detector +
session end to end and asserts the same scoreboard on two identical runs. The
domain and the layout math are hardware-free; only `cmd/riffhero` needs a
display.

---

# Phase 1 — real guitar input

Goal: guitar -> detector -> note event.

Audio backend:

- [x] evaluate `gen2brain/malgo` / miniaudio duplex capture;
- [x] one duplex device/clock for input and backing playback;
- [x] Linux first: JACK, PulseAudio or ALSA, whichever initializes, which on a
      modern desktop all mean PipeWire.

DSP:

- [x] lock-free or bounded SPSC ring buffer;
- [x] RMS/energy gate;
- [x] onset detector;
- [x] McLeod Pitch Method (primary);
- [x] YIN/YIN-FFT cross-check;
- [x] confidence + cents error;
- [x] stable note tracker to avoid frame-by-frame jitter.

Tests:

- [x] generated sine waves E2..E6;
- [x] harmonically rich synthetic guitar-like tones;
- [x] a duplex stream measured on real hardware, behind the `hardware` tag;
- [ ] recorded DI fixtures later.

Exit criterion: stable monophonic detection over normal guitar range with useful latency.

Status: met, end to end.

| Property | Value |
| --- | --- |
| Analysis latency, attack to emitted note | 65 ms |
| Analysis latency for an expected chord | 183 ms |
| CPU per second of audio, whole chain | 19 ms |
| Allocations per pitch estimate | 0 |
| Detection range | 70 Hz – 1400 Hz |
| Measured round trip, digital loopback | 40–43 ms at 87% confidence |

Three things about the device path were only findable on hardware, and are
recorded in the audio package rather than here: JACK refuses a one-channel
capture request outright, a failed `ma_device_init` leaves the process unable
to open another context, and selecting a device by name panics under cgo's
pointer rules unless the id is copied somewhere that holds no Go pointers.

---

# Phase 2 — backing playback and shared clock

Goal: what you hear and what the scorer sees use the same clock.

- [x] decode WAV first;
- [x] MP3/FLAC after architecture is stable;
- [x] audio output from the same duplex callback as capture;
- [x] transport: play/pause/seek/restart;
- [x] `Frame` is the authoritative timeline;
- [x] latency calibration offset stored as frames;
- [x] A/B loop.

Exit criterion: backing loop remains sample-accurate enough that repeated scoring does not drift.

Status: met, measured. Over three seconds of playback on PipeWire the song
advances 0.9966x of real time, with no underruns and no dropped input, and
every lap of an A-B region is counted within 5 ms of the wrap. The clock is the
device's: the callback moves the playhead from the position stamped on the last
frame it actually handed over, so a starved renderer stalls the score instead
of letting it run ahead of the sound.

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

Status: met.

1. [x] Guitar Pro 7/8 (`.gp`) — the preferred format, and the only one that
       carries real tablature. Repeats and alternate endings are expanded.
       `.gpx` (GP6) and `.gp3/4/5` are detected and refused by name rather than
       mis-parsed.
2. [x] MusicXML, raw and zipped, with chords, backup, ties across barlines and
       tab staves with their own tuning.
3. [x] MIDI, with the tempo map applied per note rather than accumulated.
4. GP3/4/5 remain out of scope; the error tells the user to export.

Importers that carry no tablature have their notes placed on the neck by
`practice.Fretboard`, which remembers where the hand was so a phrase stays in
one position instead of scattering across equally-valid alternatives.

---

# Phase 4 — practice UX

- [x] six-string horizontal tab, and standard notation as an alternative
      reading or alongside it;
- [x] current-position cursor;
- [x] expected note highlight;
- [x] detected pitch + cents;
- [x] Perfect / Good / Miss feedback;
- [x] accuracy and combo;
- [x] keyboard-first controls;
- [x] bar/beat A-B loop selection;
- [x] speed control;
- [x] progressive practice rule.

Initial adaptive rule:

```text
accuracy >= 95% -> +5 percentage points speed
accuracy < 75%  -> -5 percentage points speed
otherwise       -> repeat
```

Implemented as written, with one addition: a lap that resolved nothing is not
evidence. It means the player paused, and dragging the speed down for that
would be nonsense.

Scoring is scoped to the loop region. Scoring the whole song while looping four
bars makes accuracy meaningless — every note outside the region expires as a
miss and drags it to nothing.

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

Status: met. `TestExpectedChordScoresThroughTheSession` drives synthetic audio
of a strummed E5 through the real detector and the real scoring session.

| Case | Score | Found |
| --- | --- | --- |
| E5 power chord, played right | 0.91 | 2 of 2 |
| A barre chord, played right | 0.79 | 5 of 5 |
| A expected, E major played | 0.29 | 2 of 5 |
| E2 expected, only E3 sounding | 0.36, absent | — |

Two rules carry it. The harmonic sum decides rather than the fundamental,
because on a wound low E the fundamental is a rumour and a verifier that looked
only there would fail on exactly the power chords this exists for. And a
partial belongs to the lowest expected pitch that predicts it, because every
harmonic of a pitch an octave up is also a harmonic of the one below, so
evidence alone can never separate them.

The verifier only runs where the score writes two or more notes together. A
single expected note keeps the monophonic path and its much shorter latency.

---

# Phase 6 — quality slowdown

v0.x can alter playback rate even if pitch changes while prototyping.

Resolution: WSOLA in pure Go, in `internal/stretch`, behind an interface small
enough that a Signalsmith binding could replace it without touching a caller.
Practice rates are 0.25x to 1.0x, which is where WSOLA is strongest, and this
keeps the project's only cgo boundary the audio device.

- [x] pitch preserved: 440 Hz at half speed comes back at 440.0000 Hz;
- [x] 0.25x–1.0x, and rate 1.0 is a verbatim copy rather than an overlap-add;
- [x] 17 ms of CPU per second of stereo output, zero allocations in steady
      state.

Exit criterion: backing at 70–90% sounds good enough to practise against for long sessions.

Status: met for the tonal material a backing track is mostly made of. WSOLA
smears aperiodic content — cymbals, breath — and cannot match the phase of a
partial below about 50 Hz. Both are properties of the algorithm and are
documented in the package.

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

---

# The interface

Not a numbered phase, because Phase 4 asked only that the practice loop be
usable from the keyboard and it was. What it did not cover is everything
around the loop: a flag is set before the guitar is plugged in, and the
questions worth asking — which socket, how much latency, which track, which
reading — only become answerable afterwards.

- [x] title screen, with what each choice is currently set to under it;
- [x] a file browser for scores and backing tracks;
- [x] a settings screen reaching every setting there is;
- [x] a device picker, with a meter per input so the guitar's socket is
      visible rather than guessed;
- [x] latency measured from inside the app;
- [x] tablature, standard notation, or both.

The shape follows `inflation-rpg-go`: a mode, an open/update/draw per screen,
widgets as values recomputed every frame, and a key on every button.

---

# Where this leaves it

Every phase's exit criterion is met and the app does what the README promises.
What is worth doing next is no longer a phase:

- **Recorded DI fixtures.** Every DSP test to date is synthetic. The `pluck`
  fixture is a model of a string, not a recording, and it is the last thing
  standing between "the tests pass" and "it works on a real pickup".
- **Articulations.** Bends, slides, hammer-ons and vibrato are parsed out of
  Guitar Pro files and then ignored; a bend currently reads as an out-of-tune
  note. Scoring them needs a pitch-contour matcher, not another importer.
- **Da Capo and Dal Segno.** Detected, not followed. A score using them plays
  straight through.
- **More than six strings.** `practice.Tuning` is a fixed array, so seven- and
  eight-string tracks are skipped.
