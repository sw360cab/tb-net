package bench

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func sample() Result {
	return Result{Date: "2026-09-15T13:04:05Z", Env: "local", Link: "tcp", Git: "abc1234",
		Mode: "get_seq", Pages: 64, MTU: 65520,
		LatUS: Latency{P50: 70.5, P90: 80, P99: 95.25, P999: 120, Max: 300}, MBps: 52.5}
}

// TestResultFieldsMatchContract pins the JSON to CLAUDE.md §6: exactly these
// keys, with these JSON types, and nothing else.
func TestResultFieldsMatchContract(t *testing.T) {
	b, err := json.Marshal(sample())
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}

	// §6: date env link git mode pages mtu lat_us mbps faults evictions prefetch_hits
	wantTypes := map[string]string{
		"date": "string", "env": "string", "link": "string", "git": "string", "mode": "string",
		"pages": "number", "mtu": "number", "lat_us": "object", "mbps": "number",
		"faults": "number", "evictions": "number", "prefetch_hits": "number",
	}
	if keys, want := slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(wantTypes)); !slices.Equal(keys, want) {
		t.Fatalf("keys %v, want %v", keys, want)
	}
	for k, typ := range wantTypes {
		if jsonType(got[k]) != typ {
			t.Errorf("%s is a %s, want %s", k, jsonType(got[k]), typ)
		}
	}

	lat := got["lat_us"].(map[string]any)
	if keys, want := slices.Sorted(maps.Keys(lat)), []string{"max", "p50", "p90", "p99", "p999"}; !slices.Equal(keys, want) {
		t.Fatalf("lat_us keys %v, want %v", keys, want)
	}
	for k, v := range lat {
		if jsonType(v) != "number" {
			t.Errorf("lat_us.%s is a %s, want number", k, jsonType(v))
		}
	}
}

func jsonType(v any) string {
	switch v.(type) {
	case string:
		return "string"
	case float64:
		return "number"
	case map[string]any:
		return "object"
	}
	return "other"
}

func TestWrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "results") // created by Write
	r := sample()
	path, err := Write(dir, r)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "2026-09-15-local-tcp-abc1234-get_seq-130405.json"); path != want {
		t.Fatalf("path %s, want %s", path, want)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var back Result
	if err := json.Unmarshal(b, &back); err != nil || back != r {
		t.Fatalf("read back %+v (%v), want %+v", back, err, r)
	}

	if _, err := Write(dir, r); err == nil {
		t.Fatal("second Write with the same name succeeded, want no overwrite")
	}
	other := r
	other.Mode = "get_rand"
	if _, err := Write(dir, other); err != nil {
		t.Fatalf("another mode in the same second: %v", err)
	}
	bad := r
	bad.Date = "yesterday"
	if _, err := Write(dir, bad); err == nil {
		t.Fatal("Write accepted a non-RFC 3339 date")
	}
}
