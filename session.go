package main

import (
	"encoding/json"
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

	data, err := os.ReadFile(jsonlPath)
	if err != nil {
		return ""
	}

	var lastTitle string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, `"custom-title"`) {
			continue
		}
		var entry JournalEntry
		if json.Unmarshal([]byte(line), &entry) != nil {
			continue
		}
		if entry.Type == "custom-title" && entry.CustomTitle != "" {
			lastTitle = entry.CustomTitle
		}
	}
	return lastTitle
}
