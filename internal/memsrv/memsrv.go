// Package memsrv serves 4 KB pages of a memory buffer over the proto wire
// protocol: the host B side of farmem (CLAUDE.md §1). cmd/memsrv wraps it.
package memsrv

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"syscall"
	"time"

	"github.com/sw360cab/tb-net/internal/proto"
)

// PageSize is the size of every page memsrv serves.
const PageSize = 1 << proto.PageShift4K

// bufSize holds several pipelined requests or replies (header + page each).
const bufSize = 64 << 10

// Config configures a Server.
type Config struct {
	Size int          // buffer size in bytes, a positive multiple of PageSize
	Lock bool         // mlock the buffer; New fails if the kernel refuses
	Log  *slog.Logger // one line per connection; nil means slog.Default()
}

// Server owns the page buffer and the connections that read and write it.
//
// Access to a page is not synchronized across connections: two connections
// writing the same page at once can leave it torn. farmem runs one pager per
// memsrv, so this is by design.
type Server struct {
	mem   []byte
	pages uint64
	log   *slog.Logger
	conns map[net.Conn]struct{}
	mu    sync.Mutex // guards ln, conns and the closing of done
	ln    net.Listener
	done  chan struct{} // closed by Close()
	wg    sync.WaitGroup
}

// connStats track the connection received for the whole Server
type connStats struct {
	reqs uint64
	gets uint64
	puts uint64
	errs uint64
}

// New maps a zeroed buffer of cfg.Size bytes and, with cfg.Lock, locks it into RAM.
func New(cfg Config) (*Server, error) {
	if cfg.Size <= 0 || cfg.Size%PageSize != 0 {
		return nil, fmt.Errorf("memsrv: size %d is not a positive multiple of %d", cfg.Size, PageSize)
	}
	// mmap rather than make: page-aligned, outside the Go heap and GC pacing
	mem, err := syscall.Mmap(-1, 0, cfg.Size, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_ANON|syscall.MAP_PRIVATE)
	if err != nil {
		return nil, fmt.Errorf("memsrv: mmap %d bytes: %w", cfg.Size, err)
	}
	if cfg.Lock {
		if err := syscall.Mlock(mem); err != nil {
			_ = syscall.Munmap(mem)
			return nil, fmt.Errorf("memsrv: mlock %d MB: %w (raise RLIMIT_MEMLOCK, e.g. LimitMEMLOCK=infinity, or run with CAP_IPC_LOCK)", cfg.Size>>20, err)
		}
	}
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	return &Server{
		mem:   mem,
		pages: uint64(cfg.Size / PageSize),
		log:   log,
		done:  make(chan struct{}),
		conns: make(map[net.Conn]struct{}),
	}, nil
}

// Pages returns the number of pages in the buffer.
func (s *Server) Pages() uint64 {
	return s.pages
}

// Serve accepts connections on l until Close, one goroutine each. It returns
// nil after Close, otherwise the error that stopped it. Call it at most once.
func (s *Server) Serve(l net.Listener) error {
	s.mu.Lock()
	select {
	case <-s.done:
		s.mu.Unlock()
		_ = l.Close()
		return nil
	default:
	}
	s.ln = l
	s.mu.Unlock()

	for {
		c, err := l.Accept()
		if err != nil {
			select {
			case <-s.done: // Close closed the listener
				return nil
			default:
				return err
			}
		}
		if !s.track(c) {
			_ = c.Close()
			return nil
		}
		go s.serveConn(c)
	}
}

// Close stops Serve, closes every connection, waits for their handlers to
// return and releases the buffer. It is safe to call more than once.
func (s *Server) Close() error {
	s.mu.Lock()
	select {
	case <-s.done:
		s.mu.Unlock()
		return nil
	default:
	}
	close(s.done)
	if s.ln != nil {
		_ = s.ln.Close()
	}
	for c := range s.conns {
		_ = c.Close()
	}
	s.mu.Unlock()

	s.wg.Wait() // no handler may touch mem after munmap
	return syscall.Munmap(s.mem)
}

