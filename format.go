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
