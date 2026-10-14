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

// SourceCommit returns the commit results can be attributed to. A binary
// built by deploy/push.sh carries it as stamped; a stamp ending in "-dirty"
// means it was built from uncommitted changes, which is an error. Without a
// stamp, the repository at dir must be clean and its HEAD is used.
func SourceCommit(stamped, dir string) (string, error) {
	switch {
	case strings.HasSuffix(stamped, "-dirty"):
		return "", fmt.Errorf("bench: binary built from uncommitted changes (%s); commit and push again to measure", stamped)
	case stamped != "":
		return stamped, nil
	}
	changes, err := Dirty(dir)
	if err != nil {
		return "", err
	}
	if len(changes) > 0 {
		return "", fmt.Errorf("bench: working tree has uncommitted changes; commit them so results map to a commit:\n  %s",
			strings.Join(changes, "\n  "))
	}
	return Commit(dir)
}

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
	for line := range strings.SplitSeq(out, "\n") {
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
