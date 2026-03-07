package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// JournalEntry is a single line from the session JSONL transcript.
type JournalEntry struct {
	Type             string            `json:"type"`
	CustomTitle      string            `json:"customTitle"`
	ThinkingMetadata *ThinkingMetadata `json:"thinkingMetadata"`
}

// ThinkingMetadata holds thinking mode state from a user message entry.
type ThinkingMetadata struct {
	MaxThinkingTokens int   `json:"maxThinkingTokens"` // newer format (v2.1.37+)
	Disabled          *bool `json:"disabled"`          // older format (v2.1.17)
}

// ThinkingCache is the file-based cache for the thinking mode lookup.
type ThinkingCache struct {
	FetchedAt int64  `json:"fetched_at"`
	SessionID string `json:"session_id"`
	ThinkOn   bool   `json:"think_on"`
}

// thinkingSegment returns "think:on" or "think:off" based on the session JSONL.
func thinkingSegment(in *StatusInput) string {
	on, ok := lookupThinkingCached(in)
	if !ok {
		return ""
	}
	if on {
		return "think" + dim + ":" + reset + green + "on" + reset
	}
	return "think" + dim + ":" + reset + red + "off" + reset
}

// lookupThinkingCached returns the thinking mode with a 10s file-based cache.
func lookupThinkingCached(in *StatusInput) (on bool, ok bool) {
	if in.SessionID == "" {
		return false, false
	}

	cacheFile := filepath.Join(cacheDir, "thinking.json")
	const thinkingCacheTTL = 10

	if data, err := os.ReadFile(cacheFile); err == nil {
		var cache ThinkingCache
		if json.Unmarshal(data, &cache) == nil {
			age := time.Now().Unix() - cache.FetchedAt
			if age < int64(thinkingCacheTTL) && cache.SessionID == in.SessionID {
				return cache.ThinkOn, true
			}
		}
	}

	// Cache miss — do the JSONL lookup.
	thinkOn, found := lookupThinkingMode(in)
	if !found {
		return false, false
	}

	cache := ThinkingCache{
		FetchedAt: time.Now().Unix(),
		SessionID: in.SessionID,
		ThinkOn:   thinkOn,
	}
	if data, err := json.Marshal(&cache); err == nil {
		_ = atomicWriteFile(cacheFile, data, 0600)
	}

	return thinkOn, true
}

// lookupThinkingMode parses the session JSONL to find the most recent
// user message's thinkingMetadata. Returns (thinkingOn, found).
func lookupThinkingMode(in *StatusInput) (bool, bool) {
	if in.SessionID == "" {
		return false, false
	}

	// Prefer transcript_path from stdin JSON; fall back to manual construction.
	jsonlPath := in.TranscriptPath
	if jsonlPath == "" {
		projectDir := in.Workspace.ProjectDir
		if projectDir == "" {
			projectDir = in.Workspace.CurrentDir
		}
		if projectDir == "" {
			projectDir = in.CWD
		}
		if projectDir == "" {
			return false, false
		}
		hash := strings.ReplaceAll(projectDir, "/", "-")
		jsonlPath = filepath.Join(claudeConfigDir(), "projects", hash, in.SessionID+".jsonl")
	}

	f, err := os.Open(jsonlPath)
	if err != nil {
		return false, false
	}
	defer f.Close()

	// Tail-read: seek to last ~16KB for performance on large JSONL files.
	// Human message entries with thinkingMetadata are less frequent than
	// tool result entries, so we need a larger window.
	const tailSize = 16384
	info, err := f.Stat()
	if err != nil {
		return false, false
	}
	offset := info.Size() - tailSize
	if offset < 0 {
		offset = 0
	}
	if offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return false, false
		}
	}

	data, err := io.ReadAll(f)
	if err != nil {
		return false, false
	}

	lines := strings.Split(string(data), "\n")

	// If we seeked into the middle, discard the first (partial) line.
	if offset > 0 && len(lines) > 0 {
		lines = lines[1:]
	}

	// Walk backwards to find the most recent "type":"user" entry that has
	// thinkingMetadata. Most user entries are tool results that lack it —
	// skip those and keep searching for a human message entry.
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if !strings.Contains(line, `"user"`) {
			continue
		}

		var entry JournalEntry
		if json.Unmarshal([]byte(line), &entry) != nil {
			continue
		}
		if entry.Type != "user" {
			continue
		}

		// Tool result entries don't have thinkingMetadata — skip them.
		if entry.ThinkingMetadata == nil {
			continue
		}

		// Newer format: maxThinkingTokens > 0 means ON.
		if entry.ThinkingMetadata.MaxThinkingTokens > 0 {
			return true, true
		}

		// Older format: disabled == false means ON.
		if entry.ThinkingMetadata.Disabled != nil {
			return !*entry.ThinkingMetadata.Disabled, true
		}

		// thinkingMetadata present but maxThinkingTokens == 0 -> OFF.
		return false, true
	}

	return false, false
}
