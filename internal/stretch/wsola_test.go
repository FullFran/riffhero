package stretch

import (
	"math"
	"testing"
)

const testSampleRate = 48000

// sine renders one channel of a pure tone. A sine is the hardest signal to
// stretch convincingly and the easiest to measure: any phase error the
// overlap-add makes shows up as an amplitude notch or an extra zero crossing,
// with no harmonic clutter to hide behind.
func sine(hz, amp float64, n int) []float32 {
	out := make([]float32, n)
	for i := range out {
		out[i] = float32(amp * math.Sin(2*math.Pi*hz*float64(i)/testSampleRate))
	}
	return out
}

// chord renders four partials at once. A single tone is the easy case: the
// correlation curve has one hump per period and every offset the search can
// pick is a good one. A chord gives it several humps of similar height, which
// is where a search that runs on too short a window starts choosing badly.
func chord(amp float64, n int) []float32 {
	out := make([]float32, n)
	for _, hz := range []float64{110, 164.81, 220, 329.63} {
		for i := range out {
			out[i] += float32(amp * math.Sin(2*math.Pi*hz*float64(i)/testSampleRate))
		}
	}
	return fade(out)
}

// plucks renders decaying notes with attacks in them, so the search has to
// cope with material that is not stationary.
func plucks(n int) []float32 {
	out := make([]float32, n)
	for start := 0; start < n; start += testSampleRate / 4 {
		for i := 0; start+i < n; i++ {
			t := float64(i) / testSampleRate
			decay := math.Exp(-3 * t)
			var v float64
			for k := 1; k <= 8; k++ {
				v += (1 / float64(k)) * math.Sin(2*math.Pi*146.83*float64(k)*t+float64(k))
			}
			out[start+i] += float32(0.25 * decay * v)
		}
	}
	return fade(out)
}

// hiss renders low-passed noise: material with no period to match at all,
// which is the worst case WSOLA has and the reason the tolerances below are
// stated against the source rather than in the abstract.
func hiss(n int) []float32 {
	out := make([]float32, n)
	state := uint32(7)
	var lp float64
	for i := range out {
		state = state*1664525 + 1013904223
		u := float64(state>>8)/float64(1<<24)*2 - 1
		lp += 0.02 * (u - lp)
		out[i] = float32(3 * lp)
	}
	return fade(out)
}

// fade ramps the first and last 5 ms of a fixture.
//
// Real audio does not begin and end mid-waveform, and a fixture that does
// makes the drop to nothing at the end of the buffer look exactly like a click
// to maxStep. That cost an afternoon: the seam being measured was the
// fixture's, not the stretcher's.
func fade(buf []float32) []float32 {
	n := testSampleRate / 200
	for i := 0; i < n && i < len(buf); i++ {
		g := float32(i) / float32(n)
		buf[i] *= g
		buf[len(buf)-1-i] *= g
	}
	return buf
}

// interleave builds an interleaved buffer from one slice per channel.
func interleave(chans ...[]float32) []float32 {
	c := len(chans)
	n := len(chans[0])
	out := make([]float32, n*c)
	for f := 0; f < n; f++ {
		for ch := 0; ch < c; ch++ {
			out[f*c+ch] = chans[ch][f]
		}
	}
	return out
}

// channel pulls one channel back out of an interleaved buffer.
func channel(in []float32, c, ch int) []float32 {
	out := make([]float32, len(in)/c)
	for f := range out {
		out[f] = in[f*c+ch]
	}
	return out
}

// stretched runs a signal through a stretcher in blocks of the given size and
// returns everything it produced, drain tail included.
func stretched(w *WSOLA, in []float32, block int) []float32 {
	c := w.Channels()
	out := make([]float32, 0, 5*len(in))
	buf := make([]float32, 8192*c)

	pull := func() {
		for {
			n := w.Read(buf)
			if n == 0 {
				return
			}
			out = append(out, buf[:n*c]...)
		}
	}
	for i := 0; i < len(in); i += block * c {
		end := i + block*c
		if end > len(in) {
			end = len(in)
		}
		w.Write(in[i:end])
		pull()
	}
	w.Drain()
	pull()
	return out
}

