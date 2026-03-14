package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func skipWithoutStarship(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("starship"); err != nil {
		t.Skip("starship not installed")
	}
}

func TestHasStarship(t *testing.T) {
	// Just verify it returns a bool without panicking.
	_ = hasStarship()
}

func TestStarshipModule(t *testing.T) {
	skipWithoutStarship(t)

	t.Run("directory in git repo", func(t *testing.T) {
		dir := initTestRepo(t)
		out := starshipModule("directory", dir)
		if out == "" {
			t.Error("starshipModule(directory) returned empty for git repo")
		}
	})

	t.Run("git_branch in git repo", func(t *testing.T) {
		dir := initTestRepo(t)
		out := starshipModule("git_branch", dir)
		if out == "" {
			t.Error("starshipModule(git_branch) returned empty for git repo")
		}
	})

	t.Run("git_branch in non-git dir", func(t *testing.T) {
		dir := t.TempDir()
		out := starshipModule("git_branch", dir)
		if out != "" {
			t.Errorf("starshipModule(git_branch, /tmp) = %q, want empty", out)
		}
	})

	t.Run("empty cwd", func(t *testing.T) {
		out := starshipModule("directory", "")
		if out != "" {
			t.Errorf("starshipModule(directory, \"\") = %q, want empty", out)
		}
	})
}

func TestStarshipSegment(t *testing.T) {
	skipWithoutStarship(t)

	dir := initTestRepo(t)

	// Make an unstaged change so diff stats appear.
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello\nworld\n"), 0644); err != nil {
		t.Fatal(err)
	}

	got := starshipSegment(dir)
	plain := stripANSI(got)

	if !strings.Contains(plain, "+") {
		t.Errorf("starshipSegment() = %q, want to contain diff stats", plain)
	}
}

func TestStarshipSegmentNonGit(t *testing.T) {
	skipWithoutStarship(t)

	dir := t.TempDir()
	got := starshipSegment(dir)
	// In a non-git dir, git_branch is empty. directory may or may not render.
	// Just verify it doesn't panic.
	_ = got
}

func TestStarshipModuleCaching(t *testing.T) {
	skipWithoutStarship(t)

	origCacheDir := cacheDir
	cacheDir = t.TempDir()
	defer func() { cacheDir = origCacheDir }()

	dir := initTestRepo(t)
	_ = starshipModule("directory", dir)

	cacheFile := filepath.Join(cacheDir, "starship-directory.json")
	if _, err := os.Stat(cacheFile); os.IsNotExist(err) {
		t.Error("cache file was not created")
	}
}
