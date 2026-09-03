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
internal/dsp        ring, gate, onset, MPM, YIN, tracker, FFT, chord
                    verifier, and the Detector that joins them.
internal/audio      device, duplex engine, render goroutine, time map,
                    latency calibration.
internal/audio/codec  wav (by hand), mp3, flac, plus rate and channel
                    conversion.
internal/stretch    WSOLA, pure Go.
internal/score      gp / musicxml / midi importers behind one front door.
internal/config     ~/.config/riffhero/config.json.
internal/library    what is on disk that could be opened, for the picker.
internal/ui         tab and staff geometry and the HUD, all headless.
cmd/riffhero        flags, wiring, the screens, and the Ebitengine view.
```

### The screens

`cmd/riffhero` follows the shape of `inflation-rpg-go`: a `mode`, an
`openX`/`updateX`/`drawX` per screen, and widgets as **values** rather than
objects with state — where a button is and what it says are worked out afresh
every frame from the app, so one can never show something the app no longer
holds. Every button carries the key that presses it, because RiffHero is used
with a guitar in both hands.

The rows of a screen are a data table (`titleRows`, `settingRows`), which is
what keeps the update and the draw agreeing about what is on screen and where.
Add a setting by adding a row.

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

Timing, from an adversarial review of the finished app:

- a lap of the A-B region is counted where it is **heard**, in the audio
  callback, not where the renderer wraps its cursor. The renderer runs a whole
  output buffer ahead — about 70 ms — so counting there reset the scoreboard
  while the playhead was still short of B, and the scoring session then expired
  the entire fresh lap as misses. Every lap after the first scored 0%, and with
  the progressive rule on the speed fell to the floor: the exact inverse of the
  feature;
- the runner reads the lap count before the position, and the playhead
  publishes them in that order. Reversed, a new lap would rebaseline to a
  position at the end of the region and nothing would ever expire again;
- **playing through a note and jumping over it are not the same thing.**
  `Session.Advance` used to expire everything behind the playhead, so a seek,
  End, or a track change resolved every note skipped as a miss. The session is
  told where a jump landed and leaves alone anything whose window closed before
  it;
- the strum tolerance is not the scoring window. Sharing one number sent
  sixteenths at 150 BPM — 100 ms apart, inside the Good window — through the
  chord verifier as if they were chords, at 171 ms of latency and one shared
  onset;
- the tablature and the pitch of a note must agree. Two importers could produce
  one that disagreed, and the player can satisfy neither.

The interface, found by walking the screens rather than reading the code:

- a line of explanation drawn *between* two buttons is painted over by the
  second one. Draw all the buttons, then all the notes;
- a fixed row pitch runs off the bottom of a half-screen window, and the rows
  that fall off are the ones nobody then knows exist. The settings screen
  divides whatever room it has, and drops the explanations before it drops a
  row;
- a thick line with square caps is a rectangle. A note head is a circle;
- the desktop's folder names are localised. Guessing "Music" finds nothing on a
  Spanish desktop, where it is Música — the real names are in
  `~/.config/user-dirs.dirs`.

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
  about a third — low enough to be refused as noise;
- `Seek` bumps the generation before it stores the position. The other order
  leaves a window where a callback still matches the old generation, passes the
  staleness guard, and puts the pre-seek position back;
- the loop crossfade divides by its own length, not by the nominal one. A
  region whose length is not a multiple of the render chunk leaves a short
  final chunk, and the ramp then opens partway down — a step, which is the
  click the fade exists to remove;
- the audio **host** is opened once for the life of the process and the
  **device** as often as you like. That asymmetry is the miniaudio bug above,
  and it is what makes a device picker possible at all: closing a device and
  opening another works, and goes on working after one that would not open;
- averaging a multi-input capture is right for a microphone and wrong for a
  guitar in socket one — half the level, plus the empty socket's hum. Nothing
  in the audio distinguishes the two cases, so the channel is a setting and the
  device screen meters each input so the answer is visible.

Domain:

- a detection with the wrong pitch or bad intonation consumes nothing, so the
  player can still rescue the note inside its window;
- a detection is assigned to the *nearest* unresolved note it can legitimately
  hit, not the first;
- accuracy is `(Perfect + Good) / resolved`, and scoring is scoped to the loop
  region — otherwise every note outside it expires as a miss;
- a lap that resolved nothing is not evidence for the progressive rule;
- a sample rate read out of a file header is not to be trusted. Every
  conversion divides by it, so 1 Hz turns a two-megabyte WAV into a
  fifty-billion-frame allocation and a `runtime: out of memory` throw that no
  recover can catch, and 0 Hz silently reduces a whole track to one frame.

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
| Clock drift over three seconds of duplex playback | 0.9966x, 0 underruns, 0 dropped |
| A-B lap counted, relative to the wrap | within 5 ms |
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
3. **Da Capo and Dal Segno**, which are detected but not followed.
4. **Seven- and eight-string tracks**, which are skipped because
   `practice.Tuning` is a fixed six-element array.
5. **A key signature.** The staff spells the black notes as sharps or as flats
   by a setting, because nothing in the score model carries a key. An importer
   that read one would let the notation be spelled properly.

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
