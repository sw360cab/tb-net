package bench

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// ResultsDir is where results live, relative to the repository root. Dirty
// ignores it: a run's own output must not block the next run.
const ResultsDir = "bench/results"

// Commit returns the short HEAD commit of the repository at dir.
func Commit(dir string) (string, error) {
	out, err := git(dir, "rev-parse", "--short", "HEAD")
	return strings.TrimSpace(out), err
}

// Dirty lists the uncommitted changes in the repository at dir, untracked
// files included, ignoring ResultsDir. An empty list means results can be
// attributed to Commit.
func Dirty(dir string) ([]string, error) {
	out, err := git(dir, "status", "--porcelain", "--", ".", ":(exclude)"+ResultsDir)
	if err != nil {
		return nil, err
	}
	var changes []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			changes = append(changes, line)
		}
	}
	return changes, nil
}

func git(dir string, args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("bench: git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
