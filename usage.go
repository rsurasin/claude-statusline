package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// UsageResponse is the Anthropic usage API response.
type UsageResponse struct {
	FiveHour   *UsageBucket `json:"five_hour"`
	SevenDay   *UsageBucket `json:"seven_day"`
	ExtraUsage *ExtraUsage  `json:"extra_usage"`
}

// UsageBucket represents a single usage window (5-hour or 7-day).
type UsageBucket struct {
	Utilization float64 `json:"utilization"`
	ResetsAt    *string `json:"resets_at"`
}

// ExtraUsage holds extended/overuse credit information.
type ExtraUsage struct {
	IsEnabled    bool     `json:"is_enabled"`
	MonthlyLimit *float64 `json:"monthly_limit"` // cents
	UsedCredits  *float64 `json:"used_credits"`  // cents
}

// CredentialsFile is the structure of ~/.claude/.credentials.json.
type CredentialsFile struct {
	ClaudeAiOauth *OAuthCreds `json:"claudeAiOauth"`
}

// OAuthCreds holds the OAuth credentials for Claude AI.
type OAuthCreds struct {
	AccessToken      string   `json:"accessToken"`
	RefreshToken     string   `json:"refreshToken"`
	ExpiresAt        int64    `json:"expiresAt"`
	Scopes           []string `json:"scopes"`
	SubscriptionType string   `json:"subscriptionType"`
	RateLimitTier    string   `json:"rateLimitTier"`
}

// UsageCache is the file-based cache for the usage API response.
type UsageCache struct {
	FetchedAt int64          `json:"fetched_at"`
	Data      *UsageResponse `json:"data"`
}

// fetchUsageCached returns the usage API response with a 60s file-based cache.
// On cache miss, it fetches from the API. On failure, returns stale cached data.
func fetchUsageCached() *UsageResponse {
	cacheFile := filepath.Join(cacheDir, "usage.json")

	var staleData *UsageResponse
	if data, err := os.ReadFile(cacheFile); err == nil {
		var cache UsageCache
		if json.Unmarshal(data, &cache) == nil {
			age := time.Now().Unix() - cache.FetchedAt
			if cache.Data != nil && age < int64(usageCacheTTL) {
				return cache.Data // fresh cache hit
			}
			// Negative cache: don't retry API for 60s after a failure.
			if cache.Data == nil && age < 60 {
				return nil
			}
			if cache.Data != nil {
				staleData = cache.Data // keep for fallback
			}
		}
	}

	token := getOAuthToken()
	if token == "" {
		return staleData
	}

	usage, _ := fetchUsageAPI(token)
	if usage == nil {
		if staleData != nil {
			debugf("usage: API failed, serving stale cached data")
		} else {
			debugf("usage: API failed, no cached data to fall back on")
		}
		// API failed — write negative/stale cache to avoid hammering.
		negCache := UsageCache{FetchedAt: time.Now().Unix(), Data: staleData}
		if data, err := json.Marshal(&negCache); err == nil {
			_ = atomicWriteFile(cacheFile, data, 0600)
		}
		return staleData
	}

	cache := UsageCache{
		FetchedAt: time.Now().Unix(),
		Data:      usage,
	}
	if data, err := json.Marshal(&cache); err == nil {
		_ = atomicWriteFile(cacheFile, data, 0600)
	}

	return usage
}

// usageAPIURL is the endpoint for the Anthropic usage API.
// It is a var so tests can override it with an httptest server URL.
var usageAPIURL = "https://api.anthropic.com/api/oauth/usage"

// fetchUsageAPI calls the Anthropic usage API and returns the response
// along with the HTTP status code. Returns (nil, statusCode) on failure.
func fetchUsageAPI(token string) (*UsageResponse, int) {
	req, err := http.NewRequest("GET", usageAPIURL, nil)
	if err != nil {
		debugf("usage: request error: %v", err)
		return nil, 0
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("User-Agent", "claude-statusline/1.0")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		debugf("usage: fetch error: %v", err)
		return nil, 0
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		debugf("usage: HTTP %d", resp.StatusCode)
		return nil, resp.StatusCode
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		debugf("usage: body read error: %v", err)
		return nil, resp.StatusCode
	}

	var usage UsageResponse
	if err := json.Unmarshal(body, &usage); err != nil {
		debugf("usage: json error: %v\nbody: %s", err, body)
		return nil, resp.StatusCode
	}
	return &usage, resp.StatusCode
}

// getOAuthToken reads the OAuth access token from the environment or
// from ~/.claude/.credentials.json.
func getOAuthToken() string {
	if token := os.Getenv("CLAUDE_CODE_OAUTH_TOKEN"); token != "" {
		return token
	}

	path := filepath.Join(claudeConfigDir(), ".credentials.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	var creds CredentialsFile
	if json.Unmarshal(data, &creds) != nil {
		return ""
	}
	if creds.ClaudeAiOauth != nil && creds.ClaudeAiOauth.AccessToken != "" {
		if creds.ClaudeAiOauth.ExpiresAt > 0 && time.Now().Unix() >= creds.ClaudeAiOauth.ExpiresAt {
			debugf("usage: OAuth token expired at %d", creds.ClaudeAiOauth.ExpiresAt)
			return ""
		}
		return creds.ClaudeAiOauth.AccessToken
	}

	debugf("usage: no OAuth token found")
	return ""
}
