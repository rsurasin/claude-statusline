package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseDiffOutput(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantAdded   int
		wantRemoved int
	}{
		{"empty", "", 0, 0},
		{"single file", "10\t5\tfile.go", 10, 5},
		{"two files", "10\t5\tfile.go\n3\t1\tother.go", 13, 6},
		{"binary file", "-\t-\tbinary.png", 0, 0},
		{"mixed text and binary", "10\t5\tfile.go\n-\t-\tbinary.png", 10, 5},
		{"zero changes", "0\t0\tempty.go", 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			added, removed := parseDiffOutput(tt.input)
			if added != tt.wantAdded || removed != tt.wantRemoved {
				t.Errorf("parseDiffOutput(%q) = (%d, %d), want (%d, %d)",
					tt.input, added, removed, tt.wantAdded, tt.wantRemoved)
			}
		})
	}
}

// initTestRepo creates a temp git repo with an initial commit and returns its path.
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v failed: %s\n%s", args, err, out)
		}
	}

	// Create and commit a file.
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"git", "add", "hello.txt"},
		{"git", "commit", "-m", "initial"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v failed: %s\n%s", args, err, out)
		}
	}

	return dir
}

func TestGitSegment(t *testing.T) {
	t.Run("valid repo with changes", func(t *testing.T) {
		dir := initTestRepo(t)

		// Make an unstaged change.
		if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello\nworld\n"), 0644); err != nil {
			t.Fatal(err)
		}

		got := gitSegment(dir)
		plain := stripANSI(got)

		repoName := filepath.Base(dir)
		if !strings.Contains(plain, repoName) {
			t.Errorf("gitSegment() = %q, want to contain repo name %q", plain, repoName)
		}
		if !strings.Contains(plain, "main") && !strings.Contains(plain, "master") {
			t.Errorf("gitSegment() = %q, want to contain branch name", plain)
		}
		if !strings.Contains(plain, "+") {
			t.Errorf("gitSegment() = %q, want to contain +N diff", plain)
		}
	})

	t.Run("non-git dir", func(t *testing.T) {
		dir := t.TempDir()
		got := gitSegment(dir)
		if got != "" {
			t.Errorf("gitSegment(non-git) = %q, want empty", got)
		}
	})

	t.Run("empty cwd", func(t *testing.T) {
		got := gitSegment("")
		if got != "" {
			t.Errorf("gitSegment(\"\") = %q, want empty", got)
		}
	})
}

func TestDiffStats(t *testing.T) {
	dir := initTestRepo(t)

	// Unstaged change: add one line.
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello\nworld\n"), 0644); err != nil {
		t.Fatal(err)
	}

	added, removed := diffStats(dir)
	if added != 1 || removed != 0 {
		t.Errorf("diffStats() = (%d, %d), want (1, 0) for one added line", added, removed)
	}

	// Stage the change and add a new unstaged file.
	cmd := exec.Command("git", "add", "hello.txt")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add failed: %s\n%s", err, out)
	}

	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("git", "add", "new.txt")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add failed: %s\n%s", err, out)
	}

	added, removed = diffStats(dir)
	// staged: hello.txt +1, new.txt +1 = 2 added; unstaged: 0
	if added != 2 || removed != 0 {
		t.Errorf("diffStats() = (%d, %d), want (2, 0) for staged changes", added, removed)
	}
}