// fundamentalHz estimates the frequency of a near-sinusoidal buffer from its
// upward zero crossings, interpolated between the two samples that straddle
// each one. Unlike an FFT bin its resolution does not depend on the buffer
// length, which matters because the whole question here is whether a 440 Hz
// tone came out at 440 Hz and not 220 or 880.
func fundamentalHz(buf []float32) float64 {
	var first, last float64
	count := 0
	for i := 1; i < len(buf); i++ {
		a, b := buf[i-1], buf[i]
		if a < 0 && b >= 0 {
			t := float64(i-1) + float64(-a)/float64(b-a)
			if count == 0 {
				first = t
			}
			last = t
			count++
		}
	}
	if count < 2 {
		return 0
	}
	return float64(count-1) * testSampleRate / (last - first)
}

// maxStep is the largest sample-to-sample jump in one channel. A click is a
// jump, so comparing the output's largest step against the input's is how a
// test sees an overlap-add seam without listening to it.
func maxStep(buf []float32) float64 {
	var worst float64
	for i := 1; i < len(buf); i++ {
		if d := math.Abs(float64(buf[i] - buf[i-1])); d > worst {
			worst = d
		}
	}
	return worst
}

func peak(buf []float32) float64 {
	var worst float64
	for _, s := range buf {
		if v := math.Abs(float64(s)); v > worst {
			worst = v
		}
	}
	return worst
}

// Rate 1 is where a practice session spends most of its time, so it has to be
// a copy and not an overlap-add that is merely very close to one.
func TestUnityRateIsBitExact(t *testing.T) {
	in := sine(440, 0.7, 3*testSampleRate/4)
	got := stretched(New(1, testSampleRate), in, 1024)

	if len(got) != len(in) {
		t.Fatalf("got %d frames, want %d", len(got), len(in))
	}
	for i := range in {
		if got[i] != in[i] {
			t.Fatalf("frame %d: got %v, want %v (rate 1 must be a copy)", i, got[i], in[i])
		}
	}
}

// The bit-exactness must not depend on how the renderer chops up its writes.
func TestUnityRateIsBitExactAcrossBlockSizes(t *testing.T) {
	in := interleave(sine(220, 0.5, 20000), sine(330, 0.5, 20000))
	for _, block := range []int{1, 7, 256, 4096, 1 << 16} {
		got := stretched(New(2, testSampleRate), in, block)
		if len(got) != len(in) {
			t.Errorf("block %d: got %d samples, want %d", block, len(got), len(in))
			continue
		}
		for i := range in {
			if got[i] != in[i] {
				t.Errorf("block %d: sample %d differs: got %v, want %v", block, i, got[i], in[i])
				break
			}
		}
	}
}

// The point of the whole package: slower must not mean lower.
func TestStretchKeepsPitch(t *testing.T) {
	const want = 440.0
	in := sine(want, 0.7, 2*testSampleRate)

	// Above 1 as well: playing a passage faster than target is a practice
	// technique too, and the same search has to hold the pitch there.
	for _, rate := range []float64{0.25, 0.5, 0.75, 0.9, 1.25, 2.0} {
		w := New(1, testSampleRate)
		w.SetRate(rate)
		out := stretched(w, in, 1024)

		// Skip the drain fade, where the amplitude falls through the noise
		// floor and zero crossings stop meaning anything.
		body := out[:len(out)-2*w.hop]
		got := fundamentalHz(body)
		if cents := math.Abs(1200 * math.Log2(got/want)); cents > 35 {
			t.Errorf("rate %.2f: got %.2f Hz, want %.0f Hz (%.1f cents off)", rate, got, want, cents)
		}
		if err := math.Abs(got-want) / want; err > 0.02 {
			t.Errorf("rate %.2f: got %.2f Hz, %.2f%% off 440", rate, got, 100*err)
		}
	}
}

