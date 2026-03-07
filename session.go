package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// SessionsIndex is the structure of ~/.claude/projects/<hash>/sessions-index.json.
type SessionsIndex struct {
	Entries []SessionEntry `json:"entries"`
}

// SessionEntry is a single entry in the sessions index.
type SessionEntry struct {
	SessionID   string `json:"sessionId"`
	CustomTitle string `json:"customTitle"`
}

// lookupSessionName resolves the session's custom title.
// It checks sessions-index.json first, then falls back to scanning the
// session JSONL for the last "custom-title" entry.
func lookupSessionName(in *StatusInput) string {
	if in.SessionID == "" {
		return ""
	}

	projectDir := in.Workspace.ProjectDir
	if projectDir == "" {
		projectDir = in.Workspace.CurrentDir
	}
	if projectDir == "" {
		projectDir = in.CWD
	}
	if projectDir == "" {
		return ""
	}

	// Derive the project hash: absolute path with "/" replaced by "-".
	hash := strings.ReplaceAll(projectDir, "/", "-")

	// Try sessions-index.json first.
	indexPath := filepath.Join(claudeConfigDir(), "projects", hash, "sessions-index.json")
	if data, err := os.ReadFile(indexPath); err == nil {
		var idx SessionsIndex
		if json.Unmarshal(data, &idx) == nil {
			for _, entry := range idx.Entries {
				if entry.SessionID == in.SessionID && entry.CustomTitle != "" {
					return entry.CustomTitle
				}
			}
		}
	}

	// Fallback: scan session JSONL for the last "custom-title" entry.
	jsonlPath := in.TranscriptPath
	if jsonlPath == "" {
		jsonlPath = filepath.Join(claudeConfigDir(), "projects", hash, in.SessionID+".jsonl")
	}

	f, err := os.Open(jsonlPath)
	if err != nil {
		return ""
	}
	defer f.Close()

	// Tail-read: seek to last ~16KB for performance on large JSONL files.
	const tailSize = 16384
	info, err := f.Stat()
	if err != nil {
		return ""
	}
	offset := info.Size() - tailSize
	if offset < 0 {
		offset = 0
	}
	if offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return ""
		}
	}

	data, err := io.ReadAll(f)
	if err != nil {
		return ""
	}

	lines := strings.Split(string(data), "\n")

	// If we seeked into the middle, discard the first (partial) line.
	if offset > 0 && len(lines) > 0 {
		lines = lines[1:]
	}

	// Walk backwards to find the most recent "custom-title" entry.
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" || !strings.Contains(line, `"custom-title"`) {
			continue
		}
		var entry JournalEntry
		if json.Unmarshal([]byte(line), &entry) != nil {
			continue
		}
		if entry.Type == "custom-title" && entry.CustomTitle != "" {
			return entry.CustomTitle
		}
	}
	return ""
}
