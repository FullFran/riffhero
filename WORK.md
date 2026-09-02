# Work handoff

## Objective

Build the smallest useful Linux-first real-guitar practice game in Go.

The first usable vertical slice is:

```text
score timeline + backing clock + guitar note event -> Perfect / Good / Miss
```

Do not broaden scope before this loop works.

## Current state

Phase 0 is complete and Phase 1's DSP is complete. Everything below builds and
tests with no display and no audio device.

`internal/practice` — the domain:

- sample-frame time model (`Frame`, `Clock`);
- normalized note/event model;
- synthetic 20-note score generated from the clock (`SyntheticSong`);
- frame-driven `Transport` (play/pause/seek/restart, clamped at the song end);
- `ScriptedDetector` + `Perform` to simulate a player from a deviation plan;
- `Session`: resolves each expected note exactly once, expires late notes as
  Miss, tracks Perfect/Good/Miss, combo and accuracy;
- headless `ui.Layout` mapping frames and strings to screen coordinates;
- Ebitengine view wiring all of the above, with a scrolling tab and HUD.

`internal/dsp` — real audio to notes, no hardware yet:

- `Ring`: bounded SPSC queue; the audio callback's only job is to copy into it,
  and a full ring drops rather than blocks;
- `Gate`: RMS gate with hysteresis, so a decaying note does not chatter it;
- `Onset`: energy-rise detector that back-dates an onset to where the rise
  began;
- `MPM`: McLeod Pitch Method, the primary estimator;
- `YIN`: independent cross-check; agreement raises confidence, disagreement
  halves it;
- `Tracker`: onset-driven note assembly, requiring a quorum of windows to agree
  before a note exists;
- `Detector`: implements `practice.Detector`, so real detections drop straight
  into the Phase 0 `Session`.

Scoring rules worth knowing:

- a detection with the wrong pitch or bad intonation consumes nothing, so the
  player can still rescue the note inside its window;
- a detection is assigned to the *nearest* unresolved note it can legitimately
  hit, not the first one;
- a note expires as Miss once the playhead passes `Start + Good`;
- accuracy is `(Perfect + Good) / resolved`.

DSP decisions worth knowing, because each one was a bug first:

- onset detection compares against the *minimum* of the recent hops, not their
  average. On a guitar the previous note is still ringing when the next is
  struck, so energy adds instead of replacing, and an average is dragged up
  until it swallows the attack;
- the envelope behind that rule is measured over four hops, not one. A
  256-sample hop holds half a cycle of the low E, so its RMS tracks the phase
  of the waveform rather than its loudness, and a minimum-based rule reads that
  swing as an attack several times per note;
- MPM's search for the first key maximum starts at lag zero, not at the minimum
  lag of the pitch range. At the top of the range the minimum lag falls inside
  the first hump of the NSDF, and starting there skips the true peak and
  returns the octave below it;
- `Detector.Write` must be paired with regular `Poll` calls. The ring holds
  about 0.7 s; dumping more than that without polling drops samples, and
  `Detector.Dropped()` is how that gets noticed;
- nothing in the chain may judge level on a single hop. The gate used to, and
  it vetoed onsets: a low E whose first hop happened to land on a quiet part of
  its own waveform was discarded, and because the onset had already started its
  refractory window the attack could not fire again. The same note was heard on
  one take and ignored on the next. The gate now reads `Onset.Level`, the
  shared multi-hop envelope, and no longer vetoes anything — the onset's own
  floor is the single authority on "too quiet to be a note";
- a dropped sample is a hole in the timeline, not just lost audio. The tracker
  counts what it reads and the game counts what elapsed, so one ring overflow
  used to shift every later detection earlier for the rest of the run and turn
  the whole song into misses. `Poll` now feeds the gap to `Tracker.Skip`;
- windows where MPM and YIN disagreed do not count as ordinary evidence. A
  semitone supported only by disputed windows must be unanimous before it is
  believed, which is the only thing that makes the cross-check change an
  outcome rather than just a number;
- the quorum tie-break is explicit (support, then clarity, then lowest pitch).
  Ranking by map iteration alone would pass locally and flake in CI.

### Building and running

`make check` covers vet + tests for `internal/...` and needs nothing but Go —
every test in the repo lives there and is hardware-free. `make build` and
`make run` compile `cmd/riffhero`, which needs the X11/GL development headers
that `make deps` prints the install line for; `make check-app` vets that
package too.

Two environment traps cost a session once, both outside the code:

1. Without the X11/GL headers the cgo build fails with
   `fatal error: X11/Xlib.h: No such file or directory`. Install what
   `make deps` prints.
2. On a machine with Nix-based tooling in the shell, the binary builds but the
   window never opens. Nix exports driver-discovery variables pointing at
   `/nix/store` builds linked against a newer glibc than the system one:
   `LD_LIBRARY_PATH` yields ``libGL.so: version `GLIBC_2.38' not found``, and
   `__EGL_VENDOR_LIBRARY_FILENAMES` yields
   `glfw: EGL: Failed to get EGL display`. The second is read by libglvnd on
   its own, so clearing `LD_LIBRARY_PATH` alone does not help — Ebitengine
   goes through EGL on Linux, not GLX. `scripts/with-system-gl.sh` clears both
   plus the related driver paths, and `make run` goes through it.

Phase 0 has been confirmed on a real display: the view renders, the transport
runs, and the scripted performance scores 10 Perfect / 5 Good / 5 Miss over the
20-note song, exactly as `performance()` in `cmd/riffhero/main.go` plans it.

## Next implementation session

Finish Phase 1 by giving the DSP a real source, then move to Phase 2.

1. Evaluate `gen2brain/malgo` duplex capture and measure latency on PipeWire.
   It is the first third-party dependency in the project, so weigh it against
   raw ALSA before committing.
2. Wire the capture callback to `dsp.Detector.Write` and nothing else — no
   analysis on the callback thread.
3. Add device selection, and surface `Detector.Dropped()` in the UI, because a
   silent drop looks exactly like a player missing notes.
4. Measure round-trip latency and store it in `Detector.LatencyOffset`.
5. Replace `ScriptedDetector` in `cmd/riffhero` with `dsp.Detector` behind a
   flag, so the scripted path stays available for testing without a guitar.

Known limits to revisit rather than forget:

- 65 ms of analysis latency, dominated by the 2048-sample window the low E
  needs. A shorter window for higher notes would cut it, but only if the
  practice loop actually feels slow.
- Monophonic only. Two strings ringing together will not resolve; that is
  Phase 5, and the tracker's quorum rule makes it fail silently rather than
  emit a wrong note.
- The synthetic `pluck` fixtures are a model of a string, not a recording. DI
  fixtures are the honest next test.

## Reference project to inspect

`S95F/instrumentTutor` (formerly `guitarTutor`) is a useful MIT-licensed reference for the audio/DSP stages. Inspect rather than blindly fork: RiffHero intentionally has a narrower product scope.

## Design constraints

- Linux first.
- Go first; small cgo boundaries are acceptable when justified.
- Ebitengine UI.
- One authoritative timeline in sample frames.
- Real-time callback stays tiny and non-blocking.
- MPM/YIN for single notes.
- Chords later via expected-spectrum/chroma verification, not unconstrained transcription.
- Backing time-stretch later via Signalsmith Stretch if needed.
- No backend, accounts, ML transcription, amp simulator or 3D highway in MVP.

## Definition of useful v0.1

A user can open a song, choose an input, hear a backing track, play a real guitar, loop a section and see reliable timing/pitch feedback with low enough latency that practice feels natural.
