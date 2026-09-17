// Command cli0 is the Day 2 ad-hoc client: it Puts N pages to a memsrv,
// Gets them back, verifies them and prints wall time (CLAUDE.md Day 2,
// "Verify on local"). ultrabench (Day 3) replaces it.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/sw360cab/tb-net/internal/env"
	"github.com/sw360cab/tb-net/internal/link/tcp"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "cli0:", err)
		os.Exit(1)
	}
}

func run() error {
	envName := flag.String("env", "", "environment name: dials $ADDR_B:$PORT from env/<name>/env.conf")
	addr := flag.String("addr", "", "memsrv address (overrides -env)")
	pages := flag.Uint64("pages", 10000, "pages to put, then get and verify")
	timeout := flag.Duration("timeout", time.Minute, "deadline for the whole run")
	flag.Parse()

	if *pages == 0 {
		return errors.New("-pages must be > 0")
	}
	if *addr == "" {
		if *envName == "" {
			return errors.New("need -addr or -env")
		}
		conf, err := env.Load(env.Path(*envName))
		if err != nil {
			return err
		}
		*addr = net.JoinHostPort(conf["ADDR_B"], conf.Get("PORT", "7070"))
	}

	// one deadline for the whole run: every call reuses it, no per-call ctx
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	c, err := tcp.Dial(ctx, *addr)
	if err != nil {
		return err
	}
	defer c.Close()

	rtt, err := c.Ping(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("memsrv %s, ping %v\n", *addr, rtt)

	src := bytes.Repeat([]byte{0xa5}, tcp.PageSize)
	dst := make([]byte, tcp.PageSize)

	start := time.Now()
	for p := range *pages {
		tag(src, p)
		if err := c.Put(ctx, p, src); err != nil {
			return fmt.Errorf("put page %d: %w", p, err)
		}
	}
	report("put", *pages, time.Since(start))

	start = time.Now()
	for p := range *pages {
		if err := c.Get(ctx, p, dst); err != nil {
			return fmt.Errorf("get page %d: %w", p, err)
		}
		if tag(src, p); !bytes.Equal(dst, src) {
			return fmt.Errorf("get page %d: content differs from what was put", p)
		}
	}
	report("get", *pages, time.Since(start))
	return nil
}

// tag stamps the page number into the first 8 bytes, so a page served from
// the wrong slot fails verification.
func tag(page []byte, n uint64) {
	binary.LittleEndian.PutUint64(page, n)
}

func report(what string, n uint64, d time.Duration) {
	fmt.Printf("%-3s %d pages in %v: %v/page, %.1f MB/s\n", what, n, d.Round(time.Millisecond),
		(d / time.Duration(n)).Round(100*time.Nanosecond), float64(n*tcp.PageSize)/(1<<20)/d.Seconds())
}
