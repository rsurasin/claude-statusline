package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWriteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")
	content := []byte(`{"key":"value"}`)

	if err := atomicWriteFile(path, content, 0600); err != nil {
		t.Fatalf("atomicWriteFile() error: %v", err)
	}

	// Verify content.
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("content = %q, want %q", got, content)
	}

	// Verify no .tmp file remains.
	tmpPath := path + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Errorf("temp file %q still exists after write", tmpPath)
	}
}

func TestClaudeConfigDir(t *testing.T) {
	t.Run("env var set", func(t *testing.T) {
		t.Setenv("CLAUDE_CONFIG_DIR", "/custom/config")
		got := claudeConfigDir()
		if got != "/custom/config" {
			t.Errorf("claudeConfigDir() = %q, want %q", got, "/custom/config")
		}
	})

	t.Run("default", func(t *testing.T) {
		t.Setenv("CLAUDE_CONFIG_DIR", "")
		got := claudeConfigDir()
		home, _ := os.UserHomeDir()
		want := filepath.Join(home, ".claude")
		if got != want {
			t.Errorf("claudeConfigDir() = %q, want %q", got, want)
		}
	})
}