// A chord has to survive as well as a single tone: three simultaneous partials
// are what make a naive overlap-add ripple.
func TestStretchKeepsPitchOfAChord(t *testing.T) {
	n := 2 * testSampleRate
	in := make([]float32, n)
	for _, hz := range []float64{220, 277.18, 329.63} {
		tone := sine(hz, 0.25, n)
		for i := range in {
			in[i] += tone[i]
		}
	}

	w := New(1, testSampleRate)
	w.SetRate(0.5)
	out := stretched(w, in, 1024)

	// The reference is the *input's* own zero-crossing rate, not 220 Hz. A sum
	// of partials crosses zero more often than its lowest one does, so holding
	// the output to 220 would be measuring the estimator rather than the
	// stretcher.
	want := fundamentalHz(in)
	got := fundamentalHz(out[w.hop : len(out)-2*w.hop])
	if err := math.Abs(got-want) / want; err > 0.02 {
		t.Errorf("got %.2f Hz, want %.2f Hz (%.2f%% off)", got, want, 100*err)
	}
}

func TestOutputLengthTracksRate(t *testing.T) {
	frames := 2 * testSampleRate
	in := sine(196, 0.6, frames)

	for _, rate := range []float64{0.25, 0.5, 0.75, 1.0, 1.5, 2.0} {
		w := New(1, testSampleRate)
		w.SetRate(rate)
		got := len(stretched(w, in, 1024))

		// The drain gives up on the last segment or two rather than searching
		// a window that has run out of input, so the output lands slightly
		// short. Two seconds in, that is under 2%.
		want := float64(frames) / rate
		if err := math.Abs(float64(got)-want) / want; err > 0.03 {
			t.Errorf("rate %.2f: got %d frames, want ~%.0f (%.2f%% off)", rate, got, want, 100*err)
		}
	}
}

// A seam in the overlap-add is a step in the waveform, and a step is a click.
//
// Every fixture is checked, not just the sine, because the sine hides the bug
// this test exists for: with a single partial almost any offset is a phase
// match, so a search that has gone wrong still sounds fine. The chord is what
// makes a bad offset audible.
func TestStretchHasNoDiscontinuities(t *testing.T) {
	fixtures := map[string][]float32{
		"sine":   fade(sine(440, 0.7, 2*testSampleRate)),
		"chord":  chord(0.2, 2*testSampleRate),
		"plucks": plucks(2 * testSampleRate),
		"hiss":   hiss(2 * testSampleRate),
	}
	for name, in := range fixtures {
		source := maxStep(in)
		for _, rate := range []float64{0.25, 0.5, 0.75, 1.5} {
			w := New(1, testSampleRate)
			w.SetRate(rate)
			out := stretched(w, in, 1024)

			if got := maxStep(out); got > 1.05*source {
				t.Errorf("%s at rate %.2f: largest step %.5f, above the source's %.5f",
					name, rate, got, source)
			}
			if got := peak(out); got > 1.02*peak(in) {
				t.Errorf("%s at rate %.2f: peak %.4f, above the source's %.4f",
					name, rate, got, peak(in))
			}
		}
	}
}

// The last segment of a drained stream is the one with no overlap left to
// match against, and it was the one that clicked. The search used to shorten
// its correlation window to whatever input remained, score noise over a
// handful of frames, and place the final segment on an arbitrary phase — at
// full level, on the last chord of the song.
func TestDrainTailDoesNotClick(t *testing.T) {
	in := chord(0.2, testSampleRate)
	source := maxStep(in)

	for _, rate := range []float64{0.25, 0.5, 0.75, 0.9} {
		w := New(1, testSampleRate)
		w.SetRate(rate)
		out := stretched(w, in, 1024)

		if got := maxStep(out); got > 1.05*source {
			t.Errorf("rate %.2f: largest step %.5f, above the source's %.5f", rate, got, source)
		}

		// Nor may Drain pad the stream out with the silence it renders
		// internally: the output has to end where the caller's audio ended.
		silent := 0
		for i := len(out) - 1; i >= 0 && out[i] == 0; i-- {
			silent++
		}
		if silent > w.hop {
			t.Errorf("rate %.2f: %d silent frames at the end, want at most a hop (%d)", rate, silent, w.hop)
		}
	}
}

