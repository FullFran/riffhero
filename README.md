# RiffHero

Minimal Linux-first guitar practice game in Go.

The goal is deliberately small:

> load a score + backing track, plug in a real guitar, play, and get immediate timing/pitch feedback.

## MVP

- Native Linux desktop app.
- Ebitengine UI.
- One sample-frame clock for score, backing and guitar input.
- Real guitar capture through a low-latency duplex audio backend.
- Monophonic pitch detection first (MPM/YIN + onset gating).
- Score matching: Perfect / Good / Miss.
- A/B loop and speed control.
- Guitar Pro 7/8, MusicXML or MIDI ingestion via adapters.
- Backing WAV/FLAC/MP3.

Not in v0.1: accounts, backend, DAW features, amp simulation, ML transcription, 3D highway.

## Target UX

```text
riffhero song.gp --backing song.flac

E ─────────────────────────────────────────────
B ─────────7────────8────10────────────────────
G ───7──9──────────────────────────────────────
D ─────────────────────────────────────────────
A ─────────────────────────────────────────────
E ─────────────────────────────────────────────
                    │
                    ▼
               PERFECT

accuracy 93%    combo x24    speed 0.75x
```

## Development

```bash
go mod tidy
go run ./cmd/riffhero
```

See [`PLAN.md`](PLAN.md) and [`docs/architecture.md`](docs/architecture.md).
