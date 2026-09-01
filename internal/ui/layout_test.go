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
