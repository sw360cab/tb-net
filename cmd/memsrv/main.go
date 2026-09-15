// Command memsrv lends this host's RAM: it locks a buffer and serves 4 KB
// pages from it over the farmem protocol (host B, CLAUDE.md §1).
//
// Flags override the -env file, which overrides the built-in defaults.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"

	"github.com/sw360cab/tb-net/internal/env"
	"github.com/sw360cab/tb-net/internal/memsrv"
)

const (
	defaultPort   = "5555"
	defaultSizeMB = 512
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err) // package errors carry their own "memsrv:" prefix
		os.Exit(1)
	}
}

func run() error {
	listen := flag.String("listen", "", "address to serve on (default $ADDR_B:$PORT from -env, else :"+defaultPort+")")
	sizeMB := flag.Int("size", 0, "buffer size in MB (default $MEMSRV_MB from -env, else "+strconv.Itoa(defaultSizeMB)+")")
	envName := flag.String("env", "", "environment name: reads env/<name>/env.conf")
	flag.Parse()

	conf := env.Conf{}
	if *envName != "" {
		var err error
		if conf, err = env.Load(env.Path(*envName)); err != nil {
			return err
		}
	}

	addr := *listen
	if addr == "" {
		addr = net.JoinHostPort(conf["ADDR_B"], conf.Get("PORT", defaultPort))
	}
	size := *sizeMB
	if size == 0 {
		v := conf.Get("MEMSRV_MB", strconv.Itoa(defaultSizeMB))
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("MEMSRV_MB=%q: %w", v, err)
		}
		size = n
	}

	srv, err := memsrv.New(memsrv.Config{Size: size << 20, Lock: true})
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		_ = srv.Close()
		return err
	}
	slog.Info("memsrv: serving", "addr", ln.Addr().String(), "size_mb", size, "pages", srv.Pages(), "env", *envName)
	return srv.Serve(ln)
}