// The overlap-add must not notch a steady tone: two windowed copies that are
// half a period apart cancel, and that is what a search that never runs sounds
// like.
func TestStretchKeepsLevel(t *testing.T) {
	in := sine(330, 0.7, testSampleRate)
	w := New(1, testSampleRate)
	w.SetRate(0.5)
	out := stretched(w, in, 1024)

	body := out[w.frame : len(out)-2*w.hop]
	var sum float64
	for _, s := range body {
		sum += float64(s) * float64(s)
	}
	got := math.Sqrt(sum / float64(len(body)))
	want := 0.7 / math.Sqrt2
	if math.Abs(got-want)/want > 0.1 {
		t.Errorf("got RMS %.4f, want ~%.4f", got, want)
	}
}

// A signal panned hard left must stay hard left. This is necessary but on its
// own it proves little: silence correlates with silence at every offset, so a
// per-channel search would still leave the right channel silent. The test that
// actually pins the shared offset down is the one below it.
func TestStereoKeepsHardPannedSignalPanned(t *testing.T) {
	n := testSampleRate
	in := interleave(sine(440, 0.7, n), make([]float32, n))

	w := New(2, testSampleRate)
	w.SetRate(0.5)
	out := stretched(w, in, 512)

	right := channel(out, 2, 1)
	if got := peak(right); got != 0 {
		t.Errorf("right channel peak %.6g, want exact silence", got)
	}
	if got := peak(channel(out, 2, 0)); got < 0.6 {
		t.Errorf("left channel peak %.4f, want the signal to survive", got)
	}
}

// Every channel has to be cut at the same offset, chosen on the downmix.
//
// Overlap-add is linear, so stretching the downmix of a stereo pair must give
// exactly the downmix of the stretched pair — but only while both channels
// were assembled from the same input segments. Search each channel separately
// and the two sides land on offsets up to a tolerance apart, their average
// stops matching the mono result, and what the player hears is the phantom
// centre smearing into a chorus.
//
// The channels here are deliberately unrelated; scalar multiples of one
// another would score identically under a normalized correlation and a
// per-channel search would pick the same offset by accident.
func TestStereoUsesOneOffsetForEveryChannel(t *testing.T) {
	n := testSampleRate
	left := sine(440, 0.6, n)
	right := sine(623, 0.6, n)

	mono := make([]float32, n)
	for i := range mono {
		mono[i] = (left[i] + right[i]) / 2
	}

	sw := New(2, testSampleRate)
	sw.SetRate(0.6)
	stereoOut := stretched(sw, interleave(left, right), 512)

	mw := New(1, testSampleRate)
	mw.SetRate(0.6)
	monoOut := stretched(mw, mono, 512)

	if len(monoOut) != len(stereoOut)/2 {
		t.Fatalf("got %d mono frames and %d stereo frames; the two paths disagree on length",
			len(monoOut), len(stereoOut)/2)
	}
	for i := range monoOut {
		mixed := (stereoOut[2*i] + stereoOut[2*i+1]) / 2
		if d := math.Abs(float64(mixed - monoOut[i])); d > 1e-5 {
			t.Fatalf("frame %d: downmix of the stereo output %.6f, mono output %.6f", i, mixed, monoOut[i])
		}
	}
}

