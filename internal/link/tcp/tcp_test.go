package tcp

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand/v2"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/sw360cab/tb-net/internal/memsrv"
	"github.com/sw360cab/tb-net/internal/proto"
)

// serve starts an in-process memsrv with the given number of pages on
// 127.0.0.1:0. Cleanup closes it.
func serve(t *testing.T, pages int) (*memsrv.Server, string) {
	t.Helper()
	s, err := memsrv.New(memsrv.Config{Size: pages * PageSize, Log: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go s.Serve(ln)
	t.Cleanup(func() { s.Close() })
	return s, ln.Addr().String()
}

// dial connects a Client to addr. Cleanup closes it (before the server).
func dial(t *testing.T, addr string) *Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := Dial(ctx, addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// within runs f and fails the test if it doesn't return within d, so a hang
// shows up as a failure instead of a stuck suite.
func within(t *testing.T, d time.Duration, f func() error) error {
	t.Helper()
	errc := make(chan error, 1)
	go func() { errc <- f() }()
	select {
	case err := <-errc:
		return err
	case <-time.After(d):
		t.Fatalf("call did not return within %v", d)
		return nil
	}
}

// 1k random pages Put then Get through the Client, verified.
func TestPutGetRandom(t *testing.T) {
	const pages = 256
	ctx := context.Background()
	_, addr := serve(t, pages)
	c := dial(t, addr)
	src := rand.NewChaCha8([32]byte{1})
	rng := rand.New(src)

	want := map[uint64][]byte{}
	for range 1000 {
		n := rng.Uint64N(pages)
		p := make([]byte, PageSize)

		_, _ = src.Read(p)
		if err := c.Put(ctx, n, p); err != nil {
			t.Fatalf("Put page %d: got error %v", n, err)
		}
		want[n] = p
	}
	dst := make([]byte, PageSize)
	for n, p := range want {
		err := c.Get(ctx, n, dst)
		if err != nil {
			t.Fatalf("Get page %d: got error %v", n, err)
		}
		if !bytes.Equal(dst, p) {
			t.Fatalf("page %d: content differs from last put", n)
		}
	}
}

// Get of a never-written page returns zeros. dst starts as all 0x01, so the
// test proves Get overwrote it rather than leaving it untouched.
func TestGetUnwrittenIsZero(t *testing.T) {
	var anyPage uint64 = 2
	_, addr := serve(t, 8)
	c := dial(t, addr)
	ctx := context.Background()
	dst := bytes.Repeat([]byte{1}, PageSize) // 4096 bytes, each 0x01

	err := c.Get(ctx, anyPage, dst)
	if err != nil {
		t.Fatalf("Get page %d: got error %v", anyPage, err)
	}
	if !bytes.Equal(dst, make([]byte, PageSize)) {
		t.Fatal("never-written page is not zero")
	}
}

// Out-of-range pages are rejected with errPage and the link stays usable: Get
// and Put on the first page past the end and on math.MaxUint64, each followed
// by a Ping on the same Client.
func TestOutOfRangeKeepsLink(t *testing.T) {
	const pages = 4
	ctx := context.Background()
	_, addr := serve(t, pages)
	c := dial(t, addr)

	buf := make([]byte, PageSize)
	get := func(p uint64) error { return c.Get(ctx, p, buf) }
	put := func(p uint64) error { return c.Put(ctx, p, buf) }

	tests := []struct {
		name      string
		innerCall func(uint64) error
		page      uint64
	}{
		{"get past end", get, pages},
		{"get max", get, math.MaxUint64},
		{"put past end", put, pages},
		{"put max", put, math.MaxUint64},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.innerCall(tt.page); !errors.Is(err, errPage) {
				t.Fatalf("got %v, want errPage", err)
			}
			if _, err := c.Ping(ctx); err != nil {
				t.Fatalf("Ping after rejected page: %v, want the link still usable", err)
			}
		})
	}
}

// A server that goes away must make calls fail, never hang, and leave the
// link broken for good.
func TestServerGoneErrorsNoHang(t *testing.T) {
	ping := func(c *Client, ctx context.Context) func() error {
		return func() error { _, err := c.Ping(ctx); return err }
	}

	t.Run("closed between calls", func(t *testing.T) {
		s, addr := serve(t, 4)
		c := dial(t, addr)
		if err := ping(c, context.Background())(); err != nil {
			t.Fatal(err)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		// no deadline: the close alone has to end the call
		err := within(t, 5*time.Second, ping(c, context.Background()))
		if err == nil {
			t.Fatal("Ping after server close succeeded")
		}
		dst := make([]byte, PageSize)
		err2 := within(t, time.Second, func() error { return c.Get(context.Background(), 0, dst) })
		if err2 != err {
			t.Fatalf("next call: got %v, want the same sticky error %v", err2, err)
		}
	})

	// a server that takes the request and then drops the connection
	t.Run("closed during a call", func(t *testing.T) {
		addr := fakeServer(t, func(conn net.Conn) {
			io.ReadFull(conn, make([]byte, proto.HeaderSize))
			conn.Close()
		})
		c := dial(t, addr)
		if err := within(t, 5*time.Second, ping(c, context.Background())); err == nil {
			t.Fatal("Ping succeeded against a server that closed mid-call")
		}
	})

	// a server that takes the request and never answers: only ctx's deadline ends it
	t.Run("silent server hits ctx deadline", func(t *testing.T) {
		addr := fakeServer(t, func(conn net.Conn) {
			io.Copy(io.Discard, conn) // read forever, reply never
		})
		c := dial(t, addr)
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		err := within(t, 5*time.Second, ping(c, ctx))
		if !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("got %v, want a deadline error", err)
		}
		if err2 := ping(c, context.Background())(); err2 != err {
			t.Fatalf("after timeout: got %v, want the same sticky error %v", err2, err)
		}
	})
}

// fakeServer accepts one connection on 127.0.0.1:0 and hands it to handle.
func fakeServer(t *testing.T, handle func(net.Conn)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		t.Cleanup(func() { conn.Close() })
		handle(conn)
	}()
	return ln.Addr().String()
}

