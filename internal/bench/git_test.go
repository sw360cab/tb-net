package bench

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
)

// repo makes a git repository with one committed file.
func repo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	write(t, dir, "main.go", "package main\n")
	run("add", ".")
	run("commit", "-q", "-m", "init")
	return dir
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCommit(t *testing.T) {
	sha, err := Commit(repo(t))
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{7,}$`).MatchString(sha) {
		t.Fatalf("Commit = %q, want a short hex sha", sha)
	}
}

func TestDirty(t *testing.T) {
	tests := []struct {
		name      string
		change    func(t *testing.T, dir string)
		wantDirty bool
	}{
		{"clean", func(*testing.T, string) {}, false},
		{"new result only", func(t *testing.T, dir string) {
			write(t, dir, ResultsDir+"/2026-09-15-local-tcp-abc-rtt-120000.json", "{}")
		}, false},
		{"tracked file modified", func(t *testing.T, dir string) { write(t, dir, "main.go", "package main // edited\n") }, true},
		{"untracked source file", func(t *testing.T, dir string) { write(t, dir, "new.go", "package main\n") }, true},
		{"file next to results", func(t *testing.T, dir string) { write(t, dir, "bench/plot.py", "") }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := repo(t)
			tt.change(t, dir)
			changes, err := Dirty(dir)
			if err != nil {
				t.Fatal(err)
			}
			if (len(changes) > 0) != tt.wantDirty {
				t.Fatalf("changes %q, want dirty=%v", changes, tt.wantDirty)
			}
		})
	}
}

func TestNotARepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	if _, err := Dirty(t.TempDir()); err == nil {
		t.Fatal("Dirty outside a repository succeeded")
	}
}
