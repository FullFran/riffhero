package ui

import (
	"math"
	"testing"

	"github.com/FullFran/riffhero/internal/practice"
)

func testLayout() Layout {
	return NewLayout(1100, 650, practice.Clock{SampleRate: 48000})
}

func TestStringRowsAreOrderedHighToLow(t *testing.T) {
	l := testLayout()

	var prev float64
	for s := uint8(1); s <= 6; s++ {
		y := l.StringY(s)
		if s > 1 && y <= prev {
			t.Fatalf("string %d y=%v is not below string %d y=%v", s, y, s-1, prev)
		}
		if y < 0 || y > 650 {
			t.Fatalf("string %d y=%v is off screen", s, y)
		}
		prev = y
	}
}

func TestStringYClampsOutOfRangeStrings(t *testing.T) {
	l := testLayout()

	if l.StringY(0) != l.StringY(1) {
		t.Fatal("string 0 must clamp to string 1")
	}
	if l.StringY(9) != l.StringY(6) {
		t.Fatal("string 9 must clamp to string 6")
	}
}

func TestNoteXSitsOnThePlayheadWhenDue(t *testing.T) {
	l := testLayout()
	now := practice.Frame(48000)

	if got := l.NoteX(now, now); got != l.PlayheadX {
		t.Fatalf("x=%v want playhead %v", got, l.PlayheadX)
	}
}

func TestNoteXScrollsRightToLeft(t *testing.T) {
	l := testLayout()
	clock := practice.Clock{SampleRate: 48000}
	now := practice.Frame(48000)

	future := l.NoteX(now, now+clock.Frames(1))
	past := l.NoteX(now, now-clock.Frames(1))

	if future <= l.PlayheadX {
		t.Fatalf("future note x=%v must be right of the playhead %v", future, l.PlayheadX)
	}
	if past >= l.PlayheadX {
		t.Fatalf("past note x=%v must be left of the playhead %v", past, l.PlayheadX)
	}
	if got, want := future-l.PlayheadX, l.PixelsPerSecond; math.Abs(got-want) > 1e-9 {
		t.Fatalf("one second ahead = %v px, want %v", got, want)
	}
}

func TestVisibleWindowCoversTheScreen(t *testing.T) {
	l := testLayout()
	now := practice.Frame(480000)

	from, to := l.VisibleWindow(now)
	if from > now || to < now {
		t.Fatalf("window [%d,%d] does not contain now=%d", from, to, now)
	}
	if x := l.NoteX(now, from); x > 0 {
		t.Fatalf("window start maps to x=%v, want <= 0", x)
	}
	if x := l.NoteX(now, to); x < l.Width {
		t.Fatalf("window end maps to x=%v, want >= width %v", x, l.Width)
	}
}

func TestVisibleWindowNeverGoesNegative(t *testing.T) {
	l := testLayout()

	if from, _ := l.VisibleWindow(0); from != 0 {
		t.Fatalf("window start=%d want 0 at song start", from)
	}
}

func TestWithBandRecentresTheTab(t *testing.T) {
	// Showing notation above the tab means the tab gets half the room. It has
	// to stay inside what it was given: a tab that slides under the staff is
	// two readings on top of each other rather than one of each.
	clock := practice.Clock{SampleRate: 48000}
	l := NewLayout(1000, 900, clock).WithBand(400, 700)

	if got := l.StringY(1); got < 400 {
		t.Fatalf("the top string is at %v, above the band", got)
	}
	if got := l.StringY(6); got > 700 {
		t.Fatalf("the bottom string is at %v, below the band", got)
	}
	if l.StringY(6) <= l.StringY(1) {
		t.Fatal("the strings are upside down")
	}
}

func TestWithBandKeepsATinyBandReadable(t *testing.T) {
	// Six strings in forty pixels would be four pixels apart; the minimum
	// spacing wins and the tab overflows rather than becoming a smudge.
	clock := practice.Clock{SampleRate: 48000}
	l := NewLayout(1000, 900, clock).WithBand(400, 440)
	if gap := l.StringY(2) - l.StringY(1); gap < 18 {
		t.Fatalf("strings %v apart", gap)
	}
}

func TestBandSitsBetweenThePanels(t *testing.T) {
	clock := practice.Clock{SampleRate: 48000}
	top, bottom := NewLayout(1000, 900, clock).Band()
	if top <= HeaderHeight {
		t.Fatalf("the band starts at %v, under the header", top)
	}
	if bottom >= 900-FooterHeight {
		t.Fatalf("the band ends at %v, under the footer", bottom)
	}
}

func TestTheTabNeverStartsAboveItsBand(t *testing.T) {
	// A band thinner than six strings at the minimum spacing used to get a
	// negative centring offset, which put string 1 above the space the layout
	// was given: over the staff in "both" mode, or under the header in a short
	// tab-only window. Overflowing downwards is untidy; overflowing upwards
	// draws two readings on top of each other.
	clock := practice.Clock{SampleRate: 48000}
	for _, band := range [][2]float64{{100, 400}, {148, 182}, {0, 20}, {300, 300}} {
		l := NewLayout(1000, 900, clock).WithBand(band[0], band[1])
		if got := l.StringY(1); got < band[0]-0.001 {
			t.Fatalf("band %v: the top string is at %v", band, got)
		}
	}
}

func TestATinyWindowKeepsTheTabBelowTheHeader(t *testing.T) {
	clock := practice.Clock{SampleRate: 48000}
	for _, h := range []float64{200, 250, 282, 300, 400} {
		l := NewLayout(1000, h, clock)
		if got := l.StringY(1); got < HeaderHeight {
			t.Fatalf("height %v: the top string is at %v, under the header", h, got)
		}
	}
}
