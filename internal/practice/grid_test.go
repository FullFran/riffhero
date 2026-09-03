package practice

import "testing"

func testClock() Clock { return Clock{SampleRate: 48000} }

func TestBuildGridPlacesBarsAtTheRightSeconds(t *testing.T) {
	clock := testClock()
	grid := BuildGrid(clock, 0, []Section{{BPM: 120, Sig: CommonTime, Bars: 4}})

	if len(grid) != 4 {
		t.Fatalf("got %d bars, want 4", len(grid))
	}
	// 120 BPM in 4/4 is two seconds a bar.
	for i, bar := range grid {
		wantStart := clock.Frames(float64(i) * 2)
		if bar.Start != wantStart {
			t.Fatalf("bar %d starts at %d, want %d", i+1, bar.Start, wantStart)
		}
		if bar.Number != i+1 {
			t.Fatalf("bar at index %d is numbered %d", i, bar.Number)
		}
		if len(bar.Beats) != 4 {
			t.Fatalf("bar %d has %d beats, want 4", i+1, len(bar.Beats))
		}
	}
}

func TestBuildGridDoesNotDriftOverManyBars(t *testing.T) {
	// 100 BPM does not divide evenly into whole frames, which is exactly the
	// case where accumulating bar by bar loses samples.
	clock := testClock()
	const bars = 200
	grid := BuildGrid(clock, 0, []Section{{BPM: 100, Sig: CommonTime, Bars: bars}})

	last := grid[len(grid)-1]
	want := clock.Frames(float64(bars) * 4 * 60.0 / 100.0)
	if diff := last.End - want; diff > 1 || diff < -1 {
		t.Fatalf("after %d bars the grid ends at %d, want %d (drift %d frames)", bars, last.End, want, diff)
	}
}

func TestBuildGridHonoursTheBeatUnit(t *testing.T) {
	// BPM always counts quarter notes, so 6/8 at 120 has eighth-note beats and
	// a bar three quarters long.
	clock := testClock()
	grid := BuildGrid(clock, 0, []Section{{BPM: 120, Sig: TimeSignature{Beats: 6, Unit: 8}, Bars: 1}})

	if len(grid[0].Beats) != 6 {
		t.Fatalf("got %d beats, want 6", len(grid[0].Beats))
	}
	if want := clock.Frames(1.5); grid[0].End != want {
		t.Fatalf("6/8 bar at 120 ends at %d, want %d", grid[0].End, want)
	}
}

func TestBuildGridChainsSectionsWithoutAGap(t *testing.T) {
	clock := testClock()
	grid := BuildGrid(clock, clock.Frames(1), []Section{
		{BPM: 120, Sig: CommonTime, Bars: 2},
		{BPM: 60, Sig: TimeSignature{Beats: 3, Unit: 4}, Bars: 2},
	})

	if len(grid) != 4 {
		t.Fatalf("got %d bars, want 4", len(grid))
	}
	for i := 1; i < len(grid); i++ {
		if grid[i].Start != grid[i-1].End {
			t.Fatalf("gap between bar %d and %d: %d vs %d", i, i+1, grid[i-1].End, grid[i].Start)
		}
	}
	if grid[0].Start != clock.Frames(1) {
		t.Fatalf("grid should start at the lead-in, got %d", grid[0].Start)
	}
	if grid[2].BPM != 60 || grid[2].Sig.Beats != 3 {
		t.Fatalf("bar 3 did not pick up the second section: %+v", grid[2])
	}
}

func TestBuildGridSkipsNonsenseSections(t *testing.T) {
	grid := BuildGrid(testClock(), 0, []Section{
		{BPM: 0, Sig: CommonTime, Bars: 4},
		{BPM: 120, Sig: TimeSignature{}, Bars: 4},
		{BPM: 120, Sig: CommonTime, Bars: 0},
		{BPM: 120, Sig: CommonTime, Bars: 1},
	})
	if len(grid) != 1 {
		t.Fatalf("got %d bars, want only the one valid section's bar", len(grid))
	}
}

func TestBarAtFindsTheContainingBar(t *testing.T) {
	clock := testClock()
	grid := BuildGrid(clock, clock.Frames(1), []Section{{BPM: 120, Sig: CommonTime, Bars: 3}})

	cases := []struct {
		at   Frame
		want int
	}{
		{clock.Frames(0.5), -1}, // the lead-in belongs to no bar
		{clock.Frames(1), 0},
		{clock.Frames(2.9), 0},
		{clock.Frames(3), 1},
		{clock.Frames(6.9), 2},
		{clock.Frames(7.1), -1}, // past the end
	}
	for _, c := range cases {
		if got := grid.BarAt(c.at); got != c.want {
			t.Fatalf("BarAt(%d) = %d, want %d", c.at, got, c.want)
		}
	}
}

