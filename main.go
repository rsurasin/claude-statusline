// Package main implements a custom statusline for Claude Code.
//
// It reads a JSON payload from stdin (provided by Claude Code's statusline hook),
// and outputs two lines of ANSI-colored text:
//
//   - Line 1: model | @agent | session | think:on | repo:branch +N/-N
//   - Line 2: 43k/200k 42% | 5h ●●●○○ 10% (3h 29m) | 7d ●●○○○ 43% (2d 22h) | extra $5/$50
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ANSI 16-color codes. These follow whatever terminal palette is active
// (base16, catppuccin, nord, etc.) — no hardcoded RGB values needed.
const (
	reset   = "\033[0m"
	bold    = "\033[1m"
	dim     = "\033[90m" // gray, not actual dim — plays well with terminal and tmux colors
	red     = "\033[31m"
	green   = "\033[32m"
	yellow  = "\033[33m"
	blue    = "\033[34m"
	magenta = "\033[35m"
	cyan    = "\033[36m"
	white   = "\033[37m"
)

const (
	// usageCacheTTL is how long the usage API response is cached (seconds).
	usageCacheTTL = 60

	// cacheDir is the directory for all statusline temp/cache files.
	cacheDir = "/tmp/claude-statusline"
)

// StatusInput is the JSON payload received from Claude Code via stdin.
type StatusInput struct {
	SessionID      string        `json:"session_id"`
	SessionName    string        `json:"session_name"`
	CWD            string        `json:"cwd"`
	Model          Model         `json:"model"`
	Agent          *Agent        `json:"agent"`
	Workspace      Workspace     `json:"workspace"`
	Context        ContextWindow `json:"context_window"`
	Version        string        `json:"version"`
	TranscriptPath string        `json:"transcript_path"`
}

// Model identifies the active Claude model.
type Model struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

// Agent represents a subagent spawned by Claude Code.
type Agent struct {
	Name string `json:"name"`
}

// Workspace holds the current and project directory paths.
type Workspace struct {
	CurrentDir string `json:"current_dir"`
	ProjectDir string `json:"project_dir"`
}

// SessionsIndex is the structure of ~/.claude/projects/<hash>/sessions-index.json.
type SessionsIndex struct {
	Entries []SessionEntry `json:"entries"`
}

// SessionEntry is a single entry in the sessions index.
type SessionEntry struct {
	SessionID   string `json:"sessionId"`
	CustomTitle string `json:"customTitle"`
}

// CurrentUsage holds token counts for the current request.
type CurrentUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

// ContextWindow holds context window size and utilization from Claude Code.
type ContextWindow struct {
	TotalInputTokens    int           `json:"total_input_tokens"`
	TotalOutputTokens   int           `json:"total_output_tokens"`
	ContextWindowSize   int           `json:"context_window_size"`
	UsedPercentage      float64       `json:"used_percentage"`
	RemainingPercentage float64       `json:"remaining_percentage"`
	CurrentUsage        *CurrentUsage `json:"current_usage"`
}

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

func main() {
	ensureCacheDir()

	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "statusline: stdin:", err)
		os.Exit(1)
	}

	var input StatusInput
	if err := json.Unmarshal(raw, &input); err != nil {
		fmt.Fprintln(os.Stderr, "statusline: json:", err)
		os.Exit(1)
	}

	line1 := buildLine1(&input)
	line2 := buildLine2(&input)

	fmt.Print(reset + line1 + "\n" + reset + line2 + reset)
}

// buildLine1 renders the top statusline:
// model | @agent | session | think:on | repo:branch +N/-N
func buildLine1(in *StatusInput) string {
	sep := dim + " | " + reset
	var s []string

	if name := in.Model.DisplayName; name != "" {
		s = append(s, bold+cyan+name+reset)
	}

	// Subagent name (only shown when a subagent is active).
	if in.Agent != nil && in.Agent.Name != "" {
		s = append(s, magenta+"@"+in.Agent.Name+reset)
	}

	// Session name: prefer stdin JSON field, fall back to sessions-index.json.
	name := in.SessionName
	if name == "" {
		name = lookupSessionName(in)
	}
	if name != "" {
		s = append(s, blue+name+reset)
	}

	if ts := thinkingSegment(in); ts != "" {
		s = append(s, ts)
	}

	cwd := in.Workspace.CurrentDir
	if cwd == "" {
		cwd = in.CWD
	}
	if g := gitSegment(cwd); g != "" {
		s = append(s, g)
	}

	return strings.Join(s, sep)
}

