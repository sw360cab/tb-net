package bench

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	"github.com/sw360cab/tb-net/internal/link"
	"github.com/sw360cab/tb-net/internal/proto"
)

const pageSize = 1 << proto.PageShift4K

// Modes are the modes Run knows, in the order "all" runs them.
var Modes = []string{"rtt", "get_seq", "get_rand", "put_seq"}

// ParseModes turns a -mode flag into modes: a comma-separated list of Modes,
// or "all".
func ParseModes(s string) ([]string, error) {
	if s == "all" {
		return slices.Clone(Modes), nil
	}
	var modes []string
	for _, m := range strings.Split(s, ",") {
		if m = strings.TrimSpace(m); !slices.Contains(Modes, m) {
			return nil, fmt.Errorf("bench: unknown mode %q (have %s, or all)", m, strings.Join(Modes, ", "))
		}
		modes = append(modes, m)
	}
	return modes, nil
}

// Measurement is the outcome of one mode.
type Measurement struct {
	Mode    string
	Samples []time.Duration // latency of each timed call, in call order
	Elapsed time.Duration   // wall time of the whole timed loop
}

// MBps is the timed loop's page throughput in MB/s (2^20 bytes); 0 for rtt,
// which moves no pages.
func (m Measurement) MBps() float64 {
	if m.Mode == "rtt" || m.Elapsed <= 0 {
		return 0
	}
	return float64(len(m.Samples)*pageSize) / (1 << 20) / m.Elapsed.Seconds()
}

// Run measures one mode with pages calls on l:
//
//	rtt       pages Pings
//	put_seq   Put pages 0..pages-1 in order
//	get_seq   Get pages 0..pages-1 in order
//	get_rand  Get every page once, in a fixed pseudo-random order
//
// The get modes first Put every page untimed, so reads return written data.
// Each call is timed on its own; the first error ends the run.
func Run(ctx context.Context, l link.Link, mode string, pages uint64) (Measurement, error) {
	if pages == 0 {
		return Measurement{}, errors.New("bench: pages must be > 0")
	}
	buf := make([]byte, pageSize)

	var call func(i uint64) error
	switch mode {
	case "rtt":
		call = func(uint64) error { _, err := l.Ping(ctx); return err }
	case "put_seq":
		call = func(i uint64) error { return l.Put(ctx, i, tag(buf, i)) }
	case "get_seq", "get_rand":
		for i := range pages {
			if err := l.Put(ctx, i, tag(buf, i)); err != nil {
				return Measurement{}, fmt.Errorf("bench: %s prefill page %d: %w", mode, i, err)
			}
		}
		order := func(i uint64) uint64 { return i }
		if mode == "get_rand" {
			perm := randomOrder(pages)
			order = func(i uint64) uint64 { return perm[i] }
		}
		call = func(i uint64) error { return l.Get(ctx, order(i), buf) }
	default:
		return Measurement{}, fmt.Errorf("bench: unknown mode %q", mode)
	}

	m := Measurement{Mode: mode, Samples: make([]time.Duration, pages)}
	start := time.Now()
	for i := range pages {
		t0 := time.Now()
		if err := call(i); err != nil {
			return Measurement{}, fmt.Errorf("bench: %s call %d: %w", mode, i, err)
		}
		m.Samples[i] = time.Since(t0)
	}
	m.Elapsed = time.Since(start)
	return m, nil
}

// randomOrder returns every page in [0, pages) once, shuffled with a fixed
// seed so get_rand reads the same order on every run and every version.
func randomOrder(pages uint64) []uint64 {
	order := make([]uint64, pages)
	for i := range order {
		order[i] = uint64(i)
	}
	rng := rand.New(rand.NewPCG(0xfa, 0x4b))
	rng.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
	return order
}

// tag stamps page number n into buf's first 8 bytes and returns buf.
func tag(buf []byte, n uint64) []byte {
	binary.LittleEndian.PutUint64(buf, n)
	return buf
}
