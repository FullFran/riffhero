# Work handoff

## Objective

Build the smallest useful Linux-first real-guitar practice game in Go.

The first usable vertical slice is:

```text
score timeline + backing clock + guitar note event -> Perfect / Good / Miss
```

Do not broaden scope before this loop works.

## Current state

Phase 0 is complete. `internal/practice` and `internal/ui` build and test with
no display and no audio device:

- sample-frame time model (`Frame`, `Clock`);
- normalized note/event model;
- synthetic 20-note score generated from the clock (`SyntheticSong`);
- frame-driven `Transport` (play/pause/seek/restart, clamped at the song end);
- `ScriptedDetector` + `Perform` to simulate a player from a deviation plan;
- `Session`: resolves each expected note exactly once, expires late notes as
  Miss, tracks Perfect/Good/Miss, combo and accuracy;
- headless `ui.Layout` mapping frames and strings to screen coordinates;
- Ebitengine view wiring all of the above, with a scrolling tab and HUD.

Scoring rules worth knowing:

- a detection with the wrong pitch or bad intonation consumes nothing, so the
  player can still rescue the note inside its window;
- a detection is assigned to the *nearest* unresolved note it can legitimately
  hit, not the first one;
- a note expires as Miss once the playhead passes `Start + Good`;
- accuracy is `(Perfect + Good) / resolved`.

### Known local blocker

`go build ./cmd/riffhero` fails on this machine with
`fatal error: X11/Xlib.h: No such file or directory`. This is a missing system
dependency, not a code problem — Ebitengine needs the X11/GL development
headers:

```bash
sudo apt install libx11-dev libxcursor-dev libxrandr-dev libxinerama-dev \
    libxi-dev libxxf86vm-dev libgl1-mesa-dev
```

The domain and layout packages are unaffected: `go test ./internal/...` passes.

## Next implementation session

Phase 1 from `PLAN.md`: real guitar input.

1. Install the X11/GL headers above and run `go run ./cmd/riffhero` once, to
   confirm the Phase 0 view on a real display.
2. Evaluate `gen2brain/malgo` duplex capture and measure latency on PipeWire.
3. Add a bounded SPSC ring buffer written only by the audio callback.
4. RMS gate + onset detector outside the callback.
5. MPM as the primary pitch estimator, YIN as a cross-check.
6. Feed the resulting `DetectedNote` values into the existing `Session` — the
   scoring side needs no changes; only the source of detections does.

The real detector must satisfy the same `practice.Detector` interface that
`ScriptedDetector` already implements, so Phase 0 tests stay the regression net.

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
