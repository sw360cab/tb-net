package bench

import (
	"context"
	"encoding/binary"
	"log/slog"
	"net"
	"slices"
	"testing"

	"github.com/sw360cab/tb-net/internal/link/tcp"
	"github.com/sw360cab/tb-net/internal/memsrv"
)

// client dials an in-process memsrv with the given number of pages.
func client(t *testing.T, pages int) *tcp.Client {
	t.Helper()
	s, err := memsrv.New(memsrv.Config{Size: pages * pageSize, Log: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go s.Serve(ln)
	t.Cleanup(func() { s.Close() })
	c, err := tcp.Dial(context.Background(), ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestRunModes(t *testing.T) {
	const pages = 64
	for _, mode := range Modes {
		t.Run(mode, func(t *testing.T) {
			c := client(t, pages)
			m, err := Run(context.Background(), c, mode, pages)
			if err != nil {
				t.Fatal(err)
			}
			if m.Mode != mode || len(m.Samples) != pages || m.Elapsed <= 0 {
				t.Fatalf("got mode %q, %d samples, elapsed %v", m.Mode, len(m.Samples), m.Elapsed)
			}
			if slices.Contains(m.Samples, 0) {
				t.Fatal("a call was not timed")
			}
			if moves := mode != "rtt"; (m.MBps() > 0) != moves {
				t.Fatalf("MBps %v for %s", m.MBps(), mode)
			}
			if mode == "rtt" {
				return
			}
			// put_seq and the get prefill leave every page tagged with its number
			dst := make([]byte, pageSize)
			for _, p := range []uint64{0, pages / 2, pages - 1} {
				if err := c.Get(context.Background(), p, dst); err != nil {
					t.Fatal(err)
				}
				if got := binary.LittleEndian.Uint64(dst); got != p {
					t.Fatalf("page %d holds tag %d", p, got)
				}
			}
		})
	}
}

func TestRunErrors(t *testing.T) {
	c := client(t, 4)
	ctx := context.Background()
	if _, err := Run(ctx, c, "rtt", 0); err == nil {
		t.Error("pages 0: no error")
	}
	if _, err := Run(ctx, c, "bogus", 4); err == nil {
		t.Error("unknown mode: no error")
	}
	if _, err := Run(ctx, c, "put_seq", 5); err == nil {
		t.Error("more pages than memsrv has: no error")
	}
}

func TestRandomOrderIsFixedPermutation(t *testing.T) {
	a, b := randomOrder(1000), randomOrder(1000)
	if !slices.Equal(a, b) {
		t.Fatal("get_rand order differs between calls: runs would not be comparable")
	}
	sorted := slices.Sorted(slices.Values(a))
	for i, p := range sorted {
		if p != uint64(i) {
			t.Fatalf("not a permutation: sorted[%d] = %d", i, p)
		}
	}
	if slices.IsSorted(a) {
		t.Fatal("order is not shuffled")
	}
}

func TestParseModes(t *testing.T) {
	tests := []struct {
		in      string
		want    []string
		wantErr bool
	}{
		{"all", Modes, false},
		{"rtt", []string{"rtt"}, false},
		{"get_seq, get_rand", []string{"get_seq", "get_rand"}, false},
		{"", nil, true},
		{"rtt,bogus", nil, true},
	}
	for _, tt := range tests {
		got, err := ParseModes(tt.in)
		if (err != nil) != tt.wantErr || !slices.Equal(got, tt.want) {
			t.Errorf("ParseModes(%q) = %v, %v; want %v, err=%v", tt.in, got, err, tt.want, tt.wantErr)
		}
	}
}
