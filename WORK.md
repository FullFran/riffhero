# Work handoff

## Objective

Build the smallest useful Linux-first real-guitar practice game in Go.

```text
score timeline + backing clock + guitar note event -> Perfect / Good / Miss
```

## Current state

Every phase in `PLAN.md` has met its exit criterion. The app loads a real
score, plays a backing track, listens to a real guitar, loops a section, slows
it down without dropping pitch, scores single notes and chords, and raises the
speed on its own when a lap comes out clean.

```text
internal/practice   the domain: frames, notes, tuning, bars, loop, speed,
                    matcher, session, runner. Imports nothing but stdlib.
internal/dsp        ring, gate, onset, MPM, YIN, tracker, FFT, chroma,
                    chord verifier, and the Detector that joins them.
internal/audio      device, duplex engine, render goroutine, time map,
                    latency calibration.
internal/audio/codec  wav (by hand), mp3, flac, plus rate and channel
                    conversion.
internal/stretch    WSOLA, pure Go.
internal/score      gp / musicxml / midi importers behind one front door.
internal/config     ~/.config/riffhero/config.json.
internal/ui         tab geometry and the HUD, both headless.
cmd/riffhero        flags, wiring, the Ebitengine view.
```

## What is worth knowing before changing anything

### The two clocks

The capture stream counts every sample the device ever delivered and never goes
back. The song position jumps at a seek, wraps at a loop boundary, and moves at
practice speed. They are related only through `audio.TimeMap`, which records
one line segment per audio callback. Anything that assumes they are the same
number will work perfectly until the first loop or seek.

### The callback's contract

`Engine.onData` may not allocate, block, take a lock or do unbounded work. It
downmixes the input, copies it into a ring, pops the backing out of another
ring, spreads it across however many channels the device has, and stores a few
atomics. Everything else — decoding, stretching, loop bookkeeping, pitch
analysis — is on other goroutines.

The song position moves only for frames the device actually received. That is
what makes the clock honest, and it is why a starved renderer stalls the score
rather than racing ahead of the sound.

### Rules that were bugs first

DSP, from Phase 1 and still true:

- onset detection compares against the **minimum** of recent hops, not their
  average: on a guitar the previous note is still ringing when the next is
  struck, so energy adds rather than replaces and an average swallows the
  attack;
- that envelope is measured over four hops, not one. A 256-sample hop holds
  half a cycle of the low E, so its RMS tracks the phase of the waveform and a
  minimum-based rule reads that swing as an attack several times per note;
- MPM's search for the first key maximum starts at lag zero, not at the minimum
  lag of the range: at the top of the range the minimum lag falls inside the
  first hump of the NSDF, and starting there returns the octave below;
- nothing may judge level on a single hop. The gate used to, and vetoed onsets
  whose first hop landed on a quiet part of the waveform;
- a dropped sample is a hole in the timeline, not just lost audio. `Poll` feeds
  the gap to `Tracker.Skip`, or one overflow shifts every later detection
  earlier for the rest of the run;
- windows where MPM and YIN disagreed are not ordinary evidence, and the quorum
  tie-break is explicit, or a split vote resolves differently from run to run.

Chords:

- the harmonic sum decides, not the fundamental. On a wound low E the
  fundamental is a rumour;
- a partial belongs to the lowest expected pitch that predicts it, because
  every harmonic of a pitch an octave up is also a harmonic of the one below.
  Without that rule, expected E2 with only E3 sounding scores as present;
- 6 Hz per bin is what sets the 171 ms window. At 4096 samples A2's search band
  swallows the B2 beside it and the wrong chord scores as well as the right one.

Audio, all found by opening real devices:

- JACK refuses a one-channel capture request. Its ports are the system's, and a
  duplex device that does not match them fails to initialize;
- a failed `ma_device_init` leaves the process unable to create another
  context — the next `InitContext` segfaults inside miniaudio. There is no
  second attempt to fall back to, which is why the configuration asks for
  nothing a backend can refuse;
- handing C a pointer to `Device.id` panics, because the enclosing struct also
  holds a `Name string` and Go may only pass a pointer to memory containing no
  Go pointers. The id is copied into a bare byte array first;
