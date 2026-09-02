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
