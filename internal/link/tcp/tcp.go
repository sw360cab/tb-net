// Package tcp implements link.Link over a single TCP connection to memsrv.
package tcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/sw360cab/tb-net/internal/link"
	"github.com/sw360cab/tb-net/internal/proto"
)

// PageSize is the size of the page carried by Get and Put.
const PageSize = 1 << proto.PageShift4K

// Client is a link.Link over one TCP connection. Calls are serialized: one
// request is on the wire at a time. After an I/O error, a timeout or a
// protocol violation the link is broken and every later call returns the
// same error; reconnecting is a later step (CLAUDE.md Day 5).
type Client struct {
	conn net.Conn

	mu  sync.Mutex
	buf [proto.HeaderSize + PageSize]byte // one frame; reused so calls don't allocate
	seq uint64
	err error // sticky once the stream can't be trusted
}

var _ link.Link = (*Client)(nil)

// errPage is returned when memsrv rejects a page (FlagErr), e.g. out of
// range. The link stays usable.
var errPage = errors.New("tcp: page rejected by server")

// Dial connects to the memsrv at addr. ctx bounds the connect only; each
// call takes its own ctx.
func Dial(ctx context.Context, addr string) (*Client, error) {
	var d net.Dialer
	c, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	// Go's default already, but latency is the product: make it explicit
	// TCP_NO_DELAY
	if err := c.(*net.TCPConn).SetNoDelay(true); err != nil {
		_ = c.Close()
		return nil, err
	}
	return &Client{conn: c}, nil
}

// Close closes the connection. A call blocked on the network returns an error.
func (c *Client) Close() error {
	return c.conn.Close()
}

// roundTrip sends one request and reads its reply. out is the request
// payload (PUT), in receives the reply payload (GETR); the reply's Len bytes
// land in in[:Len]. It checks the reply is the answer to this request (seq,
// page, op) and returns its header; the caller checks Flags and Len.
//
// Only ctx's deadline is honored once the call is on the wire; a cancel
// without a deadline is noticed only before sending.
func (c *Client) roundTrip(ctx context.Context, op proto.Op, page uint64, out, in []byte) (proto.Header, error) {
	if len(out) > PageSize {
		return proto.Header{}, fmt.Errorf("tcp: %v payload %d bytes, max %d", op, len(out), PageSize)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return proto.Header{}, c.err
	}
	if err := ctx.Err(); err != nil {
		return proto.Header{}, err // nothing sent yet: the stream is intact
	}

	rep, err := c.exchange(ctx, op, page, out, in)
	if err != nil {
		// we may be mid-frame: no later reply can be trusted
		c.err = fmt.Errorf("tcp: link broken: %w", err)
		return proto.Header{}, c.err
	}
	return rep, nil
}

// exchange does the I/O for roundTrip; c.mu is held.
// Any error it returns breaks the link.
func (c *Client) exchange(ctx context.Context, op proto.Op, page uint64, out, in []byte) (proto.Header, error) {
	deadline, _ := ctx.Deadline() // zero means none
	if err := c.conn.SetDeadline(deadline); err != nil {
		return proto.Header{}, err
	}

	// one write for header + payload: copying 4 KB costs ~100 ns against a ~10 µs RTT
	c.seq++
	req := proto.Header{
		Magic:     proto.Magic,
		Op:        op,
		PageShift: proto.PageShift4K,
		Seq:       c.seq,
		Page:      page,
		Len:       uint32(len(out))}
	req.Encode(c.buf[:])
	writtenBytes := proto.HeaderSize + copy(c.buf[proto.HeaderSize:], out)
	if _, err := c.conn.Write(c.buf[:writtenBytes]); err != nil {
		return proto.Header{}, err
	}

	if _, err := io.ReadFull(c.conn, c.buf[:proto.HeaderSize]); err != nil {
		return proto.Header{}, err
	}
	rep, err := proto.Decode(c.buf[:proto.HeaderSize])
	if err != nil {
		return proto.Header{}, err
	}
	if want := replyOp(op); rep.Op != want || rep.Seq != req.Seq || rep.Page != page {
		return proto.Header{}, fmt.Errorf("reply %v seq %d page %d, want %v seq %d page %d",
			rep.Op, rep.Seq, rep.Page, want, req.Seq, page)
	}
	if int(rep.Len) > len(in) {
		return proto.Header{}, fmt.Errorf("%v payload %d bytes, room for %d", rep.Op, rep.Len, len(in))
	}
	if _, err := io.ReadFull(c.conn, in[:rep.Len]); err != nil {
		return proto.Header{}, err
	}
	return rep, nil
}

// replyOp is the op memsrv answers op with.
func replyOp(op proto.Op) proto.Op {
	switch op {
	case proto.OpPing:
		return proto.OpPong
	case proto.OpGet:
		return proto.OpGetR
	case proto.OpPut:
		return proto.OpPutA
	}
	return 0
}

// Ping measures one round trip to memsrv.
func (c *Client) Ping(ctx context.Context) (time.Duration, error) {
	t := time.Now()
	if _, err := c.roundTrip(ctx, proto.OpPing, 0, nil, nil); err != nil {
		return 0, err
	}
	return time.Since(t), nil
}

// Get reads page into dst, which must be PageSize bytes.
func (c *Client) Get(ctx context.Context, page uint64, dst []byte) error {
	if len(dst) != PageSize {
		return fmt.Errorf("tcp: Get dst is %d bytes, want %d", len(dst), PageSize)
	}
	rep, err := c.roundTrip(ctx, proto.OpGet, page, nil, dst)
	if err != nil {
		return err // link broken or ctx expired before sending
	}
	if rep.Flags&proto.FlagErr != 0 {
		return errPage // e.g. page out of range; link still fine
	}
	if rep.Len != PageSize {
		return fmt.Errorf("tcp: page %d: reply has %d bytes, want %d", page, rep.Len, PageSize)
	}
	return nil
}

// Put writes a page from src, which must be PageSize bytes.
func (c *Client) Put(ctx context.Context, page uint64, src []byte) error {
	if len(src) != PageSize {
		return fmt.Errorf("tcp: Put src is %d bytes, want %d", len(src), PageSize)
	}
	rep, err := c.roundTrip(ctx, proto.OpPut, page, src, nil)
	if err != nil {
		return err // link broken or ctx expired before sending
	}
	if rep.Flags&proto.FlagErr != 0 {
		return errPage // e.g. page out of range; link still fine
	}
	return nil
}