// Changing speed mid-passage is the normal way this gets used: the player
// slows a bar down, works it, and speeds it back up without stopping.
func TestRateChangeMidStreamStaysClean(t *testing.T) {
	// One continuous tone, sliced into blocks. Concatenating a short buffer
	// instead would tile a phase jump into the source every 200 frames and the
	// discontinuity check below would be measuring the fixture.
	const block = 200
	const blocks = 240
	tone := sine(440, 0.7, block*blocks)
	source := maxStep(tone)
	stereoIn := interleave(tone, tone)

	w := New(2, testSampleRate)
	out := make([]float32, 0, 1<<21)
	buf := make([]float32, 8192)
	pull := func() {
		for {
			n := w.Read(buf)
			if n == 0 {
				return
			}
			out = append(out, buf[:n*2]...)
		}
	}

	rates := []float64{1.0, 0.5, 0.5, 0.75, 0.75, 1.0, 1.0, 0.25, 1.0}
	for b := 0; b < blocks; b++ {
		w.SetRate(rates[b*len(rates)/blocks])
		w.Write(stereoIn[b*block*2 : (b+1)*block*2])
		pull()
	}
	w.Drain()
	pull()

	for ch := 0; ch < 2; ch++ {
		got := channel(out, 2, ch)
		if p := peak(got); p > 1.0 {
			t.Errorf("channel %d: peak %.4f, want nothing above full scale", ch, p)
		}
		if s := maxStep(got); s > 1.5*source {
			t.Errorf("channel %d: largest step %.5f, more than 1.5x the source's %.5f", ch, s, source)
		}
	}
}

// A seek must leave the filter in the state it was born in, or the first bar
// after every seek is stretched from stale history.
func TestResetMatchesFreshInstance(t *testing.T) {
	in := sine(196, 0.6, testSampleRate/2)

	fresh := New(2, testSampleRate)
	fresh.SetRate(0.6)
	want := stretched(fresh, interleave(in, in), 512)

	reused := New(2, testSampleRate)
	reused.SetRate(0.6)
	junk := interleave(sine(90, 0.9, 30000), sine(1500, 0.9, 30000))
	reused.Write(junk)
	reused.Read(make([]float32, 4096))
	reused.Reset()
	got := stretched(reused, interleave(in, in), 512)

	if len(got) != len(want) {
		t.Fatalf("got %d samples after reset, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sample %d after reset: got %v, want %v", i, got[i], want[i])
		}
	}
}

// Reset must not silently change the speed the player chose.
func TestResetKeepsRate(t *testing.T) {
	w := New(1, testSampleRate)
	w.SetRate(0.4)
	w.Reset()
	if w.Rate() != 0.4 {
		t.Errorf("got rate %v after reset, want 0.4", w.Rate())
	}
}

func TestSetRateClamps(t *testing.T) {
	w := New(1, testSampleRate)
	for _, tc := range []struct{ set, want float64 }{
		{0.5, 0.5},
		{0, MinRate},
		{-3, MinRate},
		{100, MaxRate},
		{1, 1},
	} {
		w.SetRate(tc.set)
		if w.Rate() != tc.want {
			t.Errorf("SetRate(%v): got %v, want %v", tc.set, w.Rate(), tc.want)
		}
	}

	// NaN compares false against both bounds, so a plain clamp lets it through
	// and every position in the filter turns into NaN one Write later.
	w.SetRate(0.5)
	w.SetRate(math.NaN())
	if w.Rate() != 0.5 {
		t.Errorf("got rate %v after SetRate(NaN), want the previous 0.5", w.Rate())
	}
}

func TestChannelsAndLatency(t *testing.T) {
	w := New(2, testSampleRate)
	if w.Channels() != 2 {
		t.Errorf("got %d channels, want 2", w.Channels())
	}
	if got := w.Latency(); got != 0 {
		t.Errorf("got latency %d at rate 1, want 0: that path is a copy", got)
	}
	w.SetRate(0.5)
	if got := w.Latency(); got != w.frame+w.tolerance {
		t.Errorf("got latency %d, want %d", got, w.frame+w.tolerance)
	}
}

