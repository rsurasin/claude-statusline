package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLookupThinkingMode(t *testing.T) {
	tests := []struct {
		name   string
		lines  []string
		wantOn bool
		wantOk bool
	}{
		{
			name: "newer format maxThinkingTokens > 0",
			lines: []string{
				`{"type":"user","thinkingMetadata":{"maxThinkingTokens":10000}}`,
			},
			wantOn: true,
			wantOk: true,
		},
		{
			name: "newer format maxThinkingTokens = 0",
			lines: []string{
				`{"type":"user","thinkingMetadata":{"maxThinkingTokens":0}}`,
			},
			wantOn: false,
			wantOk: true,
		},
		{
			name: "older format disabled false",
			lines: []string{
				`{"type":"user","thinkingMetadata":{"disabled":false}}`,
			},
			wantOn: true,
			wantOk: true,
		},
		{
			name: "older format disabled true",
			lines: []string{
				`{"type":"user","thinkingMetadata":{"disabled":true}}`,
			},
			wantOn: false,
			wantOk: true,
		},
		{
			name: "no user entries",
			lines: []string{
				`{"type":"assistant","message":"hello"}`,
			},
			wantOn: false,
			wantOk: false,
		},
		{
			name:   "empty file",
			lines:  []string{},
			wantOn: false,
			wantOk: false,
		},
		{
			name: "multiple entries — most recent wins",
			lines: []string{
				`{"type":"user","thinkingMetadata":{"maxThinkingTokens":10000}}`,
				`{"type":"user","thinkingMetadata":{"maxThinkingTokens":0}}`,
			},
			wantOn: false,
			wantOk: true,
		},
		{
			name: "user entry without thinkingMetadata skipped",
			lines: []string{
				`{"type":"user","content":"tool result"}`,
			},
			wantOn: false,
			wantOk: false,
		},
		{
			name: "user tool result then thinking entry",
			lines: []string{
				`{"type":"user","thinkingMetadata":{"maxThinkingTokens":10000}}`,
				`{"type":"user","content":"tool result"}`,
			},
			wantOn: true,
			wantOk: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			jsonlPath := filepath.Join(dir, "session.jsonl")

			content := ""
			for _, line := range tt.lines {
				content += line + "\n"
			}
			if err := os.WriteFile(jsonlPath, []byte(content), 0644); err != nil {
				t.Fatal(err)
			}

			in := &StatusInput{
				SessionID:      "test-session",
				TranscriptPath: jsonlPath,
			}

			gotOn, gotOk := lookupThinkingMode(in)
			if gotOn != tt.wantOn || gotOk != tt.wantOk {
				t.Errorf("lookupThinkingMode() = (%v, %v), want (%v, %v)",
					gotOn, gotOk, tt.wantOn, tt.wantOk)
			}
		})
	}

	t.Run("empty SessionID", func(t *testing.T) {
		in := &StatusInput{SessionID: ""}
		on, ok := lookupThinkingMode(in)
		if on != false || ok != false {
			t.Errorf("lookupThinkingMode(empty session) = (%v, %v), want (false, false)", on, ok)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		in := &StatusInput{
			SessionID:      "test",
			TranscriptPath: "/nonexistent/path.jsonl",
		}
		on, ok := lookupThinkingMode(in)
		if on != false || ok != false {
			t.Errorf("lookupThinkingMode(missing file) = (%v, %v), want (false, false)", on, ok)
		}
	})
}

func TestLookupThinkingCached(t *testing.T) {
	oldCache := cacheDir
	cacheDir = t.TempDir()
	defer func() { cacheDir = oldCache }()
	ensureCacheDir()

	dir := t.TempDir()
	jsonlPath := filepath.Join(dir, "session.jsonl")
	jsonl := `{"type":"user","thinkingMetadata":{"maxThinkingTokens":10000}}` + "\n"
	if err := os.WriteFile(jsonlPath, []byte(jsonl), 0644); err != nil {
		t.Fatal(err)
	}

	in := &StatusInput{
		SessionID:      "cached-test",
		TranscriptPath: jsonlPath,
	}

	// First call: cache miss, reads JSONL.
	on, ok := lookupThinkingCached(in)
	if !on || !ok {
		t.Errorf("first call = (%v, %v), want (true, true)", on, ok)
	}

	// Verify cache file was written.
	matches, _ := filepath.Glob(filepath.Join(cacheDir, "thinking-*.json"))
	if len(matches) == 0 {
		t.Error("cache file not created")
	}

	// Second call: should hit cache (even if we delete the JSONL).
	os.Remove(jsonlPath)
	on, ok = lookupThinkingCached(in)
	if !on || !ok {
		t.Errorf("cached call = (%v, %v), want (true, true)", on, ok)
	}
}

func TestThinkingSegment(t *testing.T) {
	oldCache := cacheDir
	cacheDir = t.TempDir()
	defer func() { cacheDir = oldCache }()
	ensureCacheDir()

	t.Run("thinking on", func(t *testing.T) {
		dir := t.TempDir()
		jsonlPath := filepath.Join(dir, "session.jsonl")
		jsonl := `{"type":"user","thinkingMetadata":{"maxThinkingTokens":10000}}` + "\n"
		if err := os.WriteFile(jsonlPath, []byte(jsonl), 0644); err != nil {
			t.Fatal(err)
		}

		// Use a unique session ID to avoid cache collisions between subtests.
		in := &StatusInput{
			SessionID:      "segment-on",
			TranscriptPath: jsonlPath,
		}

		got := thinkingSegment(in)
		plain := stripANSI(got)
		if !strings.Contains(plain, "think:on") {
			t.Errorf("thinkingSegment() = %q, want to contain 'think:on'", plain)
		}
	})

	t.Run("thinking off", func(t *testing.T) {
		dir := t.TempDir()
		jsonlPath := filepath.Join(dir, "session.jsonl")
		jsonl := `{"type":"user","thinkingMetadata":{"maxThinkingTokens":0}}` + "\n"
		if err := os.WriteFile(jsonlPath, []byte(jsonl), 0644); err != nil {
			t.Fatal(err)
		}

		in := &StatusInput{
			SessionID:      "segment-off",
			TranscriptPath: jsonlPath,
		}

		got := thinkingSegment(in)
		plain := stripANSI(got)
		if !strings.Contains(plain, "think:off") {
			t.Errorf("thinkingSegment() = %q, want to contain 'think:off'", plain)
		}
	})

	t.Run("no thinking metadata", func(t *testing.T) {
		in := &StatusInput{SessionID: ""}
		got := thinkingSegment(in)
		if got != "" {
			t.Errorf("thinkingSegment() = %q, want empty", got)
		}
	})
}
