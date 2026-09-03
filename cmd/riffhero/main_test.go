package main

import (
	"strings"
	"testing"

	"github.com/FullFran/riffhero/internal/practice"
)

func TestParseBarRange(t *testing.T) {
	cases := []struct {
		spec      string
		from, to  int
		wantError bool
	}{
		{spec: "9-12", from: 9, to: 12},
		{spec: " 1 - 4 ", from: 1, to: 4},
		{spec: "3-3", from: 3, to: 3},
		{spec: "12", wantError: true},
		{spec: "", wantError: true},
		{spec: "a-b", wantError: true},
		{spec: "0-4", wantError: true}, // bars are one-based, as a musician counts
		{spec: "8-4", wantError: true}, // backwards is a typo, not a request
		{spec: "-4", wantError: true},
	}
	for _, c := range cases {
		from, to, err := parseBarRange(c.spec)
		if c.wantError {
			if err == nil {
				t.Fatalf("%q should not parse", c.spec)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%q: %v", c.spec, err)
		}
		if from != c.from || to != c.to {
			t.Fatalf("%q = %d-%d, want %d-%d", c.spec, from, to, c.from, c.to)
		}
	}
}

func TestParseBarRangeErrorsSayHow(t *testing.T) {
	// An error that only says "invalid" leaves the reader guessing at a
	// format they have never seen.
	_, _, err := parseBarRange("nine to twelve")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "9-12") {
		t.Fatalf("the error should show the shape: %v", err)
	}
}

func TestPickPrefersTheFlag(t *testing.T) {
	if got := pick("flag", "stored"); got != "flag" {
		t.Fatalf("got %q", got)
	}
	if got := pick("", "stored"); got != "stored" {
		t.Fatalf("got %q", got)
	}
	if got := pick("", ""); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestScriptedPerformanceShowsEveryRating(t *testing.T) {
	// The scripted player exists so the whole loop can be seen working with no
	// hardware. If it only ever produced hits it would prove nothing.
	clock := practice.Clock{SampleRate: 48000}
	song := practice.SyntheticSong(clock)
	plan := scriptedPerformance(clock, len(song))

	if len(plan) != len(song) {
		t.Fatalf("plan covers %d of %d notes", len(plan), len(song))
	}

	session := practice.NewSession(song, practice.SessionConfig{
		Windows:  practice.TimingWindows{Perfect: clock.Frames(0.050), Good: clock.Frames(0.110)},
		MaxCents: 35,
	})
	for _, d := range practice.Perform(song, plan) {
		session.Feed(d)
	}
	session.Advance(practice.SongEnd(song) + clock.Frames(1))

	st := session.Stats()
	if st.Perfect == 0 || st.Good == 0 || st.Miss == 0 {
		t.Fatalf("the demo performance does not show all three ratings: %+v", st)
	}
	if st.Resolved != st.Total {
		t.Fatalf("%d of %d notes resolved", st.Resolved, st.Total)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Fatalf("got %q", got)
	}
	if got := truncate("a very long instrument name", 10); len([]rune(got)) != 10 {
		t.Fatalf("got %q, want ten runes", got)
	}
	if got := truncate("abc", 1); got != "a" {
		t.Fatalf("got %q", got)
	}
	if got := truncate("abc", 0); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatSeconds(t *testing.T) {
	cases := map[float64]string{0: "0:00.0", 9.5: "0:09.5", 125.25: "2:05.2"}
	for secs, want := range cases {
		if got := formatSeconds(secs); got != want {
			t.Fatalf("%.2f = %q, want %q", secs, got, want)
		}
	}
}

func TestReplayingDetectorPlaysTheSectionAgainEveryLap(t *testing.T) {
	// A ScriptedDetector emits each detection once and then goes quiet, so the
	// second lap of a region scored nothing — not because the scoring was
	// wrong but because nobody was playing. The demo has to repeat the section
	// or it shows the opposite of what it is for.
	events := []practice.DetectedNote{
		{Onset: 100, MIDI: 40, Confidence: 1},
		{Onset: 200, MIDI: 45, Confidence: 1},
		{Onset: 300, MIDI: 50, Confidence: 1},
	}
	d := newReplayingDetector(events)

	first := len(d.Poll(150)) + len(d.Poll(400))
	if first != 3 {
		t.Fatalf("first pass emitted %d detections, want 3", first)
	}
	if got := d.Poll(400); len(got) != 0 {
		t.Fatalf("a settled playhead emitted %d more", len(got))
	}

	// The playhead wraps back to the start of the region.
	second := len(d.Poll(50)) + len(d.Poll(400))
	if second != 3 {
		t.Fatalf("second lap emitted %d detections, want 3", second)
	}
}

func TestReplayingDetectorDiscardsWhatIsBehindASeek(t *testing.T) {
	// Jumping into the middle of a region must not dump every earlier
	// detection at once; they belong to music that was skipped.
	events := []practice.DetectedNote{
		{Onset: 100, MIDI: 40}, {Onset: 200, MIDI: 45}, {Onset: 5000, MIDI: 50},
	}
	d := newReplayingDetector(events)
	d.Poll(6000) // play the lot

	if got := d.Poll(4000); len(got) != 0 {
		t.Fatalf("seeking back to 4000 replayed %d detections from before it", len(got))
	}
	if got := d.Poll(6000); len(got) != 1 || got[0].MIDI != 50 {
		t.Fatalf("got %+v, want only the note after the seek", got)
	}
}
