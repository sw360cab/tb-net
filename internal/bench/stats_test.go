package bench

import (
	"math/rand/v2"
	"testing"
	"time"
)

func us(v ...int) []time.Duration {
	d := make([]time.Duration, len(v))
	for i, x := range v {
		d[i] = time.Duration(x) * time.Microsecond
	}
	return d
}

// TestPercentilesKnownDistribution: 1..1000 µs, shuffled, has exact
// nearest-rank percentiles.
func TestPercentilesKnownDistribution(t *testing.T) {
	samples := make([]time.Duration, 1000)
	for i := range samples {
		samples[i] = time.Duration(i+1) * time.Microsecond
	}
	rand.New(rand.NewPCG(1, 2)).Shuffle(len(samples), func(i, j int) { samples[i], samples[j] = samples[j], samples[i] })

	got, ok := Percentiles(samples)
	want := Latency{P50: 500, P90: 900, P99: 990, P999: 999, Max: 1000}
	if !ok || got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestPercentilesSmall(t *testing.T) {
	tests := []struct {
		name    string
		samples []time.Duration
		want    Latency
	}{
		{"one sample", us(7), Latency{7, 7, 7, 7, 7}},
		{"two samples", us(2, 1), Latency{1, 2, 2, 2, 2}},
		{"ten samples", us(10, 9, 8, 7, 6, 5, 4, 3, 2, 1), Latency{5, 9, 10, 10, 10}},
		{"ties", us(3, 3, 3, 3), Latency{3, 3, 3, 3, 3}},
		{"sub-microsecond", []time.Duration{150 * time.Nanosecond}, Latency{0.15, 0.15, 0.15, 0.15, 0.15}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, ok := Percentiles(tt.samples); !ok || got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestPercentilesEmpty(t *testing.T) {
	if _, ok := Percentiles(nil); ok {
		t.Fatal("Percentiles(nil) reported samples")
	}
}