- the renderer must fill the ring, not merely keep a chunk's worth of room in
  it. Demanding space for the worst-case expansion up front kept the ring three
  quarters empty and underran on every period that ran late;
- latency calibration correlates the *rise* in each envelope. A room turns a
  4 ms click into 200 ms of decay, and the envelopes themselves correlate at
  about a third — low enough to be refused as noise.

Domain:

- a detection with the wrong pitch or bad intonation consumes nothing, so the
  player can still rescue the note inside its window;
- a detection is assigned to the *nearest* unresolved note it can legitimately
  hit, not the first;
- accuracy is `(Perfect + Good) / resolved`, and scoring is scoped to the loop
  region — otherwise every note outside it expires as a miss;
- a lap that resolved nothing is not evidence for the progressive rule.

## Building and running

```bash
make check        # vet + tests, no display and no audio device
make race         # the same under -race
make check-audio  # opens a real device; behind the `hardware` build tag
make demo         # the whole loop with no hardware at all
make run ARGS="song.gp --backing song.flac --loop 9-12"
```

Two environment traps, both outside the code:

1. Without the X11/GL headers the cgo build fails with
   `fatal error: X11/Xlib.h: No such file or directory`. Install what
   `make deps` prints.
2. On a machine with Nix-based tooling in the shell the binary builds but the
   window never opens. Nix exports driver-discovery variables pointing at
   `/nix/store` builds linked against a newer glibc: `LD_LIBRARY_PATH` yields
   ``libGL.so: version `GLIBC_2.38' not found``, and
   `__EGL_VENDOR_LIBRARY_FILENAMES` yields
   `glfw: EGL: Failed to get EGL display`. The second is read by libglvnd on
   its own, so clearing `LD_LIBRARY_PATH` alone does not help — Ebitengine goes
   through EGL on Linux, not GLX. `scripts/with-system-gl.sh` clears both, and
   `make run` goes through it.

## Verified on hardware

On the development machine (Pop!_OS, PipeWire 1.0.3, built-in audio):

| | |
| --- | --- |
| Clock drift over a second of duplex playback | 0.998x, 0 underruns, 0 dropped |
| Round trip, digital loopback | 40–43 ms at 87% confidence, ±3 ms |
| Round trip, speaker to microphone | 101–123 ms at 43–49% confidence |
| Live detection from the room | a 1200 Hz click read as D6 +36¢ |

The acoustic figure varies by about one device period between runs, which is
real: PipeWire changes its quantum. The digital loopback is the measurement to
trust, and `--latency` overrides either.

## Next implementation session

Nothing is half-finished; these are new pieces of work, in the order they would
most improve the practice loop:

1. **Recorded DI fixtures.** Every DSP test is synthetic. `pluck` is a model of
   a string, not a recording, and it is the last thing between "the tests pass"
   and "it works on a real pickup". Record a few takes at a known tempo and
   assert the detector's output against a hand-checked transcription.
2. **Articulations.** Guitar Pro carries bends, slides, hammer-ons and vibrato;
   the importer drops them and a bend currently scores as an out-of-tune note.
   Scoring them means matching a pitch contour rather than a semitone.
3. **A song picker in the app.** Everything is command-line today.
4. **Da Capo and Dal Segno**, which are detected but not followed.
5. **Seven- and eight-string tracks**, which are skipped because
   `practice.Tuning` is a fixed six-element array.

## Design constraints

- Linux first.
- Go first; small cgo boundaries are acceptable when justified. There is
  exactly one: the audio device.
- Ebitengine UI.
- One authoritative timeline in sample frames.
- Real-time callback stays tiny and non-blocking.
- MPM/YIN for single notes, expected-spectrum verification for chords, never
  unconstrained transcription.
- No backend, accounts, ML transcription, amp simulator or 3D highway.

## Reference project to inspect

`S95F/instrumentTutor` (formerly `guitarTutor`) is a useful MIT-licensed
reference for the audio/DSP stages. Inspect rather than blindly fork: RiffHero
intentionally has a narrower product scope.
