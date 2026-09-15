package memsrv

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"math"
	"math/rand/v2"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sw360cab/tb-net/internal/proto"
)

// serve starts a Server on 127.0.0.1:0.
// Cleanup closes it and checks that Serve returned nil.
func serve(t *testing.T, cfg Config) (*Server, string) {
	t.Helper()
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.DiscardHandler)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.Serve(ln) }()
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
		if err := <-done; err != nil {
			t.Errorf("Serve: %v", err)
		}
	})
	return s, ln.Addr().String()
}

func dial(t *testing.T, addr string) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	return c
}

func frame(h proto.Header, payload []byte) []byte {
	b := make([]byte, proto.HeaderSize, proto.HeaderSize+len(payload))
	h.Encode(b)
	return append(b, payload...)
}

// req builds a well-formed request frame.
func req(op proto.Op, seq, page uint64, payload []byte) []byte {
	return frame(proto.Header{
		Magic:     proto.Magic,
		Op:        op,
		PageShift: proto.PageShift4K,
		Seq:       seq,
		Page:      page,
		Len:       uint32(len(payload)),
	}, payload)
}

// reply reads one reply frame and its payload.
func reply(t *testing.T, c net.Conn) (proto.Header, []byte) {
	t.Helper()
	var b [proto.HeaderSize]byte
	if _, err := io.ReadFull(c, b[:]); err != nil {
		t.Fatalf("read reply: %v", err)
	}
	h, err := proto.Decode(b[:])
	if err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	body := make([]byte, h.Len)
	if _, err := io.ReadFull(c, body); err != nil {
		t.Fatalf("read payload: %v", err)
	}
	return h, body
}

// call sends one request frame and returns its reply, checking that the
// reply echoes seq and page.
func call(t *testing.T, c net.Conn, f []byte) (proto.Header, []byte) {
	t.Helper()
	reqHeader, err := proto.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Write(f); err != nil {
		t.Fatalf("write: %v", err)
	}
	h, body := reply(t, c)
	if h.Seq != reqHeader.Seq || h.Page != reqHeader.Page {
		t.Fatalf("reply seq/page %d/%d, want %d/%d", h.Seq, h.Page, reqHeader.Seq, reqHeader.Page)
	}
	return h, body
}

// wantClosed fails unless the server has closed c (EOF or reset, not a timeout).
func wantClosed(t *testing.T, c net.Conn) {
	t.Helper()
	n, err := c.Read(make([]byte, 1))
	if err == nil || errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("read: %d bytes, err %v; want connection closed", n, err)
	}
}

func TestNewSize(t *testing.T) {
	for _, size := range []int{0, -PageSize, PageSize - 1, PageSize + 1} {
		if s, err := New(Config{Size: size}); err == nil {
			s.Close()
			t.Errorf("New(%d): want error", size)
		}
	}
}

func TestNewLock(t *testing.T) {
	s, err := New(Config{Size: 16 * PageSize, Lock: true})
	if err != nil {
		t.Skipf("mlock not permitted here: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPing(t *testing.T) {
	_, addr := serve(t, Config{Size: PageSize})
	h, body := call(t, dial(t, addr), req(proto.OpPing, 7, 0, nil))
	if h.Op != proto.OpPong || h.Flags != 0 || len(body) != 0 {
		t.Fatalf("got %+v with %d bytes", h, len(body))
	}
}

func TestGetUnwrittenIsZero(t *testing.T) {
	_, addr := serve(t, Config{Size: 8 * PageSize})
	h, body := call(t, dial(t, addr), req(proto.OpGet, 1, 5, nil))
	if h.Op != proto.OpGetR || h.Flags != 0 {
		t.Fatalf("got %+v", h)
	}
	if !bytes.Equal(body, make([]byte, PageSize)) {
		t.Fatal("never-written page is not zero")
	}
}

func TestPutGetRandom(t *testing.T) {
	const pages = 256
	_, addr := serve(t, Config{Size: pages * PageSize})
	c := dial(t, addr)
	src := rand.NewChaCha8([32]byte{1})
	rng := rand.New(src)

	want := map[uint64][]byte{}
	for i := range 1000 {
		n := rng.Uint64N(pages)
		p := make([]byte, PageSize)
		_, _ = src.Read(p)
		if h, _ := call(t, c, req(proto.OpPut, uint64(i), n, p)); h.Op != proto.OpPutA || h.Flags != 0 {
			t.Fatalf("put page %d: got %+v", n, h)
		}
		want[n] = p
	}
	for n, p := range want {
		if _, got := call(t, c, req(proto.OpGet, n, n, nil)); !bytes.Equal(got, p) {
			t.Fatalf("page %d: content differs from last put", n)
		}
	}
}

func TestOutOfRange(t *testing.T) {
	const pages = 4
	_, addr := serve(t, Config{Size: pages * PageSize})
	c := dial(t, addr)
	page := bytes.Repeat([]byte{0xab}, PageSize)

	tests := []struct {
		name string
		f    []byte
		op   proto.Op
	}{
		{"get past end", req(proto.OpGet, 1, pages, nil), proto.OpGetR},
		{"get max", req(proto.OpGet, 2, math.MaxUint64, nil), proto.OpGetR},
		{"put past end", req(proto.OpPut, 3, pages, page), proto.OpPutA},
		{"put max", req(proto.OpPut, 4, math.MaxUint64, page), proto.OpPutA},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, body := call(t, c, tt.f)
			if h.Op != tt.op || h.Flags&proto.FlagErr == 0 || len(body) != 0 {
				t.Fatalf("got %+v with %d bytes, want %v with FlagErr and no payload", h, len(body), tt.op)
			}
			// the connection stays usable and in sync
			if h, _ := call(t, c, req(proto.OpPing, 99, 0, nil)); h.Op != proto.OpPong {
				t.Fatalf("ping after error: got %+v", h)
			}
		})
	}
	if _, got := call(t, c, req(proto.OpGet, 5, pages-1, nil)); !bytes.Equal(got, make([]byte, PageSize)) {
		t.Fatal("a rejected put wrote into the buffer")
	}
}

