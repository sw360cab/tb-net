package bench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Result is one ultrabench run: exactly the CLAUDE.md §6 fields, no more.
type Result struct {
	Date         string  `json:"date"` // RFC 3339, UTC
	Env          string  `json:"env"`
	Link         string  `json:"link"`
	Git          string  `json:"git"` // short commit the binary was built from
	Mode         string  `json:"mode"`
	Pages        uint64  `json:"pages"` // pages per mode; pings for rtt
	MTU          int     `json:"mtu"`
	LatUS        Latency `json:"lat_us"`
	MBps         float64 `json:"mbps"`          // MB/s (2^20 bytes) over the timed loop; 0 for rtt
	Faults       uint64  `json:"faults"`        // pager counters: 0 until the pager exists (Day 8)
	Evictions    uint64  `json:"evictions"`     //
	PrefetchHits uint64  `json:"prefetch_hits"` //
}

// Write stores r as indented JSON in dir and returns the file's path. The
// name extends §4's YYYY-MM-DD-<env>-<link>-<git>.json with the mode and the
// time of day, so the modes and repeats of one commit don't collide. It never
// overwrites an existing file.
func Write(dir string, r Result) (string, error) {
	t, err := time.Parse(time.RFC3339, r.Date)
	if err != nil {
		return "", fmt.Errorf("bench: result date: %w", err)
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	t = t.UTC()
	path := filepath.Join(dir, fmt.Sprintf("%s-%s-%s-%s-%s-%s.json",
		t.Format("2006-01-02"), r.Env, r.Link, r.Git, r.Mode, t.Format("150405")))
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		_ = f.Close()
		return "", err
	}
	return path, f.Close()
}
