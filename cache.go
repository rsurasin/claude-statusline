package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

var debugMode = os.Getenv("CLAUDE_STATUSLINE_DEBUG") != ""

// debugf logs to stderr only when CLAUDE_STATUSLINE_DEBUG is set.
func debugf(format string, args ...any) {
	if debugMode {
		fmt.Fprintf(os.Stderr, "statusline: "+format+"\n", args...)
	}
}

// shortHash returns the first 8 hex characters of the SHA-256 of s.
// Used to give each unique key (CWD, session ID) its own cache file.
func shortHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])[:8]
}

const (
	// usageCacheTTL is how long the usage API response is cached (seconds).
	usageCacheTTL = 60
)

// cacheDir is the directory for all statusline temp/cache files.
// It is a var so tests can override it with t.TempDir().
var cacheDir = "/tmp/claude-statusline"

// ensureCacheDir creates the cache directory if it doesn't exist.
func ensureCacheDir() {
	_ = os.MkdirAll(cacheDir, 0755)
}

// atomicWriteFile writes data to path atomically via a temp file + rename.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// claudeConfigDir returns the path to the Claude config directory
// (~/.claude by default, overridable via CLAUDE_CONFIG_DIR).
func claudeConfigDir() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude")
}
