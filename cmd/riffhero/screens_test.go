package main

import (
	"strings"
	"testing"

	"github.com/FullFran/riffhero/internal/config"
	"github.com/FullFran/riffhero/internal/practice"
	"github.com/FullFran/riffhero/internal/ui"
)

func TestWrapTextBreaksAtSpaces(t *testing.T) {
	got := wrapText("the quick brown fox jumps over the lazy dog", 12)
	for i, line := range got {
		if len(line) > 12 {
			t.Fatalf("line %d is %d characters: %q", i, len(line), line)
		}
	}
	if joined := strings.Join(got, " "); joined != "the quick brown fox jumps over the lazy dog" {
		t.Fatalf("wrapping lost or gained words: %q", joined)
	}
}

func TestWrapTextKeepsAWordTooLongForTheLine(t *testing.T) {
	// Losing a word is worse than overflowing by one: a file name or an error
	// message is exactly where a long unbroken run turns up.
	got := wrapText("short supercalifragilistic", 10)
	if len(got) != 2 || got[1] != "supercalifragilistic" {
		t.Fatalf("got %q", got)
	}
}

func TestWrapTextOnNothing(t *testing.T) {
	if got := wrapText("", 20); len(got) != 0 {
		t.Fatalf("got %q", got)
	}
}

func TestClipMarksWhatItCut(t *testing.T) {
	// A file name that runs off the edge of its row is worse than one that
	// admits it is too long.
	if got := clip("short", 20); got != "short" {
		t.Fatalf("got %q", got)
	}
	got := clip("a considerably longer name", 10)
	if len([]rune(got)) != 10 {
		t.Fatalf("clipped to %d runes: %q", len([]rune(got)), got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("%q does not say it was cut", got)
	}
	if got := clip("abc", 0); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestClipCountsRunesNotBytes(t *testing.T) {
	// A song title with an accent in it must not be cut mid-character.
	if got := clip("canción", 10); got != "canción" {
		t.Fatalf("got %q", got)
	}
}

func TestMenuColumnStaysOnScreen(t *testing.T) {
	// A tiling desktop hands the window whatever shape it likes, so nothing in
	// a menu is a fixed coordinate.
	for _, size := range [][2]int{{1280, 720}, {677, 723}, {400, 300}, {1920, 1080}} {
		a := &app{width: size[0], height: size[1]}
		x, w, top, pitch := a.menuColumn(6)

		if x < 0 || x+w > float64(size[0]) {
			t.Fatalf("%v: column runs from %v to %v", size, x, x+w)
		}
		if top < 0 {
			t.Fatalf("%v: column starts above the window at %v", size, top)
		}
		if pitch <= 0 {
			t.Fatalf("%v: pitch %v", size, pitch)
		}
	}
}

func TestShortPathKeepsEnoughToTellSongsApart(t *testing.T) {
	if got := shortPath(""); got != "built-in phrase" {
		t.Fatalf("got %q", got)
	}
	got := shortPath("/home/me/music/metallica/one.gp")
	if got != "metallica/one.gp" {
		t.Fatalf("got %q", got)
	}
}

func TestPercent(t *testing.T) {
	cases := map[float64]string{0: "0%", 0.5: "50%", 1: "100%", 0.35: "35%"}
	for v, want := range cases {
		if got := percent(v); got != want {
			t.Fatalf("%v = %q, want %q", v, got, want)
		}
	}
}

func TestStaffFitsWhateverBandItIsGiven(t *testing.T) {
	for _, band := range [][2]float64{{100, 400}, {100, 130}, {0, 1000}} {
		s := staffFor(band[0], band[1])
		if s.LineY(0) < band[0]-1 {
			t.Fatalf("band %v: top line at %v", band, s.LineY(0))
		}
		if s.LineY(4) > band[1]+1 {
			t.Fatalf("band %v: bottom line at %v", band, s.LineY(4))
		}
		if s.LineGap < 5 {
			t.Fatalf("band %v: lines %v apart, too close to read", band, s.LineGap)
		}
	}
}

func TestBarBPMFollowsATempoChange(t *testing.T) {
	clock := practice.Clock{SampleRate: 48000}
	a := &app{
		clock: clock,
		song: &practice.Song{
			Clock: clock,
			Grid: practice.BuildGrid(clock, 0, []practice.Section{
				{BPM: 120, Sig: practice.CommonTime, Bars: 2},
				{BPM: 60, Sig: practice.CommonTime, Bars: 2},
			}),
		},
	}

	if got := a.barBPM(clock.Frames(1)); got != 120 {
		t.Fatalf("bar 1 is at %v BPM", got)
	}
	if got := a.barBPM(clock.Frames(5)); got != 60 {
		t.Fatalf("bar 3 is at %v BPM", got)
	}
	// Past the end there is no bar; the first one's tempo is a better guess
	// than dividing by zero.
	if got := a.barBPM(clock.Frames(999)); got != 120 {
		t.Fatalf("past the end gave %v", got)
	}
	if got := (&app{song: &practice.Song{}}).barBPM(0); got != 120 {
		t.Fatalf("a song with no grid gave %v", got)
	}
}

func TestNotationSwitchesWhatIsDrawn(t *testing.T) {
	a := &app{}
	for _, c := range []struct {
		mode      string
		tab, staf bool
	}{
		{"tab", true, false},
		{"staff", false, true},
		{"both", true, true},
	} {
		a.notation = config.Notation(c.mode)
		if a.showsTab() != c.tab || a.showsStaff() != c.staf {
			t.Fatalf("%s shows tab=%v staff=%v", c.mode, a.showsTab(), a.showsStaff())
		}
	}
}

func TestSpellingFor(t *testing.T) {
	if got := spellingFor("flats"); got != ui.Flats {
		t.Fatalf("got %v", got)
	}
	for _, name := range []string{"sharps", "", "nonsense"} {
		if got := spellingFor(name); got != ui.Sharps {
			t.Fatalf("%q gave %v", name, got)
		}
	}
}