// Less than a window of input is not an error, it is just not enough yet.
func TestShortInputWaitsForMore(t *testing.T) {
	w := New(1, testSampleRate)
	w.SetRate(0.5)
	w.Write(sine(440, 0.5, 100))
	if got := w.Available(); got != 0 {
		t.Errorf("got %d frames available from 100 input frames, want 0", got)
	}

	// Drain is how the caller says there will be no more.
	w.Drain()
	if got := w.Available(); got == 0 {
		t.Error("got nothing after Drain, want the buffered input flushed")
	}
}

func TestEmptyAndPartialWrites(t *testing.T) {
	w := New(2, testSampleRate)
	w.SetRate(0.5)
	w.Write(nil)
	w.Write([]float32{})
	w.Write([]float32{0.5}) // half a stereo frame is no frame at all
	if got := w.Available(); got != 0 {
		t.Errorf("got %d frames available, want 0", got)
	}
	w.Drain()
	if got := w.Available(); got != 0 {
		t.Errorf("got %d frames after draining half a frame, want 0", got)
	}
}

// A buffer too small for one frame must read nothing rather than tearing a
// frame in half and swapping the channels for the rest of the song.
func TestReadIntoUndersizedBuffer(t *testing.T) {
	w := New(2, testSampleRate)
	w.Write(interleave(sine(440, 0.5, 4096), sine(440, 0.5, 4096)))
	if got := w.Available(); got == 0 {
		t.Fatal("nothing available to read")
	}

	before := w.Available()
	if got := w.Read(make([]float32, 1)); got != 0 {
		t.Errorf("got %d frames into a 1-sample buffer, want 0", got)
	}
	if got := w.Read(nil); got != 0 {
		t.Errorf("got %d frames into a nil buffer, want 0", got)
	}
	if w.Available() != before {
		t.Errorf("got %d frames available after two no-op reads, want %d", w.Available(), before)
	}
}

func TestReadReturnsWholeFramesOnly(t *testing.T) {
	w := New(2, testSampleRate)
	w.SetRate(0.5)
	w.Write(interleave(sine(440, 0.5, 20000), sine(220, 0.5, 20000)))

	// An odd-length buffer must still come back with whole frames in it.
	buf := make([]float32, 101)
	total := 0
	for {
		n := w.Read(buf)
		if n == 0 {
			break
		}
		if n > 50 {
			t.Fatalf("got %d frames from a 101-sample stereo buffer, want at most 50", n)
		}
		total += n
	}
	if total == 0 {
		t.Fatal("read nothing")
	}
}

func TestMonoAndStereoBothWork(t *testing.T) {
	for _, channels := range []int{1, 2} {
		n := testSampleRate / 2
		parts := make([][]float32, channels)
		for ch := range parts {
			parts[ch] = sine(220*float64(ch+1), 0.5, n)
		}
		w := New(channels, testSampleRate)
		w.SetRate(0.5)
		out := stretched(w, interleave(parts...), 777)

		if got, want := len(out)/channels, 2*n; math.Abs(float64(got-want))/float64(want) > 0.04 {
			t.Errorf("%d channels: got %d frames, want ~%d", channels, got, want)
		}
		if got := fundamentalHz(channel(out, channels, 0)); math.Abs(got-220)/220 > 0.02 {
			t.Errorf("%d channels: channel 0 came out at %.2f Hz, want 220", channels, got)
		}
	}
}

// New must not be a way to crash the app from a bad config file.
func TestNewToleratesNonsenseGeometry(t *testing.T) {
	w := New(0, 0)
	if w.Channels() != 1 {
		t.Errorf("got %d channels, want the fallback 1", w.Channels())
	}
	w.SetRate(0.5)
	w.Write(sine(440, 0.5, 8192))
	w.Drain()
	if w.Available() == 0 {
		t.Error("got no output from the fallback geometry")
	}
}