func TestSpanCoversWholeBarsInEitherOrder(t *testing.T) {
	clock := testClock()
	grid := BuildGrid(clock, 0, []Section{{BPM: 120, Sig: CommonTime, Bars: 4}})

	from, to := grid.Span(1, 2)
	if from != grid[1].Start || to != grid[2].End {
		t.Fatalf("Span(1,2) = %d..%d, want %d..%d", from, to, grid[1].Start, grid[2].End)
	}
	if rf, rt := grid.Span(2, 1); rf != from || rt != to {
		t.Fatalf("Span is not order-independent: %d..%d", rf, rt)
	}
	if cf, ct := grid.Span(-5, 99); cf != grid[0].Start || ct != grid.End() {
		t.Fatalf("Span did not clamp: %d..%d", cf, ct)
	}
}

func TestSnapMovesToTheNearestBeat(t *testing.T) {
	clock := testClock()
	grid := BuildGrid(clock, 0, []Section{{BPM: 120, Sig: CommonTime, Bars: 2}})

	// Beats are half a second apart at 120 BPM in 4/4.
	if got, want := grid.Snap(clock.Frames(0.6)), clock.Frames(0.5); got != want {
		t.Fatalf("Snap(0.6s) = %d, want %d", got, want)
	}
	if got, want := grid.Snap(clock.Frames(0.9)), clock.Frames(1.0); got != want {
		t.Fatalf("Snap(0.9s) = %d, want %d", got, want)
	}
	// The end of the song is a legitimate loop boundary even though no beat
	// starts there.
	if got, want := grid.Snap(clock.Frames(4.1)), grid.End(); got != want {
		t.Fatalf("Snap past the end = %d, want the grid end %d", got, want)
	}
}

func TestSnapOnAnEmptyGridIsTheIdentity(t *testing.T) {
	var grid Grid
	if got := grid.Snap(1234); got != 1234 {
		t.Fatalf("empty grid snapped %d to %d", 1234, got)
	}
}

func TestLocateReportsBarAndBeat(t *testing.T) {
	clock := testClock()
	grid := BuildGrid(clock, 0, []Section{{BPM: 120, Sig: CommonTime, Bars: 2}})

	cases := []struct {
		at         Frame
		bar, beat  int
		wantsValid bool
	}{
		{clock.Frames(0), 1, 1, true},
		{clock.Frames(0.6), 1, 2, true},
		{clock.Frames(1.75), 1, 4, true},
		{clock.Frames(2.25), 2, 1, true},
		{clock.Frames(99), 0, 0, false},
	}
	for _, c := range cases {
		got := grid.Locate(c.at)
		if got.Valid != c.wantsValid || (c.wantsValid && (got.Bar != c.bar || got.Beat != c.beat)) {
			t.Fatalf("Locate(%d) = %+v, want bar %d beat %d valid %v", c.at, got, c.bar, c.beat, c.wantsValid)
		}
	}
}

func TestGridFromPlacesBarsExactlyWhereItIsTold(t *testing.T) {
	// The case BuildGrid cannot express: a tempo change part-way through bar 1,
	// so bar 2 does not start where laying equal sections end to end would put
	// it. An importer with a tempo map knows the real frames.
	grid := GridFrom([]BarSpec{
		{Start: 0, End: 96000, Sig: CommonTime, BPM: 120},
		{Start: 96000, End: 180000, Sig: CommonTime, BPM: 90},
		{Start: 180000, End: 244000, Sig: TimeSignature{Beats: 3, Unit: 4}, BPM: 90},
	})

	if len(grid) != 3 {
		t.Fatalf("got %d bars, want 3", len(grid))
	}
	for i, want := range []Frame{0, 96000, 180000} {
		if grid[i].Start != want {
			t.Fatalf("bar %d starts at %d, want %d", i+1, grid[i].Start, want)
		}
		if grid[i].Number != i+1 {
			t.Fatalf("bar at index %d is numbered %d", i, grid[i].Number)
		}
	}
	if len(grid[2].Beats) != 3 {
		t.Fatalf("the 3/4 bar has %d beats", len(grid[2].Beats))
	}
	// Beats are spread across the bar the caller gave, not across a nominal one.
	if got, want := grid[1].Beats[2], Frame(96000+(180000-96000)*2/4); got != want {
		t.Fatalf("third beat of bar 2 at %d, want %d", got, want)
	}
}

func TestGridFromSkipsBarsThatMakeNoSense(t *testing.T) {
	grid := GridFrom([]BarSpec{
		{Start: 100, End: 100, Sig: CommonTime, BPM: 120},     // zero length
		{Start: 200, End: 100, Sig: CommonTime, BPM: 120},     // backwards
		{Start: 0, End: 1000, Sig: TimeSignature{}, BPM: 120}, // no meter
		{Start: 0, End: 1000, Sig: CommonTime, BPM: 120},
	})
	if len(grid) != 1 || grid[0].Number != 1 {
		t.Fatalf("got %+v, want one bar numbered 1", grid)
	}
}

func TestGridFromNothing(t *testing.T) {
	if got := GridFrom(nil); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}