// buildLine2 renders the bottom statusline:
// 43k/200k 42% | 5h ●●●○○ 10% (3h 29m) | 7d ●●○○○ 43% (2d 22h) | extra $5/$50
func buildLine2(in *StatusInput) string {
	sep := dim + " | " + reset
	var s []string

	if in.Context.ContextWindowSize > 0 {
		s = append(s, contextSegment(&in.Context))
	}

	usage := fetchUsageCached()
	if usage != nil {
		if usage.FiveHour != nil {
			s = append(s, usageBucketSegment("5h", usage.FiveHour, false, cyan))
		}
		if usage.SevenDay != nil {
			s = append(s, usageBucketSegment("7d", usage.SevenDay, true, blue))
		}

		// Show $used/$limit when actively consuming extra credit.
		if usage.ExtraUsage != nil && usage.ExtraUsage.IsEnabled &&
			usage.ExtraUsage.UsedCredits != nil && usage.ExtraUsage.MonthlyLimit != nil {
			used := *usage.ExtraUsage.UsedCredits / 100   // cents -> dollars
			limit := *usage.ExtraUsage.MonthlyLimit / 100 // cents -> dollars

			color := green
			pct := 0.0
			if limit > 0 {
				pct = (used / limit) * 100
			}
			switch {
			case pct >= 80:
				color = red
			case pct >= 50:
				color = yellow
			}

			if used > 0 {
				s = append(s, fmt.Sprintf("extra %s$%.0f/$%.0f%s",
					color+bold, used, limit, reset))
			}
		}
	}

	return strings.Join(s, sep)
}

// gitSegment returns the git status segment: repo:branch +added/-removed.
func gitSegment(cwd string) string {
	if cwd == "" {
		return ""
	}

	toplevel := gitCmd(cwd, "rev-parse", "--show-toplevel")
	if toplevel == "" {
		return ""
	}

	repoName := filepath.Base(toplevel)
	branch := gitCmd(cwd, "branch", "--show-current")
	if branch == "" {
		branch = gitCmd(cwd, "rev-parse", "--short", "HEAD")
	}

	added, removed := diffStats(cwd)

	var b strings.Builder
	b.WriteString(green + repoName + reset)
	if branch != "" {
		b.WriteString(dim + ":" + reset + magenta + branch + reset)
	}

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

// diffStats returns the total lines added and removed (staged + unstaged).
func diffStats(cwd string) (added, removed int) {
	a1, r1 := parseDiffNumstat(cwd, "diff", "--numstat")
	a2, r2 := parseDiffNumstat(cwd, "diff", "--cached", "--numstat")
	return a1 + a2, r1 + r2
}

// parseDiffNumstat runs git diff --numstat and sums the added/removed lines.
func parseDiffNumstat(cwd string, args ...string) (added, removed int) {
	out := gitCmd(cwd, args...)
	if out == "" {
		return 0, 0
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] == "-" {
			continue
		}
		if a, err := strconv.Atoi(fields[0]); err == nil {
			added += a
		}
		if r, err := strconv.Atoi(fields[1]); err == nil {
			removed += r
		}
	}
	return
}

