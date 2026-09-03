package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FullFran/riffhero/internal/config"
	"github.com/FullFran/riffhero/internal/library"
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
	// ASCII, because Ebiten's debug font stops at U+00FF: an ellipsis would
	// draw as three holes.
	if !strings.HasSuffix(got, ">") {
		t.Fatalf("%q does not say it was cut", got)
	}
	if got := clip("abc", 0); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestClipCountsRunesNotBytes(t *testing.T) {
	// A song title with an accent in it must not be cut mid-character. An
	// accent is inside Latin-1, so the font can draw it.
	if got := clip("canción", 10); got != "canción" {
		t.Fatalf("got %q", got)
	}
}

func TestNothingDrawnUsesARuneTheFontLacks(t *testing.T) {
	// Ebiten's debug font is a 32-by-8 atlas of the first 256 code points.
	// Anything past U+00FF draws nothing at all and still advances the cursor,
	// so an em dash is a hole in the line and an ellipsis is three. This is
	// the test that stops one creeping back in.
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	// Two exceptions, both correct: the window title is drawn by the window
	// manager, and truncate is only used on stdout by --list-tracks.
	allowed := map[string]bool{"main.go": true}

	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") || allowed[name] {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(src), "\n") {
			code, _, _ := strings.Cut(line, "//")
			if !strings.Contains(code, `"`) {
				continue
			}
			for _, r := range code {
				if r > 0xff {
					t.Errorf("%s: %q is past U+00FF and would draw as a gap: %s", name, r, strings.TrimSpace(line))
					break
				}
			}
		}
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
	for _, band := range [][2]float64{{100, 400}, {100, 130}, {0, 1000}, {0, 60}} {
		s := staffFor(band[0], band[1])
		if s.LineY(0) < band[0]-1 {
			t.Fatalf("band %v: top line at %v", band, s.LineY(0))
		}
		if s.LineY(4) > band[1]+1 {
			t.Fatalf("band %v: bottom line at %v", band, s.LineY(4))
		}
		// Below four pixels the five lines are a smudge; a band too small for
		// that overflows rather than shrinking further.
		if s.LineGap < 4 {
			t.Fatalf("band %v: lines %v apart, too close to read", band, s.LineGap)
		}
	}
}