func TestProtocolErrorClosesConn(t *testing.T) {
	_, addr := serve(t, Config{Size: 4 * PageSize})
	good := proto.Header{Magic: proto.Magic, Op: proto.OpGet, PageShift: proto.PageShift4K}
	with := func(f func(*proto.Header)) []byte { h := good; f(&h); return frame(h, nil) }

	tests := []struct {
		name string
		f    []byte
	}{
		{"bad magic", with(func(h *proto.Header) { h.Magic = 0xdeadbeef })},
		{"pageshift 2MB", with(func(h *proto.Header) { h.PageShift = 21 })},
		{"get with len", with(func(h *proto.Header) { h.Len = PageSize })},
		{"put short len", with(func(h *proto.Header) { h.Op, h.Len = proto.OpPut, 100 })},
		{"reply op", with(func(h *proto.Header) { h.Op = proto.OpPong })},
		{"garbage", bytes.Repeat([]byte{0xff}, proto.HeaderSize)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := dial(t, addr)
			if _, err := c.Write(tt.f); err != nil {
				t.Fatal(err)
			}
			wantClosed(t, c)
		})
	}
}

func TestPipelined(t *testing.T) {
	const n = 16
	_, addr := serve(t, Config{Size: n * PageSize})
	c := dial(t, addr)
	var batch []byte
	for i := range n {
		batch = append(batch, req(proto.OpPut, uint64(i), uint64(i), bytes.Repeat([]byte{byte(i)}, PageSize))...)
		batch = append(batch, req(proto.OpGet, uint64(100+i), uint64(i), nil)...)
	}
	errc := make(chan error, 1)
	go func() { _, err := c.Write(batch); errc <- err }()

	for i := range n {
		if h, _ := reply(t, c); h.Op != proto.OpPutA || h.Seq != uint64(i) {
			t.Fatalf("reply %d: got %+v, want PUTA seq %d", 2*i, h, i)
		}
		if h, body := reply(t, c); h.Op != proto.OpGetR || h.Seq != uint64(100+i) || body[0] != byte(i) {
			t.Fatalf("reply %d: got %+v, want GETR seq %d with page %d", 2*i+1, h, 100+i, i)
		}
	}
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
}

func TestOneLogLinePerConn(t *testing.T) {
	var buf bytes.Buffer
	s, addr := serve(t, Config{Size: PageSize, Log: slog.New(slog.NewTextHandler(&buf, nil))})
	c := dial(t, addr)
	for i := range 10 {
		call(t, c, req(proto.OpPing, uint64(i), 0, nil))
	}
	c.Close()
	if err := s.Close(); err != nil { // waits for the handler, so buf is complete
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], "reqs=10") || strings.Contains(lines[0], "err=") {
		t.Fatalf("want one clean line with reqs=10, got:\n%s", buf.String())
	}
}

func TestCloseDisconnectsClients(t *testing.T) {
	s, addr := serve(t, Config{Size: PageSize})
	c := dial(t, addr)
	call(t, c, req(proto.OpPing, 1, 0, nil)) // handler is running
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	wantClosed(t, c)
	if c2, err := net.Dial("tcp", addr); err == nil {
		c2.Close()
		t.Fatal("dial after Close succeeded")
	}
}