// The render goroutine feeds a real-time callback, so a garbage pause here is
// a dropout the player hears. Steady state has to be allocation-free.
func TestSteadyStateDoesNotAllocate(t *testing.T) {
	w := New(2, testSampleRate)
	w.SetRate(0.5)
	block := interleave(sine(440, 0.6, 1024), sine(660, 0.6, 1024))
	out := make([]float32, 8192)

	pump := func() {
		w.Write(block)
		for w.Read(out) > 0 {
		}
	}
	for i := 0; i < 64; i++ {
		pump() // let every buffer reach its steady-state capacity
	}
	if got := testing.AllocsPerRun(200, pump); got != 0 {
		t.Errorf("got %v allocs/op in steady state, want 0", got)
	}
}

func TestSteadyStateDoesNotAllocateAtUnityRate(t *testing.T) {
	w := New(2, testSampleRate)
	block := interleave(sine(440, 0.6, 1024), sine(660, 0.6, 1024))
	out := make([]float32, 8192)

	pump := func() {
		w.Write(block)
		for w.Read(out) > 0 {
		}
	}
	for i := 0; i < 64; i++ {
		pump()
	}
	if got := testing.AllocsPerRun(200, pump); got != 0 {
		t.Errorf("got %v allocs/op in steady state, want 0", got)
	}
}

// The two-pass search is only worth its complexity if it is as clean as the
// exhaustive one. It does not pick the same offsets — on dense material the
// correlation curve has several humps of nearly equal height and the coarse
// pass can prefer a different one — so what is held constant here is the thing
// that actually matters: no extra seam, no extra level, near enough the same
// amount of output.
func TestCoarseSearchIsAsCleanAsExhaustive(t *testing.T) {
	fixtures := map[string][]float32{
		"sine":   fade(sine(440, 0.7, testSampleRate)),
		"chord":  chord(0.2, testSampleRate),
		"plucks": plucks(testSampleRate),
		"hiss":   hiss(testSampleRate),
	}
	for name, in := range fixtures {
		for _, rate := range []float64{0.5, 0.75} {
			coarse := New(1, testSampleRate)
			coarse.SetRate(rate)
			gotCoarse := stretched(coarse, in, 1024)

			exact := New(1, testSampleRate)
			exact.SetRate(rate)
			exact.stride = 1
			gotExact := stretched(exact, in, 1024)

			if a, b := maxStep(gotCoarse), maxStep(gotExact); a > 1.05*b {
				t.Errorf("%s at rate %.2f: coarse step %.5f, exhaustive %.5f", name, rate, a, b)
			}
			if a, b := peak(gotCoarse), peak(gotExact); a > 1.05*b {
				t.Errorf("%s at rate %.2f: coarse peak %.4f, exhaustive %.4f", name, rate, a, b)
			}
			a, b := float64(len(gotCoarse)), float64(len(gotExact))
			if math.Abs(a-b)/b > 0.02 {
				t.Errorf("%s at rate %.2f: coarse produced %.0f frames, exhaustive %.0f", name, rate, a, b)
			}
		}
	}
}

// The similarity search has to prefer the phase match, not the loud candidate.
// A bare dot product picks the attack every time, which is the bug this
// normalization exists to prevent.
func TestSearchPrefersPhaseMatchOverLoudness(t *testing.T) {
	w := New(1, testSampleRate)
	w.SetRate(0.5)

	// A quiet periodic bed with one loud burst sitting inside the search range.
	n := 4 * w.frame
	in := sine(200, 0.05, n)
	burst := w.frame + w.tolerance/2
	for i := burst; i < burst+200 && i < n; i++ {
		in[i] += float32(4 * math.Sin(2*math.Pi*3000*float64(i)/testSampleRate))
	}
	w.Write(in)

	period := int64(testSampleRate / 200)
	nom := int64(w.frame)
	w.cont = nom + period // one whole period ahead: the match is at nom
	got := w.searchOffset(nom, w.end())

	off := (got - nom) % period
	if off > period/2 {
		off -= period
	}
	if off < -period/2 {
		off += period
	}
	if off < -4 || off > 4 {
		t.Errorf("search landed %d frames off the phase match (offset %d, nominal %d)", off, got, nom)
	}
}