// gitCmd runs a git command in the given directory and returns trimmed stdout.
func gitCmd(cwd string, args ...string) string {
	cmd := exec.Command("git", append([]string{"-C", cwd, "--no-optional-locks"}, args...)...)
	cmd.Stderr = nil
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
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

// contextSegment renders the context window segment: "43k/200k 42%".
func contextSegment(ctx *ContextWindow) string {
	used := int(float64(ctx.ContextWindowSize) * ctx.UsedPercentage / 100)
	total := ctx.ContextWindowSize
	pct := int(ctx.UsedPercentage)

	color := green
	switch {
	case pct >= 80:
		color = red
	case pct >= 50:
		color = yellow
	}

	return fmt.Sprintf("%s/%s %s%d%%%s",
		humanTokens(used), humanTokens(total),
		color+bold, pct, reset)
}

// humanTokens formats a token count as "43k", "1.2m", etc.
func humanTokens(n int) string {
	switch {
	case n >= 1_000_000:
		f := float64(n) / 1_000_000
		if f == float64(int(f)) {
			return fmt.Sprintf("%dm", int(f))
		}
		return fmt.Sprintf("%.1fm", f)
	case n >= 1_000:
		f := float64(n) / 1_000
		if f == float64(int(f)) {
			return fmt.Sprintf("%dk", int(f))
		}
		return fmt.Sprintf("%.0fk", f)
	default:
		return strconv.Itoa(n)
	}
}

// usageBucketSegment renders a usage bucket: "5h ●●●○○ 10% (3h 29m)".
func usageBucketSegment(label string, bucket *UsageBucket, weekly bool, fillColor string) string {
	pct := int(math.Round(bucket.Utilization))

	pctColor := green
	switch {
	case pct >= 80:
		pctColor = red
	case pct >= 50:
		pctColor = yellow
	}

	// 5 circles: each represents 20%.
	filled := pct / 20
	if filled > 5 {
		filled = 5
	}
	empty := 5 - filled

	var circleBar string
	if filled > 0 {
		circleBar += fillColor + strings.Repeat("●", filled) + reset
	}
	if empty > 0 {
		circleBar += dim + strings.Repeat("○", empty) + reset
	}

	seg := fmt.Sprintf("%s %s %s%d%%%s", label, circleBar, pctColor+bold, pct, reset)

	if bucket.ResetsAt != nil && *bucket.ResetsAt != "" {
		if remaining := timeUntilReset(*bucket.ResetsAt, weekly); remaining != "" {
			seg += dim + " (" + reset + remaining + dim + ")" + reset
		}
	}

	return seg
}

// timeUntilReset parses an ISO 8601 timestamp and returns a compact duration.
// 5-hour buckets: "3h 29m"  |  7-day buckets: "2d 22h"
func timeUntilReset(isoStr string, weekly bool) string {
	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05.999999-07:00",
		"2006-01-02T15:04:05.999999Z",
		"2006-01-02T15:04:05-07:00",
		"2006-01-02T15:04:05Z",
	}

	var t time.Time
	var err error
	for _, layout := range formats {
		t, err = time.Parse(layout, isoStr)
		if err == nil {
			break
		}
	}
	if err != nil {
		return ""
	}

	dur := time.Until(t)
	if dur <= 0 {
		return "now"
	}

	if weekly {
		days := int(dur.Hours()) / 24
		hours := int(dur.Hours()) % 24
		return fmt.Sprintf("%dd %dh", days, hours)
	}

	hours := int(dur.Hours())
	mins := int(dur.Minutes()) % 60
	return fmt.Sprintf("%dh %dm", hours, mins)
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
		_ = os.WriteFile(cacheFile, data, 0600)
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
		// API failed — write negative/stale cache to avoid hammering.
		negCache := UsageCache{FetchedAt: time.Now().Unix(), Data: staleData}
		if data, err := json.Marshal(&negCache); err == nil {
			_ = os.WriteFile(cacheFile, data, 0600)
		}
		return staleData
	}

	cache := UsageCache{
		FetchedAt: time.Now().Unix(),
		Data:      usage,
	}
	if data, err := json.Marshal(&cache); err == nil {
		_ = os.WriteFile(cacheFile, data, 0600)
	}

	return usage
}

// fetchUsageAPI calls the Anthropic usage API and returns the response
// along with the HTTP status code. Returns (nil, statusCode) on failure.
func fetchUsageAPI(token string) (*UsageResponse, int) {
	req, err := http.NewRequest("GET", "https://api.anthropic.com/api/oauth/usage", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "statusline: usage: request error: %v\n", err)
		return nil, 0
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("User-Agent", "claude-statusline/1.0")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "statusline: usage: fetch error: %v\n", err)
		return nil, 0
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		fmt.Fprintf(os.Stderr, "statusline: usage: HTTP %d\n", resp.StatusCode)
		return nil, resp.StatusCode
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "statusline: usage: body read error: %v\n", err)
		return nil, resp.StatusCode
	}

	var usage UsageResponse
	if err := json.Unmarshal(body, &usage); err != nil {
		fmt.Fprintf(os.Stderr, "statusline: usage: json error: %v\nbody: %s\n", err, body)
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
		return creds.ClaudeAiOauth.AccessToken
	}

	fmt.Fprintln(os.Stderr, "statusline: usage: no OAuth token found")
	return ""
}

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
