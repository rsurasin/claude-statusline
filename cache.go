package main

import (
	"os"
	"path/filepath"
)

const (
	// usageCacheTTL is how long the usage API response is cached (seconds).
	usageCacheTTL = 60

	// cacheDir is the directory for all statusline temp/cache files.
	cacheDir = "/tmp/claude-statusline"
)

// ensureCacheDir creates the cache directory if it doesn't exist.
func ensureCacheDir() {
	_ = os.MkdirAll(cacheDir, 0755)
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
