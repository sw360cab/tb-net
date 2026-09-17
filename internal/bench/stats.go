// Package bench measures a link.Link and writes results in the CLAUDE.md §6
// format: one JSON per run, with fields fixed so runs stay comparable across
// versions. cmd/ultrabench wraps it.
package bench

import (
	"math"
	"slices"
	"time"
)

// Latency holds per-call latency percentiles in microseconds (§6 "lat_us").
type Latency struct {
	P50  float64 `json:"p50"`
	P90  float64 `json:"p90"`
	P99  float64 `json:"p99"`
	P999 float64 `json:"p999"`
	Max  float64 `json:"max"`
}

// Percentiles sorts samples in place and returns their nearest-rank
// percentiles: the p-th percentile is the smallest sample with at least a
// fraction p of all samples at or below it. It returns false for no samples.
func Percentiles(samples []time.Duration) (Latency, bool) {
	n := len(samples)
	if n == 0 {
		return Latency{}, false
	}
	slices.Sort(samples)
	// p in parts per 10000, so ranks are exact integer math, not float rounding
	at := func(p int) float64 {
		rank := (p*n + 9999) / 10000 // ceil(p/10000 * n), 1-based
		return micros(samples[max(rank, 1)-1])
	}
	return Latency{P50: at(5000), P90: at(9000), P99: at(9900), P999: at(9990), Max: micros(samples[n-1])}, true
}

// micros converts d to microseconds, kept to 0.001 µs.
func micros(d time.Duration) float64 {
	return math.Round(float64(d.Nanoseconds())) / 1e3
}