func TestTheStaffLeavesRoomForWhatHangsOffIt(t *testing.T) {
	// A guitar's low E is three ledger lines below the staff and its stem
	// hangs another three gaps past that. Reserving only the five lines put
	// that note through the tablature underneath, and the high notes behind
	// the header.
	for _, band := range [][2]float64{{90, 575}, {90, 300}, {200, 260}} {
		s := staffFor(band[0], band[1])
		top, bottom := staffExtent(s)
		if height := bottom - top; height <= s.LineY(4)-s.LineY(0) {
			t.Fatalf("band %v: the reserved extent %v is no bigger than the staff itself", band, height)
		}
		// Centred: as much room above as below.
		above, below := s.LineY(0)-band[0], band[1]-s.LineY(4)
		if diff := above - below; diff > 1 || diff < -1 {
			t.Fatalf("band %v: %v above the staff and %v below", band, above, below)
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

// browserFixture is an app with just enough filled in to build a listing.
func browserFixture(t *testing.T, dir string, kind library.Kind) *app {
	t.Helper()
	a := &app{width: 800, height: 600}
	a.browse.kind = kind
	a.browseTo(dir)
	return a
}

func TestBrowserPutsUpFirstThenShortcutsThenFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.gp", "b.mid"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	a := browserFixture(t, dir, library.Score)
	rows := a.browseRows()
	if len(rows) == 0 || rows[0].label != ".." {
		t.Fatalf("first row is %+v, want up", rows[0])
	}

	// Directories before files, and every file openable.
	var sawDir, sawFile bool
	for _, r := range rows[1:] {
		if r.dir != "" {
			if sawFile {
				t.Fatalf("a directory came after a file: %v", rows)
			}
			sawDir = true
			continue
		}
		sawFile = true
	}
	if !sawDir || !sawFile {
		t.Fatalf("expected both kinds: %v", rows)
	}
}

func TestBrowserCursorStaysInRangeAndOnScreen(t *testing.T) {
	// The cursor drives what ENTER opens, so it must never point past the end
	// of a list — and it has to stay visible, or a long directory cannot be
	// walked with the keyboard at all.
	a := &app{width: 800, height: 600}
	count := 100

	for i := 0; i < 200; i++ {
		a.browse.cursor++
		if a.browse.cursor >= count {
			a.browse.cursor = count - 1
		}
	}
	if a.browse.cursor != count-1 {
		t.Fatalf("cursor %d", a.browse.cursor)
	}

	// And the scroll rule keeps it inside the visible window.
	a.browse.scroll = 0
	capacity := a.browseCapacity()
	a.browse.cursor = capacity + 5
	if last := a.browse.scroll + capacity - 1; a.browse.cursor > last {
		a.browse.scroll = a.browse.cursor - capacity + 1
	}
	if a.browse.cursor < a.browse.scroll || a.browse.cursor >= a.browse.scroll+capacity {
		t.Fatalf("cursor %d is outside the window %d..%d", a.browse.cursor, a.browse.scroll, a.browse.scroll+capacity)
	}
}

func TestBrowserSurvivesADirectoryItCannotRead(t *testing.T) {
	a := &app{width: 800, height: 600}
	a.browseTo(filepath.Join(t.TempDir(), "nowhere"))
	if a.browse.err == "" {
		t.Fatal("no error was reported")
	}
	// And it must still be able to draw: rows over a listing that never loaded.
	if got := a.browseRows(); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestSettingRowsAndButtonsAgree(t *testing.T) {
	// The update indexes the buttons by the row's position, so a mismatch
	// would act on the wrong setting.
	a := &app{width: 800, height: 600, song: &practice.Song{Tracks: []practice.Track{{Name: "Guitar"}}}}
	a.head = practice.NewTransport(practice.Clock{SampleRate: 48000}, 1000)
	a.runner = practice.NewRunner(a.head, practice.NewScriptedDetector(nil), practice.RunnerConfig{})

	buttons := a.settingButtons()
	if len(buttons) != len(settingRows) {
		t.Fatalf("%d buttons for %d rows", len(buttons), len(settingRows))
	}
	for i := range settingRows {
		x, y, w := a.settingGeometry(i)
		if buttons[i].x != x || buttons[i].y != y || buttons[i].w != w {
			t.Fatalf("row %d: button at %v,%v,%v but geometry says %v,%v,%v",
				i, buttons[i].x, buttons[i].y, buttons[i].w, x, y, w)
		}
	}
}

func TestSettingsFitEveryWindow(t *testing.T) {
	// Whatever the window, every visible row is inside it and none of them
	// overlaps the next. A window too small for the lot scrolls rather than
	// hiding the last few: a setting nobody can reach does not exist.
	for _, size := range [][2]int{{1280, 720}, {677, 723}, {500, 380}, {320, 240}, {1920, 1080}} {
		a := &app{width: size[0], height: size[1]}
		if a.settingPitch() < settingRowH {
			t.Fatalf("%v: rows overlap at pitch %v", size, a.settingPitch())
		}
		for i := range settingRows {
			if !a.settingVisible(i) {
				continue
			}
			_, y, _ := a.settingGeometry(i)
			if y < settingsTop-1 || y+settingRowH > float64(size[1])-settingsBottom+1 {
				t.Fatalf("%v: visible row %d runs from %v to %v", size, i, y, y+settingRowH)
			}
		}
	}
}

func TestEverySettingCanBeScrolledTo(t *testing.T) {
	a := &app{width: 500, height: 380}
	for i := range settingRows {
		a.revealSetting(i)
		if !a.settingVisible(i) {
			t.Fatalf("row %d cannot be brought on screen", i)
		}
	}
	// And the list cannot be scrolled past either end.
	a.scrollSettings(-100)
	if a.settings.scroll != 0 {
		t.Fatalf("scrolled above the top: %d", a.settings.scroll)
	}
	a.scrollSettings(100)
	if want := len(settingRows) - a.settingCapacity(); a.settings.scroll != want {
		t.Fatalf("scrolled to %d, want %d", a.settings.scroll, want)
	}
}

func TestEverySettingRowIsComplete(t *testing.T) {
	// A row with no value would draw an empty right-hand side; a stepper with
	// no less or more would look adjustable and do nothing.
	for i, r := range settingRows {
		if r.label == "" {
			t.Fatalf("row %d has no label", i)
		}
		switch r.kind {
		case settingStep:
			if r.value == nil || r.less == nil || r.more == nil {
				t.Fatalf("stepper %q is missing a value or an end", r.label)
			}
		case settingToggle:
			if r.on == nil || r.do == nil {
				t.Fatalf("toggle %q is missing its state or its action", r.label)
			}
		default:
			if r.value == nil || r.do == nil {
				t.Fatalf("action %q is missing a value or an action", r.label)
			}
		}
	}
}

func TestEveryTitleRowDoesSomething(t *testing.T) {
	for i, r := range titleRows {
		if r.label == "" || r.do == nil {
			t.Fatalf("title row %d is incomplete: %+v", i, r)
		}
		if r.key == "" {
			t.Fatalf("title row %q has no key; the mouse would be the only way to it", r.label)
		}
	}
}
