# Work handoff

## Objective

Build the smallest useful Linux-first real-guitar practice game in Go.

The first usable vertical slice is:

```text
score timeline + backing clock + guitar note event -> Perfect / Good / Miss
```

Do not broaden scope before this loop works.

## Current state

Initialized:

- Go module targeting Go 1.26.
- Ebitengine as the UI dependency.
- sample-frame time model;
- normalized note/event model;
- deterministic single-note matcher;
- matcher unit tests;
- minimal Ebitengine window scaffold;
- architecture and phased implementation plan.

## First implementation session

Complete Phase 0 from `PLAN.md`:

1. Create a synthetic 20-note guitar phrase in `internal/practice`.
2. Add a deterministic transport driven by sample frames.
3. Add a fake detector that emits note events at configurable offsets.
4. Match each emitted note once; avoid double scoring.
5. Render six strings, moving expected notes, playhead, latest rating, accuracy and combo.
6. Add tests for early/late boundary conditions and duplicate detections.
7. Keep the domain runnable/testable without Ebitengine.

Do **not** add malgo/audio capture yet. Phase 0 must be deterministic before hardware enters the system.

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
