package main

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

// stripANSI removes ANSI escape sequences from a string.
func stripANSI(s string) string {
	return regexp.MustCompile(`\033\[[0-9;]*m`).ReplaceAllString(s, "")
}

func TestHumanTokens(t *testing.T) {
	tests := []struct {
		input int
		want  string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1k"},
		{1500, "1.5k"},
		{3200, "3.2k"},
		{10000, "10k"},
		{42500, "42k"},
		{43000, "43k"},
		{200000, "200k"},
		{523000, "523k"},
		{1000000, "1m"},
		{1500000, "1.5m"},
		{2000000, "2m"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := humanTokens(tt.input)
			if got != tt.want {
				t.Errorf("humanTokens(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestContextSegment(t *testing.T) {
	tests := []struct {
		name      string
		ctx       ContextWindow
		wantPct   string
		wantColor string // the raw ANSI code expected
	}{
		{
			name:      "42% green",
			ctx:       ContextWindow{ContextWindowSize: 200000, UsedPercentage: 42},
			wantPct:   "42%",
			wantColor: green,
		},
		{
			name:      "55% yellow",
			ctx:       ContextWindow{ContextWindowSize: 200000, UsedPercentage: 55},
			wantPct:   "55%",
			wantColor: yellow,
		},
		{
			name:      "85% red",
			ctx:       ContextWindow{ContextWindowSize: 200000, UsedPercentage: 85},
			wantPct:   "85%",
			wantColor: red,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := contextSegment(&tt.ctx)
			plain := stripANSI(got)

			if !strings.Contains(plain, tt.wantPct) {
				t.Errorf("contextSegment() plain = %q, want to contain %q", plain, tt.wantPct)
			}

			if !strings.Contains(got, tt.wantColor) {
				t.Errorf("contextSegment() missing expected color code %q", tt.wantColor)
			}

			// Verify "Nk/Nk" format: the used/total tokens should appear.
			used := int(float64(tt.ctx.ContextWindowSize) * tt.ctx.UsedPercentage / 100)
			wantUsed := humanTokens(used)
			wantTotal := humanTokens(tt.ctx.ContextWindowSize)
			wantRatio := wantUsed + "/" + wantTotal
			if !strings.Contains(plain, wantRatio) {
				t.Errorf("contextSegment() plain = %q, want to contain %q", plain, wantRatio)
			}
		})
	}
}

func TestUsageBucketSegment(t *testing.T) {
	tests := []struct {
		name       string
		pct        float64
		wantFilled int
		wantEmpty  int
	}{
		{"0%", 0, 0, 5},
		{"5%", 5, 0, 5},
		{"15%", 15, 1, 4},
		{"50%", 50, 3, 2},
		{"85%", 85, 4, 1},
		{"100%", 100, 5, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bucket := &UsageBucket{Utilization: tt.pct}
			got := usageBucketSegment("5h", bucket, false, cyan)
			plain := stripANSI(got)

			filled := strings.Count(plain, "●")
			empty := strings.Count(plain, "○")
			if filled != tt.wantFilled {
				t.Errorf("filled circles = %d, want %d (pct=%v)", filled, tt.wantFilled, tt.pct)
			}
			if empty != tt.wantEmpty {
				t.Errorf("empty circles = %d, want %d (pct=%v)", empty, tt.wantEmpty, tt.pct)
			}
		})
	}

	// Verify reset time parenthetical appears when ResetsAt is set.
	t.Run("reset time shown", func(t *testing.T) {
		future := time.Now().Add(3*time.Hour + 29*time.Minute).Format(time.RFC3339)
		bucket := &UsageBucket{Utilization: 50, ResetsAt: &future}
		got := usageBucketSegment("5h", bucket, false, cyan)
		plain := stripANSI(got)
		if !strings.Contains(plain, "(") || !strings.Contains(plain, ")") {
			t.Errorf("expected parenthetical reset time, got %q", plain)
		}
	})
}

func TestTimeUntilReset(t *testing.T) {
	t.Run("hourly format", func(t *testing.T) {
		future := time.Now().Add(3*time.Hour + 29*time.Minute + 30*time.Second).Format(time.RFC3339)
		got := timeUntilReset(future, false)
		if got != "3h 29m" {
			t.Errorf("timeUntilReset() = %q, want %q", got, "3h 29m")
		}
	})

	t.Run("weekly format", func(t *testing.T) {
		future := time.Now().Add(2*24*time.Hour + 22*time.Hour + 30*time.Minute).Format(time.RFC3339)
		got := timeUntilReset(future, true)
		if got != "2d 22h" {
			t.Errorf("timeUntilReset() = %q, want %q", got, "2d 22h")
		}
	})

	t.Run("past timestamp", func(t *testing.T) {
		past := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
		got := timeUntilReset(past, false)
		if got != "now" {
			t.Errorf("timeUntilReset() = %q, want %q", got, "now")
		}
	})

	t.Run("invalid string", func(t *testing.T) {
		got := timeUntilReset("not-a-date", false)
		if got != "" {
			t.Errorf("timeUntilReset() = %q, want %q", got, "")
		}
	})

	t.Run("empty string", func(t *testing.T) {
		got := timeUntilReset("", false)
		if got != "" {
			t.Errorf("timeUntilReset() = %q, want %q", got, "")
		}
	})
}
