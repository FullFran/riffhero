package dsp

import "testing"

// The whole chain has to run comfortably faster than real time, otherwise the
// ring backs up and detections are dropped. These benchmarks are how that
// claim stays honest as the DSP grows.

func BenchmarkMPMDetect(b *testing.B) {
	est := NewMPM(testSampleRate)
	buf := pluck(196, est.WindowSize(), 10)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		est.Detect(buf)
	}
}

func BenchmarkYINDetect(b *testing.B) {
	est := NewYIN(testSampleRate)
	buf := pluck(196, est.WindowSize(), 10)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		est.Detect(buf)
	}
}

func BenchmarkEstimatorDetect(b *testing.B) {
	est := NewEstimator(testSampleRate)
	buf := pluck(196, est.WindowSize(), 10)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		est.Detect(buf)
	}
}

// BenchmarkTrackerRealtimeSecond measures the cost of one second of audio
// through the full chain: gate, onset, estimation and note tracking.
func BenchmarkTrackerRealtimeSecond(b *testing.B) {
	signal := perform(testSampleRate, []voice{
		{at: 0, hz: 110},
		{at: int(0.25 * testSampleRate), hz: 146.8324},
		{at: int(0.50 * testSampleRate), hz: 195.9977},
		{at: int(0.75 * testSampleRate), hz: 246.9417},
	})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tr := NewTracker(testSampleRate)
		for j := 0; j < len(signal); j += 512 {
			end := j + 512
			if end > len(signal) {
				end = len(signal)
			}
			tr.Push(signal[j:end])
		}
	}
}

// BenchmarkRingWrite is the only part of the chain that runs on the audio
// callback, so it is the only part whose cost is a hard real-time constraint.
func BenchmarkRingWrite(b *testing.B) {
	r := NewRing(1 << 16)
	block := make([]float32, 256)
	out := make([]float32, 256)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Write(block)
		r.Read(out)
	}
}
