package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLookupSessionName(t *testing.T) {
	t.Run("index hit", func(t *testing.T) {
		configDir := t.TempDir()
		t.Setenv("CLAUDE_CONFIG_DIR", configDir)

		projectDir := "/home/user/project"
		hash := "-home-user-project"
		sessionID := "sess-123"

		indexDir := filepath.Join(configDir, "projects", hash)
		if err := os.MkdirAll(indexDir, 0755); err != nil {
			t.Fatal(err)
		}

		idx := SessionsIndex{
			Entries: []SessionEntry{
				{SessionID: sessionID, CustomTitle: "My Session"},
			},
		}
		data, _ := json.Marshal(idx)
		if err := os.WriteFile(filepath.Join(indexDir, "sessions-index.json"), data, 0644); err != nil {
			t.Fatal(err)
		}

		in := &StatusInput{
			SessionID: sessionID,
			Workspace: Workspace{ProjectDir: projectDir},
		}

		got := lookupSessionName(in)
		if got != "My Session" {
			t.Errorf("lookupSessionName() = %q, want %q", got, "My Session")
		}
	})

	t.Run("index miss, JSONL hit", func(t *testing.T) {
		configDir := t.TempDir()
		t.Setenv("CLAUDE_CONFIG_DIR", configDir)

		projectDir := "/home/user/project"
		hash := "-home-user-project"
		sessionID := "sess-456"

		projDir := filepath.Join(configDir, "projects", hash)
		if err := os.MkdirAll(projDir, 0755); err != nil {
			t.Fatal(err)
		}

		// Write index without the session.
		idx := SessionsIndex{Entries: []SessionEntry{
			{SessionID: "other-session", CustomTitle: "Other"},
		}}
		data, _ := json.Marshal(idx)
		if err := os.WriteFile(filepath.Join(projDir, "sessions-index.json"), data, 0644); err != nil {
			t.Fatal(err)
		}

		// Write JSONL with custom-title entry.
		jsonl := `{"type":"user","content":"hello"}
{"type":"custom-title","customTitle":"JSONL Title"}
`
		if err := os.WriteFile(filepath.Join(projDir, sessionID+".jsonl"), []byte(jsonl), 0644); err != nil {
			t.Fatal(err)
		}

		in := &StatusInput{
			SessionID: sessionID,
			Workspace: Workspace{ProjectDir: projectDir},
		}

		got := lookupSessionName(in)
		if got != "JSONL Title" {
			t.Errorf("lookupSessionName() = %q, want %q", got, "JSONL Title")
		}
	})

	t.Run("JSONL fallback via TranscriptPath", func(t *testing.T) {
		dir := t.TempDir()
		jsonlPath := filepath.Join(dir, "transcript.jsonl")

		jsonl := `{"type":"custom-title","customTitle":"Transcript Title"}
`
		if err := os.WriteFile(jsonlPath, []byte(jsonl), 0644); err != nil {
			t.Fatal(err)
		}

		in := &StatusInput{
			SessionID:      "sess-789",
			CWD:            "/some/path",
			TranscriptPath: jsonlPath,
		}

		got := lookupSessionName(in)
		if got != "Transcript Title" {
			t.Errorf("lookupSessionName() = %q, want %q", got, "Transcript Title")
		}
	})

	t.Run("no match", func(t *testing.T) {
		configDir := t.TempDir()
		t.Setenv("CLAUDE_CONFIG_DIR", configDir)

		in := &StatusInput{
			SessionID: "nonexistent",
			CWD:       "/some/path",
		}

		got := lookupSessionName(in)
		if got != "" {
			t.Errorf("lookupSessionName() = %q, want empty", got)
		}
	})

	t.Run("empty SessionID", func(t *testing.T) {
		in := &StatusInput{SessionID: ""}
		got := lookupSessionName(in)
		if got != "" {
			t.Errorf("lookupSessionName() = %q, want empty", got)
		}
	})

	t.Run("multiple JSONL entries — most recent wins", func(t *testing.T) {
		dir := t.TempDir()
		jsonlPath := filepath.Join(dir, "transcript.jsonl")

		jsonl := `{"type":"custom-title","customTitle":"First Title"}
{"type":"custom-title","customTitle":"Latest Title"}
`
		if err := os.WriteFile(jsonlPath, []byte(jsonl), 0644); err != nil {
			t.Fatal(err)
		}

		in := &StatusInput{
			SessionID:      "sess-multi",
			CWD:            "/some/path",
			TranscriptPath: jsonlPath,
		}

		got := lookupSessionName(in)
		if got != "Latest Title" {
			t.Errorf("lookupSessionName() = %q, want %q", got, "Latest Title")
		}
	})
}