func (s *Server) track(c net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.done:
		return false
	default:
	}
	s.conns[c] = struct{}{}
	s.wg.Add(1)
	return true
}

// serveConn forward the request to handle
func (s *Server) serveConn(c net.Conn) {
	defer s.wg.Done()
	start := time.Now()

	st, err := s.handle(c)
	_ = c.Close()

	s.mu.Lock()
	delete(s.conns, c)
	s.mu.Unlock()

	// preparing attributes to be logged
	attrs := []any{"remote", c.RemoteAddr().String(), "reqs", st.reqs, "gets", st.gets,
		"puts", st.puts, "errs", st.errs, "dur", time.Since(start).Round(time.Millisecond)}

	// additional attributes in case of error
	if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) { // handle only returns on error
		attrs = append(attrs, "err", err)
	}
	s.log.Info("memsrv: conn closed", attrs...)
}

// handle serves one connection until it fails. A malformed frame ends the
// connection: after one, the stream can't be trusted to be in sync.
func (s *Server) handle(c net.Conn) (connStats, error) {
	var st connStats
	var in, out [proto.HeaderSize]byte

	br := bufio.NewReaderSize(c, bufSize)
	bw := bufio.NewWriterSize(c, bufSize)
	for {
		if _, err := io.ReadFull(br, in[:]); err != nil {
			return st, err
		}
		req, err := proto.Decode(in[:])
		if err != nil {
			return st, err
		}
		if err := checkFrame(req); err != nil {
			return st, err
		}
		st.reqs++

		rep := proto.Header{
			Magic:     proto.Magic,
			PageShift: proto.PageShift4K,
			Seq:       req.Seq,
			Page:      req.Page,
		}
		var payload []byte
		switch req.Op {
		case proto.OpPing:
			rep.Op = proto.OpPong
		case proto.OpGet:
			st.gets++
			rep.Op = proto.OpGetR
			if p, ok := s.page(req.Page); ok {
				rep.Len, payload = PageSize, p
			} else {
				rep.Flags = proto.FlagErr
				st.errs++
			}
		case proto.OpPut:
			st.puts++
			rep.Op = proto.OpPutA
			if p, ok := s.page(req.Page); ok {
				_, err = io.ReadFull(br, p)
			} else {
				_, err = br.Discard(PageSize) // keep the stream in sync
				rep.Flags = proto.FlagErr
				st.errs++
			}
			if err != nil {
				return st, err
			}
		}

		rep.Encode(out[:])
		if _, err := bw.Write(out[:]); err != nil {
			return st, err
		}
		if _, err := bw.Write(payload); err != nil {
			return st, err
		}
		// flush once every pipelined request already read has its reply
		if br.Buffered() == 0 {
			if err := bw.Flush(); err != nil {
				return st, err
			}
		}
	}
}

// checkFrame enforces the framing memsrv relies on: 4 KB pages, requests
// only, and a payload exactly when the op carries one.
func checkFrame(h proto.Header) error {
	if h.PageShift != proto.PageShift4K {
		return fmt.Errorf("memsrv: pageshift %d, want %d", h.PageShift, proto.PageShift4K)
	}
	var want uint32
	switch h.Op {
	case proto.OpPing, proto.OpGet:
	case proto.OpPut:
		want = PageSize
	default:
		return fmt.Errorf("memsrv: %v is not a request", h.Op)
	}
	if h.Len != want {
		return fmt.Errorf("memsrv: %v with len %d, want %d", h.Op, h.Len, want)
	}
	return nil
}

// page returns page n of the buffer, or false if n is out of range.
func (s *Server) page(n uint64) ([]byte, bool) {
	if n >= s.pages {
		return nil, false
	}
	off := n * PageSize
	return s.mem[off : off+PageSize : off+PageSize], true
}
