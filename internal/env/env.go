// Package env loads environment files (env/<name>/env.conf, CLAUDE.md §5):
// KEY=VALUE lines that the shell scripts source and the Go binaries parse.
package env

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Conf holds the keys of one environment file.
type Conf map[string]string

// Path returns the file for environment name, relative to the repo root.
func Path(name string) string {
	return filepath.Join("env", name, "env.conf")
}

// Load parses an environment file. Blank lines and # comments are skipped;
// every other line must be KEY=VALUE. Values are literal (no quotes, no
// expansion), so keep them shell-safe.
func Load(path string) (Conf, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parse(f, path)
}

func parse(r io.Reader, name string) (Conf, error) {
	c := Conf{}
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || k == "" || strings.ContainsAny(k, " \t") {
			return nil, fmt.Errorf("%s:%d: want KEY=VALUE, got %q", name, n, line)
		}
		c[k] = v
	}
	return c, sc.Err()
}

// Get returns the value of key, or def if it is unset or empty.
func (c Conf) Get(key, def string) string {
	if v := c[key]; v != "" {
		return v
	}
	return def
}
