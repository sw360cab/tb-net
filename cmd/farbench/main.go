// Command farbench measures a link against memsrv and writes one CLAUDE.md §6
// JSON per mode to -out. It refuses to run on a dirty working tree, so every
// result maps to a commit.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"maps"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/sw360cab/tb-net/internal/bench"
	"github.com/sw360cab/tb-net/internal/env"
	"github.com/sw360cab/tb-net/internal/link"
	"github.com/sw360cab/tb-net/internal/link/tcp"
)

// commit is the source commit, stamped by deploy/push.sh with
// -ldflags "-X main.commit=<sha>" ("<sha>-dirty" for uncommitted changes).
// Empty when built in the repository, where farbench asks git instead.
var commit string

// dialers are the -link implementations; rawl2 and verbs join in Week 3.
var dialers = map[string]func(ctx context.Context, addr string) (link.Link, error){
	"tcp": func(ctx context.Context, addr string) (link.Link, error) {
		c, err := tcp.Dial(ctx, addr)
		if err != nil {
			return nil, err // not a typed nil *tcp.Client inside the interface
		}
		return c, nil
	},
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "farbench:", err)
		os.Exit(1)
	}
}

func run() error {
	envName := flag.String("env", "", "environment name: dials $ADDR_B:$PORT from env/<name>/env.conf (required)")
	linkName := flag.String("link", "tcp", "link implementation: "+strings.Join(slices.Sorted(maps.Keys(dialers)), ", "))
	modeFlag := flag.String("mode", "rtt", "comma-separated modes ("+strings.Join(bench.Modes, ", ")+") or all")
	pages := flag.Uint64("pages", 0, "calls per mode, pages or pings (default: every page of $MEMSRV_MB)")
	out := flag.String("out", bench.ResultsDir, "directory for result JSON")
	timeout := flag.Duration("timeout", 10*time.Minute, "deadline for each mode")
	flag.Parse()

	if *envName == "" {
		return errors.New("-env is required")
	}
	dial, ok := dialers[*linkName]
	if !ok {
		return fmt.Errorf("unknown -link %q", *linkName)
	}
	modes, err := bench.ParseModes(*modeFlag)
	if err != nil {
		return err
	}

	// results must map to a commit: check before spending minutes measuring
	sha, err := bench.SourceCommit(commit, ".")
	if err != nil {
		return err
	}

	conf, err := env.Load(env.Path(*envName))
	if err != nil {
		return err
	}
	mtu, err := strconv.Atoi(conf["MTU"])
	if err != nil {
		return fmt.Errorf("%s: MTU=%q: %w", env.Path(*envName), conf["MTU"], err)
	}
	if *pages == 0 {
		mb, err := strconv.ParseUint(conf["MEMSRV_MB"], 10, 64)
		if err != nil || mb == 0 {
			return fmt.Errorf("%s: MEMSRV_MB=%q: set it or pass -pages", env.Path(*envName), conf["MEMSRV_MB"])
		}
		*pages = mb << 20 >> 12
	}
	addr := net.JoinHostPort(conf["ADDR_B"], conf.Get("PORT", "7070"))

	dialCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	l, err := dial(dialCtx, addr)
	cancel()
	if err != nil {
		return err
	}
	defer l.Close()
	fmt.Printf("farbench %s: env %s, link %s, memsrv %s, %d calls per mode\n", sha, *envName, *linkName, addr, *pages)

	for _, mode := range modes {
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		m, err := bench.Run(ctx, l, mode, *pages)
		cancel()
		if err != nil {
			return err
		}
		lat, _ := bench.Percentiles(m.Samples)
		r := bench.Result{
			Date: time.Now().UTC().Format(time.RFC3339), Env: *envName, Link: *linkName, Git: sha,
			Mode: mode, Pages: *pages, MTU: mtu, LatUS: lat, MBps: roundTo(m.MBps(), 2),
		}
		path, err := bench.Write(*out, r)
		if err != nil {
			return err
		}
		fmt.Printf("%-8s p50 %8.1fµs  p99 %8.1fµs  max %9.1fµs  %7.1f MB/s  %s\n",
			mode, lat.P50, lat.P99, lat.Max, r.MBps, path)
	}
	return nil
}

func roundTo(x float64, digits int) float64 {
	p := 1.0
	for range digits {
		p *= 10
	}
	return float64(int64(x*p+0.5)) / p
}
