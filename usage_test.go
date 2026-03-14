package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFetchUsageAPI(t *testing.T) {
	t.Run("200 with valid JSON", func(t *testing.T) {
		want := UsageResponse{
			FiveHour: &UsageBucket{Utilization: 42.5},
			SevenDay: &UsageBucket{Utilization: 10.0},
		}
		body, _ := json.Marshal(want)

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				t.Errorf("Authorization = %q, want %q", got, "Bearer test-token")
			}
			if got := r.Header.Get("anthropic-beta"); got == "" {
				t.Error("anthropic-beta header not set")
			}
			w.WriteHeader(200)
			w.Write(body)
		}))
		defer srv.Close()

		old := usageAPIURL
		usageAPIURL = srv.URL
		defer func() { usageAPIURL = old }()

		usage, code := fetchUsageAPI("test-token")
		if code != 200 {
			t.Fatalf("status = %d, want 200", code)
		}
		if usage == nil || usage.FiveHour == nil || usage.FiveHour.Utilization != 42.5 {
			t.Errorf("FiveHour.Utilization = %v, want 42.5", usage)
		}
		if usage.SevenDay == nil || usage.SevenDay.Utilization != 10.0 {
			t.Errorf("SevenDay.Utilization = %v, want 10.0", usage)
		}
	})

	t.Run("200 with invalid JSON", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
			w.Write([]byte("not json"))
		}))
		defer srv.Close()

		old := usageAPIURL
		usageAPIURL = srv.URL
		defer func() { usageAPIURL = old }()

		usage, code := fetchUsageAPI("test-token")
		if usage != nil {
			t.Errorf("expected nil usage for invalid JSON, got %+v", usage)
		}
		if code != 200 {
			t.Errorf("status = %d, want 200", code)
		}
	})

	t.Run("500 response", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(500)
		}))
		defer srv.Close()

		old := usageAPIURL
		usageAPIURL = srv.URL
		defer func() { usageAPIURL = old }()

		usage, code := fetchUsageAPI("test-token")
		if usage != nil {
			t.Errorf("expected nil usage for 500, got %+v", usage)
		}
		if code != 500 {
			t.Errorf("status = %d, want 500", code)
		}
	})
}

func TestFetchUsageCached(t *testing.T) {
	t.Run("fresh API response is cached", func(t *testing.T) {
		want := UsageResponse{
			FiveHour: &UsageBucket{Utilization: 33.0},
		}
		body, _ := json.Marshal(want)
		callCount := 0

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			w.WriteHeader(200)
			w.Write(body)
		}))
		defer srv.Close()

		old := usageAPIURL
		usageAPIURL = srv.URL
		defer func() { usageAPIURL = old }()

		oldCache := cacheDir
		cacheDir = t.TempDir()
		defer func() { cacheDir = oldCache }()
		ensureCacheDir()

		t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "test-token")

		// First call hits the API.
		usage := fetchUsageCached()
		if usage == nil || usage.FiveHour == nil || usage.FiveHour.Utilization != 33.0 {
			t.Fatalf("first call: got %+v, want FiveHour.Utilization=33.0", usage)
		}
		if callCount != 1 {
			t.Fatalf("expected 1 API call, got %d", callCount)
		}

		// Second call should hit cache, not the API.
		usage = fetchUsageCached()
		if usage == nil || usage.FiveHour == nil || usage.FiveHour.Utilization != 33.0 {
			t.Fatalf("second call: got %+v, want cached result", usage)
		}
		if callCount != 1 {
			t.Errorf("expected 1 API call (cached), got %d", callCount)
		}
	})

	t.Run("no token returns nil", func(t *testing.T) {
		oldCache := cacheDir
		cacheDir = t.TempDir()
		defer func() { cacheDir = oldCache }()

		t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
		t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

		usage := fetchUsageCached()
		if usage != nil {
			t.Errorf("expected nil with no token, got %+v", usage)
		}
	})
}

func TestGetOAuthToken(t *testing.T) {
	t.Run("env var set", func(t *testing.T) {
		t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "env-token-123")
		got := getOAuthToken()
		if got != "env-token-123" {
			t.Errorf("getOAuthToken() = %q, want %q", got, "env-token-123")
		}
	})

	t.Run("credentials file", func(t *testing.T) {
		t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
		configDir := t.TempDir()
		t.Setenv("CLAUDE_CONFIG_DIR", configDir)

		creds := CredentialsFile{
			ClaudeAiOauth: &OAuthCreds{
				AccessToken: "file-token-456",
			},
		}
		data, _ := json.Marshal(creds)
		if err := os.WriteFile(filepath.Join(configDir, ".credentials.json"), data, 0644); err != nil {
			t.Fatal(err)
		}

		got := getOAuthToken()
		if got != "file-token-456" {
			t.Errorf("getOAuthToken() = %q, want %q", got, "file-token-456")
		}
	})

	t.Run("expired token in credentials file", func(t *testing.T) {
		t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
		configDir := t.TempDir()
		t.Setenv("CLAUDE_CONFIG_DIR", configDir)

		creds := CredentialsFile{
			ClaudeAiOauth: &OAuthCreds{
				AccessToken: "expired-token",
				ExpiresAt:   time.Now().Unix() - 3600, // expired 1 hour ago
			},
		}
		data, _ := json.Marshal(creds)
		if err := os.WriteFile(filepath.Join(configDir, ".credentials.json"), data, 0644); err != nil {
			t.Fatal(err)
		}

		got := getOAuthToken()
		if got != "" {
			t.Errorf("getOAuthToken() = %q, want empty for expired token", got)
		}
	})

	t.Run("valid token with future expiry", func(t *testing.T) {
		t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
		configDir := t.TempDir()
		t.Setenv("CLAUDE_CONFIG_DIR", configDir)

		creds := CredentialsFile{
			ClaudeAiOauth: &OAuthCreds{
				AccessToken: "valid-token-789",
				ExpiresAt:   time.Now().Unix() + 3600, // expires in 1 hour
			},
		}
		data, _ := json.Marshal(creds)
		if err := os.WriteFile(filepath.Join(configDir, ".credentials.json"), data, 0644); err != nil {
			t.Fatal(err)
		}

		got := getOAuthToken()
		if got != "valid-token-789" {
			t.Errorf("getOAuthToken() = %q, want %q", got, "valid-token-789")
		}
	})

	t.Run("neither env nor file", func(t *testing.T) {
		t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
		t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

		got := getOAuthToken()
		if got != "" {
			t.Errorf("getOAuthToken() = %q, want empty", got)
		}
	})
}
