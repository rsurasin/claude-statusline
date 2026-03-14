package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// starshipAvailable is set once by hasStarship via sync.Once.
var (
	starshipOnce      sync.Once
	starshipAvailable bool
)

// hasStarship returns true if the starship binary is on PATH.
func hasStarship() bool {
	starshipOnce.Do(func() {
		_, err := exec.LookPath("starship")
		starshipAvailable = err == nil
	})
	return starshipAvailable
}

// cwdHash returns the first 8 hex characters of the SHA-256 of cwd,
// used to give each working directory its own cache file.
func cwdHash(cwd string) string {
	h := sha256.Sum256([]byte(cwd))
	return hex.EncodeToString(h[:])[:8]
}

// starshipCache is the file-based cache for a single Starship module output.
type starshipCache struct {
	FetchedAt int64  `json:"fetched_at"`
	Output    string `json:"output"`
}

const starshipCacheTTL = 5 // seconds

// starshipModule runs `starship module <name>` in cwd and returns the output.
// Results are cached per-module with a 5s TTL, keyed by CWD.
func starshipModule(name, cwd string) string {
	if cwd == "" {
		return ""
	}

	cacheFile := filepath.Join(cacheDir, fmt.Sprintf("starship-%s-%s.json", name, cwdHash(cwd)))

	// Check cache.
	if data, err := os.ReadFile(cacheFile); err == nil {
		var cache starshipCache
		if json.Unmarshal(data, &cache) == nil {
			age := time.Now().Unix() - cache.FetchedAt
			if age < int64(starshipCacheTTL) {
				return cache.Output
			}
		}
	}

	// Cache miss — run starship.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "starship", "module", name,
		"--cmd-duration=0", "--status=0", "--jobs=0")
	cmd.Dir = cwd
	cmd.Stderr = nil

	out, err := cmd.Output()
	if err != nil {
		debugf("starship: module %s error: %v", name, err)
		return ""
	}

	result := strings.TrimRight(string(out), " \t\n\r")

	// Write cache.
	cache := starshipCache{
		FetchedAt: time.Now().Unix(),
		Output:    result,
	}
	if data, err := json.Marshal(&cache); err == nil {
		_ = atomicWriteFile(cacheFile, data, 0600)
	}

	return result
}

// starshipSegment returns the Starship-enhanced git segment:
// directory + git_branch + native diff stats.
func starshipSegment(cwd string) string {
	if cwd == "" {
		return ""
	}

	dir := starshipModule("directory", cwd)
	branch := starshipModule("git_branch", cwd)

	if dir == "" && branch == "" {
		return ""
	}

	var b strings.Builder
	if dir != "" {
		b.WriteString(dir)
	}
	if branch != "" {
		// Starship modules include trailing whitespace for prompt spacing,
		// which we strip in starshipModule(). Re-add a separator so the
		// combined output reads "dir on branch" instead of "diron branch".
		if dir != "" {
			b.WriteString(" ")
		}
		b.WriteString(branch)
	}

	added, removed := diffStats(cwd)
	if added > 0 || removed > 0 {
		b.WriteString(" ")
		if added > 0 {
			b.WriteString(green + "+" + strconv.Itoa(added) + reset)
		}
		if removed > 0 {
			if added > 0 {
				b.WriteString(dim + "/" + reset)
			}
			b.WriteString(red + "-" + strconv.Itoa(removed) + reset)
		}
	}

	return b.String()
}
