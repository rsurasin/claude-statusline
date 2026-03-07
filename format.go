package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// contextSegment renders the context window segment: "43k/200k 42%".
func contextSegment(ctx *ContextWindow) string {
	// Derive used tokens from UsedPercentage × window size rather than summing
	// TotalInputTokens + TotalOutputTokens. Claude Code's UsedPercentage includes
	// cache tokens and other overhead not reflected in the raw I/O counts, so the
	// percentage is the authoritative source. Deriving both values from it keeps
	// the displayed "Nk/Nk N%" self-consistent.
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

// humanTokens formats a token count as a compact, human-friendly string.
// Uses one decimal place for sub-10k values to preserve precision:
//
//	1500  → "1.5k"  (not "2k" — avoids rounding up)
//	1300  → "1.3k"  (not "1k" — avoids rounding down)
//	43000 → "43k"   (≥10k: no decimal, rounded to nearest k)
//	1500000 → "1.5m"
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
		if n < 10_000 {
			return fmt.Sprintf("%.1fk", f)
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

	// 5 circles: each represents 20%. Adding 10 before dividing rounds to
	// the nearest circle rather than truncating:
	//
	//	 0–10%  → ○○○○○  (0 filled)
	//	11–30%  → ●○○○○  (1 filled)
	//	31–50%  → ●●○○○  (2 filled)
	//	51–70%  → ●●●○○  (3 filled)
	//	71–90%  → ●●●●○  (4 filled)
	//	91–100% → ●●●●●  (5 filled)
	filled := (pct + 10) / 20
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