// Zero allocations per call (CLAUDE.md Day 2). Background ctx: a
// WithTimeout ctx allocates on the caller's side, not in the Client.
func TestNoAllocs(t *testing.T) {
	_, addr := serve(t, 4)
	c := dial(t, addr)
	ctx := context.Background()
	page := make([]byte, PageSize)

	tests := []struct {
		name string
		call func() error
	}{
		{"Get", func() error { return c.Get(ctx, 1, page) }},
		{"Put", func() error { return c.Put(ctx, 1, page) }},
		{"Ping", func() error { _, err := c.Ping(ctx); return err }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); err != nil { // warm up buffers and the poller
				t.Fatal(err)
			}
			n := testing.AllocsPerRun(1000, func() {
				if err := tt.call(); err != nil {
					t.Fatal(err)
				}
			})
			if n != 0 {
				t.Fatalf("%v allocations per call, want 0", n)
			}
		})
	}
}

// One Client shared by many goroutines, as the pager's writeback workers
// will do (Day 7): frames must not interleave. make test runs it under -race.
func TestConcurrentCalls(t *testing.T) {
	const workers, perWorker = 8, 64
	_, addr := serve(t, workers*perWorker)
	c := dial(t, addr)
	ctx := context.Background()

	var wg sync.WaitGroup
	errc := make(chan error, workers)
	for w := range workers {
		wg.Go(func() {
			src, dst := make([]byte, PageSize), make([]byte, PageSize)
			for i := range perWorker {
				page := uint64(w*perWorker + i) // each worker owns its pages
				binary.LittleEndian.PutUint64(src, page)
				if err := c.Put(ctx, page, src); err != nil {
					errc <- err
					return
				}
				if err := c.Get(ctx, page, dst); err != nil {
					errc <- err
					return
				}
				if !bytes.Equal(src, dst) {
					errc <- fmt.Errorf("page %d: read back different data", page)
					return
				}
			}
		})
	}
	wg.Wait()
	close(errc)
	for err := range errc {
		t.Error(err)
	}
}

// go test -bench . -benchmem ./internal/link/tcp/ (the spec's allocation check).
func BenchmarkGet(b *testing.B) {
	benchCall(b, func(c *Client, page []byte) error { return c.Get(context.Background(), 1, page) })
}

func BenchmarkPut(b *testing.B) {
	benchCall(b, func(c *Client, page []byte) error { return c.Put(context.Background(), 1, page) })
}

func benchCall(b *testing.B, call func(*Client, []byte) error) {
	s, err := memsrv.New(memsrv.Config{Size: 4 * PageSize, Log: slog.New(slog.DiscardHandler)})
	if err != nil {
		b.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	go s.Serve(ln)
	defer s.Close()
	c, err := Dial(context.Background(), ln.Addr().String())
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()

	page := make([]byte, PageSize)
	b.ReportAllocs()
	b.SetBytes(PageSize)
	for b.Loop() {
		if err := call(c, page); err != nil {
			b.Fatal(err)
		}
	}
}
