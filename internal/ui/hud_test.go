package ui

import (
	"math"
	"strings"
	"testing"

	"github.com/FullFran/riffhero/internal/practice"
)

func hudFixture() HUDInput {
	clock := practice.Clock{SampleRate: 48000}
	return HUDInput{
		Clock:  clock,
		Grid:   practice.BuildGrid(clock, 0, []practice.Section{{BPM: 120, Sig: practice.CommonTime, Bars: 8}}),
		Tuning: practice.StandardTuning,
		Title:  "Test Riff",
		Artist: "Nobody",
		Track:  "Lead",
		End:    clock.Frames(16),
		Speed:  1,
	}
}

func TestNoteName(t *testing.T) {
	cases := map[uint8]string{
		40: "E2", 45: "A2", 60: "C4", 64: "E4", 69: "A4", 88: "E6",
	}
	for midi, want := range cases {
		if got := NoteName(midi); got != want {
			t.Fatalf("MIDI %d = %q, want %q", midi, got, want)
		}
	}
}

func TestStringLabelsFollowTheTuning(t *testing.T) {
	// Hard-coding "eBGDAE" would lie about a drop-D score, which is exactly
	// the score someone is most likely to be practising.
	if got := StringLabels(practice.StandardTuning); got != [6]string{"e", "B", "G", "D", "A", "E"} {
		t.Fatalf("standard tuning labelled %v", got)
	}
	got := StringLabels(practice.DropDTuning)
	if got[5] != "D" {
		t.Fatalf("drop D's bottom string is labelled %q, want D", got[5])
	}
}

func TestTimecode(t *testing.T) {
	clock := practice.Clock{SampleRate: 48000}
	cases := map[float64]string{
		0:    "0:00.0",
		9.5:  "0:09.5",
		61.2: "1:01.2",
	}
	for secs, want := range cases {
		if got := Timecode(clock, clock.Frames(secs)); got != want {
			t.Fatalf("%.1fs = %q, want %q", secs, got, want)
		}
	}
	if got := Timecode(clock, -100); got != "0:00.0" {
		t.Fatalf("a negative position rendered as %q", got)
	}
}

func TestCentsAlwaysCarriesASign(t *testing.T) {
	if got := Cents(12.4); got != "+12¢" {
		t.Fatalf("got %q", got)
	}
	if got := Cents(-3); got != "-3¢" {
		t.Fatalf("got %q", got)
	}
}

func TestMeterLevelIsLinearInDecibels(t *testing.T) {
	// Linear in amplitude would spend nine tenths of the bar on the top 20 dB
	// and never tell the player whether their pickup is connected.
	if got := MeterLevel(0); got != 1 {
		t.Fatalf("full scale = %v", got)
	}
	if got := MeterLevel(MeterFloorDB / 2); math.Abs(got-0.5) > 1e-9 {
		t.Fatalf("halfway down the scale = %v, want 0.5", got)
	}
	if got := MeterLevel(-200); got != 0 {
		t.Fatalf("far below the floor = %v", got)
	}
	if got := MeterLevel(math.Inf(-1)); got != 0 {
		t.Fatalf("digital silence = %v", got)
	}
	if got := MeterLevel(6); got != 1 {
		t.Fatalf("above full scale = %v, want a clamped 1", got)
	}
}

