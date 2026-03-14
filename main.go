// Package main implements a custom statusline for Claude Code.
//
// It reads a JSON payload from stdin (provided by Claude Code's statusline hook),
// and outputs two lines of ANSI-colored text:
//
//   - Line 1: model | @agent | think:on | repo:branch +N/-N
//   - Line 2: 43k/200k 42% | 5h ●●●○○ 10% (3h 29m) | 7d ●●○○○ 43% (2d 22h) | extra $5/$50
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
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

// StatusInput is the JSON payload received from Claude Code via stdin.
type StatusInput struct {
	SessionID      string        `json:"session_id"`
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
// model | @agent | think:on | repo:branch +N/-N
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

	if ts := thinkingSegment(in); ts != "" {
		s = append(s, ts)
	}

	cwd := in.Workspace.CurrentDir
	if cwd == "" {
		cwd = in.CWD
	}
	if hasStarship() {
		if g := starshipSegment(cwd); g != "" {
			s = append(s, g)
		}
	} else {
		if g := gitSegment(cwd); g != "" {
			s = append(s, g)
		}
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
