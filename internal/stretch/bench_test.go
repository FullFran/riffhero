package stretch

import "testing"

// The stretcher runs on the render goroutine, one step ahead of the audio
// callback. It does not have to be fast, it has to be comfortably faster than
// real time and it has to be silent about memory — a collection pause on that
// goroutine is a dropout. These benchmarks are how both claims stay honest.
//
// Each of them pushes enough input to produce exactly one second of output, so
// ns/op read against 1e9 is the fraction of one core that a second of playback
// costs.

func benchStretch(b *testing.B, channels int, rate float64, stride int) {
	b.Helper()

	frames := int(float64(testSampleRate) * rate)
	parts := make([][]float32, channels)
	for ch := range parts {
		parts[ch] = plucks(frames)
	}
	in := interleave(parts...)

	const block = 1024
	out := make([]float32, 8192*channels)

	w := New(channels, testSampleRate)
	w.SetRate(rate)
	w.stride = stride

	run := func() {
		for i := 0; i < len(in); i += block * channels {
			end := i + block*channels
			if end > len(in) {
				end = len(in)
			}
			w.Write(in[i:end])
			for w.Read(out) > 0 {
			}
		}
	}
	run() // reach steady state before measuring

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		run()
	}
}

func BenchmarkStretchSecond(b *testing.B) { benchStretch(b, 2, 0.5, defaultStride) }

// The single-pass search, for comparison. Everything else is identical, so the
// ratio between the two is what the coarse-to-fine pass buys.
func BenchmarkStretchSecondExhaustive(b *testing.B) { benchStretch(b, 2, 0.5, 1) }

func BenchmarkStretchSecondMono(b *testing.B)    { benchStretch(b, 1, 0.5, defaultStride) }
func BenchmarkStretchSecondQuarter(b *testing.B) { benchStretch(b, 2, 0.25, defaultStride) }

// Rate 1 is the copy path and most of a practice session sits on it, so its
// cost is the one that has to be nothing.
func BenchmarkUnitySecond(b *testing.B) { benchStretch(b, 2, 1, defaultStride) }

// BenchmarkWriteRead is one block in and its output back out: the granularity
// the render goroutine actually works at, and the number whose allocs/op has
// to read zero.
func BenchmarkWriteRead(b *testing.B) {
	w := New(2, testSampleRate)
	w.SetRate(0.5)
	block := interleave(sine(440, 0.6, 1024), sine(660, 0.6, 1024))
	out := make([]float32, 8192)

	for i := 0; i < 64; i++ {
		w.Write(block)
		for w.Read(out) > 0 {
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Write(block)
		for w.Read(out) > 0 {
		}
	}
}

// BenchmarkSearchOffset isolates the similarity search, which is where every
// cycle in this package goes.
func BenchmarkSearchOffset(b *testing.B) {
	for _, stride := range []int{1, defaultStride} {
		name := "stride1"
		if stride != 1 {
			name = "coarse"
		}
		b.Run(name, func(b *testing.B) {
			// Fill the input buffer directly rather than through Write, which
			// would run the whole pipeline and then compact the very frames
			// this benchmark wants to search over.
			w := New(1, testSampleRate)
			w.SetRate(0.5)
			w.stride = stride
			sig := plucks(8 * w.frame)
			w.in = append(w.in, sig...)
			w.mono = append(w.mono, sig...)
			w.primed = true

			nom := int64(4 * w.frame)
			w.cont = nom + int64(w.hop)
			end := w.end()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				w.searchOffset(nom, end)
			}
		})
	}
}