func TestBar(t *testing.T) {
	if got := Bar(0.5, 10); got != "#####....." {
		t.Fatalf("got %q", got)
	}
	if got := Bar(-1, 4); got != "...." {
		t.Fatalf("got %q", got)
	}
	if got := Bar(2, 4); got != "####" {
		t.Fatalf("got %q", got)
	}
	if got := Bar(0.5, 0); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestHUDTitleCarriesTheContext(t *testing.T) {
	h := BuildHUD(hudFixture())
	for _, want := range []string{"Test Riff", "Nobody", "Lead", "Standard"} {
		if !strings.Contains(h.Title, want) {
			t.Fatalf("title %q is missing %q", h.Title, want)
		}
	}
}

func TestHUDTitleFallsBackToTheAppName(t *testing.T) {
	in := hudFixture()
	in.Title, in.Artist, in.Track = "", "", ""
	in.Tuning = practice.Tuning{}
	if got := BuildHUD(in).Title; got != "RiffHero" {
		t.Fatalf("got %q", got)
	}
}

func TestHUDTransportShowsBarAndBeat(t *testing.T) {
	in := hudFixture()
	in.Position = in.Clock.Frames(2.6) // bar 2, beat 2 at 120 BPM in 4/4
	in.Playing = true

	h := BuildHUD(in)
	if !strings.Contains(h.Transport, "PLAYING") {
		t.Fatalf("transport %q", h.Transport)
	}
	if !strings.Contains(h.Transport, "bar 2.2") {
		t.Fatalf("transport %q should locate bar 2 beat 2", h.Transport)
	}
}

func TestHUDTransportOutsideTheGrid(t *testing.T) {
	in := hudFixture()
	in.Position = in.Clock.Frames(999)
	if !strings.Contains(BuildHUD(in).Transport, "bar -") {
		t.Fatalf("a position outside the grid should say so")
	}
}

func TestHUDLoopReadsInBars(t *testing.T) {
	// A player thinks "loop bars 3 to 4", not "loop 4.0 s to 8.0 s".
	in := hudFixture()
	in.Loop = practice.Loop{A: in.Clock.Frames(4), B: in.Clock.Frames(8), Enabled: true}

	if got := BuildHUD(in).Practice; !strings.Contains(got, "bars 3-4") {
		t.Fatalf("practice line %q", got)
	}
}

func TestHUDLoopOff(t *testing.T) {
	if got := BuildHUD(hudFixture()).Practice; !strings.Contains(got, "loop off") {
		t.Fatalf("practice line %q", got)
	}
}

func TestHUDLoopArmedWithOnlyOneEndSet(t *testing.T) {
	// Pressing A without B yet is a real state and the player needs to see it,
	// or it looks as though the key did nothing.
	in := hudFixture()
	in.Loop = practice.Loop{A: in.Clock.Frames(4)}
	if got := BuildHUD(in).Practice; !strings.Contains(got, "armed") {
		t.Fatalf("practice line %q", got)
	}
}

func TestHUDScoreLine(t *testing.T) {
	in := hudFixture()
	in.Stats = practice.SessionStats{Total: 20, Resolved: 10, Perfect: 7, Good: 2, Miss: 1, Combo: 3, MaxCombo: 6, Accuracy: 0.9}

	got := BuildHUD(in).Score
	for _, want := range []string{"accuracy  90%", "combo x3", "best x6", "perfect 7", "good 2", "miss 1", "10/20"} {
		if !strings.Contains(got, want) {
			t.Fatalf("score line %q is missing %q", got, want)
		}
	}
}

func TestHUDInputLineSaysWhenThereIsNoGuitar(t *testing.T) {
	if got := BuildHUD(hudFixture()).Input; !strings.Contains(got, "scripted") {
		t.Fatalf("input line %q", got)
	}
}

func TestHUDInputLineShowsTheDetectedPitch(t *testing.T) {
	in := hudFixture()
	in.Live = true
	in.Level = -18
	in.HasDetected = true
	in.Detected = practice.DetectedNote{MIDI: 45, CentsError: -7, Confidence: 0.82}
	in.Latency = in.Clock.Frames(0.012)

	got := BuildHUD(in).Input
	for _, want := range []string{"A2", "-7¢", "82%", "12 ms"} {
		if !strings.Contains(got, want) {
			t.Fatalf("input line %q is missing %q", got, want)
		}
	}
}

func TestHUDWarnsAboutDroppedSamples(t *testing.T) {
	// From the player's side a dropped sample is indistinguishable from their
	// own mistake, so it has to be said out loud.
	in := hudFixture()
	in.Live = true
	in.Latency = 100
	in.Dropped = 4096

	warnings := BuildHUD(in).Warnings
	if len(warnings) != 1 || !strings.Contains(warnings[0], "4096") {
		t.Fatalf("warnings %v", warnings)
	}
}

func TestHUDWarnsAboutUnderrunsAndUncalibratedLatency(t *testing.T) {
	in := hudFixture()
	in.Live = true
	in.Underruns = 3

	joined := strings.Join(BuildHUD(in).Warnings, " | ")
	if !strings.Contains(joined, "underrun") {
		t.Fatalf("warnings %q", joined)
	}
	if !strings.Contains(joined, "calibrat") {
		t.Fatalf("warnings %q should mention calibration", joined)
	}
}

func TestHUDIsQuietWhenNothingIsWrong(t *testing.T) {
	in := hudFixture()
	in.Live = true
	in.Latency = 480
	if got := BuildHUD(in).Warnings; len(got) != 0 {
		t.Fatalf("warnings %v, want none", got)
	}
}

func TestHUDRatingOnlyAppearsOnceSomethingResolved(t *testing.T) {
	in := hudFixture()
	if got := BuildHUD(in).Rating; got != "" {
		t.Fatalf("rating %q before anything was played", got)
	}
	in.HasRatng, in.Rating = true, practice.Good
	if got := BuildHUD(in).Rating; got != "GOOD" {
		t.Fatalf("rating %q", got)
	}
}

func TestLoopBounds(t *testing.T) {
	clock := practice.Clock{SampleRate: 48000}
	l := NewLayout(1000, 600, clock)

	loop := practice.Loop{A: clock.Frames(1), B: clock.Frames(2), Enabled: true}
	x1, x2, ok := l.LoopBounds(clock.Frames(1), loop)
	if !ok {
		t.Fatal("a region at the playhead must be visible")
	}
	if x1 != l.PlayheadX {
		t.Fatalf("region start at %v, want the playhead %v", x1, l.PlayheadX)
	}
	if x2 <= x1 {
		t.Fatalf("region runs backwards: %v..%v", x1, x2)
	}

	if _, _, ok := l.LoopBounds(clock.Frames(600), loop); ok {
		t.Fatal("a region long past should not be visible")
	}
	if _, _, ok := l.LoopBounds(0, practice.Loop{A: 1, B: 2}); ok {
		t.Fatal("an inactive region is never visible")
	}
}

func TestVisibleBarsCoversTheScreenAndNoMore(t *testing.T) {
	clock := practice.Clock{SampleRate: 48000}
	l := NewLayout(1000, 600, clock)
	grid := practice.BuildGrid(clock, 0, []practice.Section{{BPM: 120, Sig: practice.CommonTime, Bars: 60}})

	bars := l.VisibleBars(clock.Frames(30), grid)
	if len(bars) == 0 {
		t.Fatal("no bars visible in the middle of the song")
	}
	from, to := l.VisibleWindow(clock.Frames(30))
	for _, b := range bars {
		if b.End < from || b.Start > to {
			t.Fatalf("bar %d (%d..%d) is off screen (%d..%d)", b.Number, b.Start, b.End, from, to)
		}
	}
	if len(bars) >= len(grid) {
		t.Fatalf("%d of %d bars reported visible; the window is not bounding anything", len(bars), len(grid))
	}
}
