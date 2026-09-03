# RiffHero

Minimal Linux-first guitar practice game in Go.

> Load a score and a backing track, plug in a real guitar, play, and get
> immediate timing and pitch feedback.

```text
riffhero song.gp --backing song.flac --loop 9-12 --progressive

e ─────────────────────────────────────────────
B ─────────7────────8────10────────────────────
G ───7──9──────────────────────────────────────
D ─────────────────────────────────────────────
A ─────────────────────────────────────────────
E ─────────────────────────────────────────────
                    │
                    ▼
               PERFECT

accuracy 93%    combo x24    speed 0.75x    loop bars 9-12
```

## What works

- **Real guitar in.** One duplex stream, so what you hear and what the scorer
  sees run on the same clock — measured at 0.9966x of real time over three
  seconds, with no underruns and nothing dropped.
- **Monophonic detection** with McLeod's method cross-checked against YIN, an
  onset detector that back-dates the attack, and a stability quorum so the
  score never sees a note you did not play. 65 ms of analysis latency.
- **Chords**, verified rather than transcribed: the score says which pitches to
  expect and the spectrum is asked whether they are there. Power chords, open
  voicings and barre chords score; the wrong chord scores 0.29 where the right
  one scores 0.79.
- **Backing tracks** in WAV, MP3 and FLAC, slowed to 0.25x without dropping
  pitch (WSOLA, pure Go — a 440 Hz tone comes back at 440.0000 Hz at half
  speed).
- **Real scores**: Guitar Pro 7/8 (`.gp`), MusicXML (`.musicxml`, `.mxl`,
  `.xml`) and MIDI, all normalized into one model. Guitar Pro is preferred
  because it is the only one carrying real tablature; repeats and alternate
  endings are expanded so the practice timeline is linear.
- **A-B loop** snapped to beats, selected live or from the command line, with
  the scoreboard scoped to the region.
- **Progressive practice**: play a lap clean and the speed comes up on its own;
  struggle and it comes down.
- **Latency calibration** by click train, so a player who is dead on time is
  not told they are consistently late.
- **Standard notation** as well as tablature, or both at once. Written in
  treble clef sounding an octave down, the way guitar music is; the two
  readings scroll on one timeline, so a note is at the same place in each.
- **An interface**: a title screen, a file browser, a settings screen that
  reaches every setting there is, a device picker with a meter per input, and
  calibration without leaving the app.

## Getting started

```bash
make deps           # prints the system packages to install
sudo apt install …  # what it printed
make build
make run
```

That opens the app on its title screen, and everything else is reachable from
there: choose a song, choose a backing track, pick your interface, measure the
latency, switch between tablature and notation. Nothing has to be set on the
command line — the flags exist, and are listed below, but they are set before
the guitar is plugged in and the interesting questions only become answerable
afterwards.

### With an audio interface

1. Plug it in, then **SETTINGS → INPUT DEVICE**. If you opened the app first,
   **REFRESH** finds it.
2. On that screen each socket has its own meter. Play, watch which bar moves,
   and press **C** until "listening to" names it. A two-input box puts the
   guitar in one socket and leaves the other open; averaging the pair would
   halve the guitar and add the empty socket's hum.
3. **SETTINGS → MEASURE LATENCY**, or **5** from the title. Patch the output
   back into the input if you can — that measurement is worth 87% confidence
   against about 45% through a microphone.
4. Play.

No score runs the built-in phrase. No backing track keeps the timeline on the
same clock and plays silence. `--no-audio` replays a scripted performance, so
the whole loop can be seen working on a machine with no sound card:

```bash
make demo
```

And with no window either, which is how the whole loop is checked in a
terminal or in CI:

```bash
$ riffhero --dry-run --loop 1-2 --progressive --speed 0.5
Pentatonic warm-up
track 1 of 1: Guitar, Standard, 20 notes
3 bars, 0:09.7, 16 notes in scope

lap 1: 75% accuracy, repeat -> 0.50x
lap 2: 75% accuracy, repeat -> 0.50x

perfect 2  good 1  miss 1  extra 0
accuracy 75%  best combo x2  4 of 16 resolved
```

## Controls

Every button in the menus has its key printed on it, and the mouse works
everywhere the keyboard does. While playing:

| Key | |
| --- | --- |
| `ESC` | back to the menu |
| `SPACE` | play / pause |
| `R` | restart and clear the scoreboard |
| `←` `→` | seek a bar back / forward |
| `HOME` `END` | jump to the start / the end |
| `A` `B` | set the loop start / end at the playhead, snapped to a beat |
| `L` `X` | loop on-off / clear the loop |
| `[` `]` | practice speed down / up |
| `P` | progressive practice on / off |
| `-` `=` | backing track quieter / louder |
| `M` | guitar monitoring level |
| `TAB` | next track |
| `N` | tablature / notation / both |
| `S` | settings |
| `H` | help |

## Options

```text
riffhero [score] [flags]

  --backing PATH     backing track (wav, mp3, flac)
  --track N          which track of the score to practise
  --loop 9-12        practise a bar range
  --speed 0.75       initial practice speed, 0.25 to 1
  --progressive      start with the progressive rule on
  --input NAME       capture device, by name or id
  --output NAME      playback device
  --backend NAME     jack, pulseaudio or alsa
  --latency MS       override the stored round-trip measurement
  --monitor 0.4      mix this much guitar into the output
  --volume 0.8       backing track level
  --no-audio         no device at all; replay a scripted performance
  --no-playback      capture the guitar, open no output
  --list-devices     print the audio devices and exit
  --list-tracks      print the score's tracks and exit
  --calibrate        measure the round-trip latency, store it and exit
  --dry-run          run the practice loop with no window and no device,
                     print the scoreboard and exit
  --rate N           sample rate to ask the device for
```

Device selection, the measured latency and the last speed are remembered in
`~/.config/riffhero/config.json`.

## Calibration

Round-trip latency is the one number that cannot be derived, only measured, and
getting it wrong shifts every detection by a constant. `--calibrate` plays a
click train and listens for it.

It works two ways. Patch the output back into the input, or run it through the
sound card's own monitor, and the measurement is clean — 40 ms at 87%
confidence on the development machine, repeatable to under 3 ms. Played out
loud into a microphone it also works, at lower confidence, and includes the
speaker and the air along with the buffers. A measurement it cannot trust is
refused rather than stored.

## Development

```bash
make check        # vet + tests for internal/..., no display or audio device
make race         # the same under the race detector
make check-audio  # the tests that open a real device
make deps         # print the system packages cmd/riffhero needs
make build        # build the app (needs those packages)
make run          # build and launch; ARGS="song.gp" to pass flags
make demo         # the built-in phrase with a scripted player
make smoke        # the whole loop with no window and no device
make help         # list every target
```

Everything under `internal/` builds and tests with no display and no audio
device. The tests that need hardware live behind the `hardware` build tag and
are never part of `make check`.

`make run` goes through `scripts/with-system-gl.sh`, which pins the app to the
system OpenGL stack. Nix-based tooling in the shell otherwise redirects driver
discovery at `/nix/store` builds linked against a newer glibc, and the window
never opens.

See [`PLAN.md`](PLAN.md), [`WORK.md`](WORK.md) and
[`docs/architecture.md`](docs/architecture.md).

## Not in scope

Accounts, a backend, DAW features, amp simulation, neural transcription, a 3D
note highway. The product constraint is in `PLAN.md`: a feature earns its place
only by improving the practice loop.
