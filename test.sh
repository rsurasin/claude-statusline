#!/usr/bin/env bash
# test.sh — Pipe mock Claude Code JSON into the statusline binary.
# We also inject a fake usage cache to test 5h/7d circle rendering.
#
# NOTE on token display: The statusline derives the displayed token count from
# used_percentage × context_window_size, NOT from total_input_tokens +
# total_output_tokens. Claude Code's used_percentage includes cache tokens and
# other overhead not reflected in raw I/O counts, so the percentage is treated
# as the authoritative source. This keeps the displayed "Nk/Nk N%" consistent.
# When writing tests, set used_percentage to the value you want displayed —
# the raw token fields don't affect the rendered output.
set -euo pipefail

BIN="./claude-statusline"

if [ ! -x "$BIN" ]; then
    echo "Binary not found. Run 'make build' first."
    exit 1
fi

CACHE_DIR="/tmp/claude-statusline"
CACHE="$CACHE_DIR/usage.json"

# Save original cache so we can restore it after tests
CACHE_BACKUP=""
if [ -f "$CACHE" ]; then
    CACHE_BACKUP=$(mktemp)
    cp "$CACHE" "$CACHE_BACKUP"
fi

mkdir -p "$CACHE_DIR"

divider() {
    printf '\n%s\n' "────────────────────────────────────────────────────────"
}

# assert_contains OUTPUT EXPECTED_SUBSTRING TEST_NAME
assert_contains() {
    local output="$1" expected="$2" label="$3"
    # Strip ANSI codes for comparison
    local plain
    plain=$(echo "$output" | sed 's/\x1b\[[0-9;]*m//g')
    if [[ "$plain" != *"$expected"* ]]; then
        echo "FAIL: $label"
        echo "  Expected to contain: $expected"
        echo "  Got: $plain"
        FAILURES=$((FAILURES + 1))
    fi
}

FAILURES=0

# ---------------------------------------------------------------------------
# Helper: write a fake usage cache so 5h/7d circles render in tests
# ---------------------------------------------------------------------------
write_usage_cache() {
    local five_pct=$1
    local seven_pct=$2
    local extra_enabled=${3:-false}
    local extra_used=${4:-0}
    local extra_limit=${5:-0}

    # Reset timestamps: 5h resets 3h29m from now, 7d resets 2d22h from now
    local five_reset seven_reset
    five_reset=$(date -u -d "+3 hours 29 minutes" "+%Y-%m-%dT%H:%M:%S+00:00" 2>/dev/null \
              || date -u -v+3H -v+29M "+%Y-%m-%dT%H:%M:%S+00:00" 2>/dev/null \
              || echo "2026-03-07T03:29:00+00:00")
    seven_reset=$(date -u -d "+2 days 22 hours" "+%Y-%m-%dT%H:%M:%S+00:00" 2>/dev/null \
               || date -u -v+2d -v+22H "+%Y-%m-%dT%H:%M:%S+00:00" 2>/dev/null \
               || echo "2026-03-09T22:00:00+00:00")

    local extra_block="null"
    if [ "$extra_enabled" = "true" ]; then
        extra_block="{\"is_enabled\":true,\"monthly_limit\":$extra_limit,\"used_credits\":$extra_used}"
    fi

    cat > "$CACHE" <<ENDJSON
{
  "fetched_at": $(date +%s),
  "data": {
    "five_hour": { "utilization": $five_pct, "resets_at": "$five_reset" },
    "seven_day": { "utilization": $seven_pct, "resets_at": "$seven_reset" },
    "extra_usage": $extra_block
  }
}
ENDJSON
}

# ---------------------------------------------------------------------------
# Cleanup on exit: remove fake cache
# ---------------------------------------------------------------------------
cleanup() {
    if [ -n "$CACHE_BACKUP" ]; then
        mkdir -p "$CACHE_DIR"
        mv "$CACHE_BACKUP" "$CACHE"
    else
        rm -f "$CACHE"
    fi
}
trap cleanup EXIT

# ---------------------------------------------------------------------------
# Test 1: Full payload
# ---------------------------------------------------------------------------
write_usage_cache 10 43
divider
echo "Test 1: Full payload"
echo "Expected L1: Opus 4.6 | @code-reviewer | [think/effort from your settings] | repo:branch +N/-N"
echo "Expected L2: 85k/200k 42% | 5h ●○○○○ 10% (3h 29m) | 7d ●●○○○ 43% (2d 22h)"
echo ""
OUTPUT=$(echo '
{
  "model": { "display_name": "Opus 4.6" },
  "agent": { "name": "code-reviewer" },
  "workspace": { "current_dir": "'"$(pwd)"'" },
  "context_window": {
    "total_input_tokens": 42500,
    "total_output_tokens": 8700,
    "context_window_size": 200000,
    "used_percentage": 42.5
  }
}
' | $BIN)
echo "$OUTPUT"
assert_contains "$OUTPUT" "Opus 4.6" "Test 1: model name"
assert_contains "$OUTPUT" "@code-reviewer" "Test 1: agent"
assert_contains "$OUTPUT" "85k/200k" "Test 1: context tokens"
assert_contains "$OUTPUT" "42%" "Test 1: context percentage"
assert_contains "$OUTPUT" "5h" "Test 1: 5h bucket label"
assert_contains "$OUTPUT" "10%" "Test 1: 5h percentage"
assert_contains "$OUTPUT" "7d" "Test 1: 7d bucket label"
assert_contains "$OUTPUT" "43%" "Test 1: 7d percentage"
echo ""

# ---------------------------------------------------------------------------
# Test 2: Minimal — no agent, no session name
# ---------------------------------------------------------------------------
write_usage_cache 5 12
divider
echo "Test 2: Minimal (no agent, no session name)"
echo "Expected L1: Sonnet 4.6 | [think/effort if configured] | repo:branch"
echo "Expected L2: 4.6k/200k 2% | 5h ○○○○○ 5% (...) | 7d ●○○○○ 12% (...)"
echo ""
OUTPUT=$(echo '
{
  "model": { "display_name": "Sonnet 4.6" },
  "workspace": { "current_dir": "'"$(pwd)"'" },
  "context_window": {
    "total_input_tokens": 3200,
    "total_output_tokens": 1400,
    "context_window_size": 200000,
    "used_percentage": 2.3
  }
}
' | $BIN)
echo "$OUTPUT"
assert_contains "$OUTPUT" "Sonnet 4.6" "Test 2: model name"
assert_contains "$OUTPUT" "4.6k/200k" "Test 2: context tokens"
assert_contains "$OUTPUT" "2%" "Test 2: context percentage"
assert_contains "$OUTPUT" "5h" "Test 2: 5h bucket label"
assert_contains "$OUTPUT" "5%" "Test 2: 5h percentage"
assert_contains "$OUTPUT" "7d" "Test 2: 7d bucket label"
assert_contains "$OUTPUT" "12%" "Test 2: 7d percentage"
echo ""

# ---------------------------------------------------------------------------
# Test 3: High context + high 5h/7d — circles should be mostly filled
# ---------------------------------------------------------------------------
write_usage_cache 85 72
divider
echo "Test 3: High usage — context 85% (red), 5h 85% ●●●●○ (red), 7d 72% ●●●●○ (yellow)"
echo ""
OUTPUT=$(echo '
{
  "model": { "display_name": "Opus 4.6" },
  "workspace": { "current_dir": "'"$(pwd)"'" },
  "context_window": {
    "total_input_tokens": 170000,
    "total_output_tokens": 25000,
    "context_window_size": 200000,
    "used_percentage": 85.0
  }
}
' | $BIN)
echo "$OUTPUT"
assert_contains "$OUTPUT" "Opus 4.6" "Test 3: model name"
assert_contains "$OUTPUT" "170k/200k" "Test 3: context tokens"
assert_contains "$OUTPUT" "85%" "Test 3: context percentage"
assert_contains "$OUTPUT" "5h" "Test 3: 5h bucket label"
assert_contains "$OUTPUT" "7d" "Test 3: 7d bucket label"
assert_contains "$OUTPUT" "72%" "Test 3: 7d percentage"
echo ""

# ---------------------------------------------------------------------------
# Test 4: 100% 5h, 100% 7d — all circles filled
# ---------------------------------------------------------------------------
write_usage_cache 100 100
divider
echo "Test 4: Maxed out — 5h ●●●●● 100% (red), 7d ●●●●● 100% (red)"
echo ""
OUTPUT=$(echo '
{
  "model": { "display_name": "Sonnet 4.6" },
  "workspace": { "current_dir": "/tmp" },
  "context_window": {
    "total_input_tokens": 124000,
    "total_output_tokens": 15000,
    "context_window_size": 200000,
    "used_percentage": 62.0
  }
}
' | $BIN)
echo "$OUTPUT"
assert_contains "$OUTPUT" "Sonnet 4.6" "Test 4: model name"
assert_contains "$OUTPUT" "124k/200k" "Test 4: context tokens"
assert_contains "$OUTPUT" "62%" "Test 4: context percentage"
assert_contains "$OUTPUT" "5h" "Test 4: 5h bucket label"
assert_contains "$OUTPUT" "100%" "Test 4: 5h percentage"
assert_contains "$OUTPUT" "7d" "Test 4: 7d bucket label"
echo ""

# ---------------------------------------------------------------------------
# Test 5: Mid-range — 50% 5h, 30% 7d
# ---------------------------------------------------------------------------
write_usage_cache 50 30
divider
echo "Test 5: Mid-range — 5h ●●●○○ 50% (yellow), 7d ●●○○○ 30% (green)"
echo ""
OUTPUT=$(echo '
{
  "model": { "display_name": "Opus 4.6" },
  "workspace": { "current_dir": "'"$(pwd)"'" },
  "context_window": {
    "total_input_tokens": 60000,
    "total_output_tokens": 10000,
    "context_window_size": 200000,
    "used_percentage": 30.0
  }
}
' | $BIN)
echo "$OUTPUT"
assert_contains "$OUTPUT" "Opus 4.6" "Test 5: model name"
assert_contains "$OUTPUT" "60k/200k" "Test 5: context tokens"
assert_contains "$OUTPUT" "30%" "Test 5: context percentage"
assert_contains "$OUTPUT" "5h" "Test 5: 5h bucket label"
assert_contains "$OUTPUT" "50%" "Test 5: 5h percentage"
assert_contains "$OUTPUT" "7d" "Test 5: 7d bucket label"
echo ""

# ---------------------------------------------------------------------------
# Test 6: Non-git directory
# Note: raw tokens (500+100=600) don't match displayed "3k/200k" because
# the display is derived from used_percentage (1.5% × 200k = 3000). See the
# header comment for details.
# ---------------------------------------------------------------------------
write_usage_cache 20 10
divider
echo "Test 6: Non-git directory (/tmp) — no git segment on line 1"
echo ""
OUTPUT=$(echo '
{
  "model": { "display_name": "Haiku 4.5" },
  "workspace": { "current_dir": "/tmp" },
  "context_window": {
    "total_input_tokens": 500,
    "total_output_tokens": 100,
    "context_window_size": 200000,
    "used_percentage": 1.5
  }
}
' | $BIN)
echo "$OUTPUT"
assert_contains "$OUTPUT" "Haiku 4.5" "Test 6: model name"
assert_contains "$OUTPUT" "3k/200k" "Test 6: context tokens"
assert_contains "$OUTPUT" "1%" "Test 6: context percentage"
assert_contains "$OUTPUT" "5h" "Test 6: 5h bucket label"
assert_contains "$OUTPUT" "20%" "Test 6: 5h percentage"
assert_contains "$OUTPUT" "7d" "Test 6: 7d bucket label"
echo ""

# ---------------------------------------------------------------------------
# Test 7: Extra credit active — $12 of $50 used
# ---------------------------------------------------------------------------
write_usage_cache 90 65 true 1200 5000
divider
echo "Test 7: Extra credit active — extra \$12/\$50 (green), 5h 90% (red), 7d 65% (yellow)"
echo ""
OUTPUT=$(echo '
{
  "model": { "display_name": "Opus 4.6" },
  "workspace": { "current_dir": "'"$(pwd)"'" },
  "context_window": {
    "total_input_tokens": 180000,
    "total_output_tokens": 30000,
    "context_window_size": 200000,
    "used_percentage": 90.0
  }
}
' | $BIN)
echo "$OUTPUT"
assert_contains "$OUTPUT" "Opus 4.6" "Test 7: model name"
assert_contains "$OUTPUT" "180k/200k" "Test 7: context tokens"
assert_contains "$OUTPUT" "90%" "Test 7: context percentage"
assert_contains "$OUTPUT" "5h" "Test 7: 5h bucket label"
assert_contains "$OUTPUT" "7d" "Test 7: 7d bucket label"
assert_contains "$OUTPUT" "65%" "Test 7: 7d percentage"
assert_contains "$OUTPUT" "extra" "Test 7: extra label"
assert_contains "$OUTPUT" '$12/$50' "Test 7: extra usage"
echo ""

# ---------------------------------------------------------------------------
# Test 8: Extra credit high — $45 of $50 used (should be red)
# ---------------------------------------------------------------------------
write_usage_cache 40 25 true 4500 5000
divider
echo "Test 8: Extra credit nearly exhausted — extra \$45/\$50 (red)"
echo ""
OUTPUT=$(echo '
{
  "model": { "display_name": "Sonnet 4.6" },
  "workspace": { "current_dir": "'"$(pwd)"'" },
  "context_window": {
    "total_input_tokens": 50000,
    "total_output_tokens": 8000,
    "context_window_size": 200000,
    "used_percentage": 25.0
  }
}
' | $BIN)
echo "$OUTPUT"
assert_contains "$OUTPUT" "Sonnet 4.6" "Test 8: model name"
assert_contains "$OUTPUT" "50k/200k" "Test 8: context tokens"
assert_contains "$OUTPUT" "25%" "Test 8: context percentage"
assert_contains "$OUTPUT" "5h" "Test 8: 5h bucket label"
assert_contains "$OUTPUT" "40%" "Test 8: 5h percentage"
assert_contains "$OUTPUT" "7d" "Test 8: 7d bucket label"
assert_contains "$OUTPUT" "extra" "Test 8: extra label"
assert_contains "$OUTPUT" '$45/$50' "Test 8: extra usage"
echo ""

# ---------------------------------------------------------------------------
# Test 9: 1M context window
# ---------------------------------------------------------------------------
write_usage_cache 15 8
divider
echo "Test 9: Large context — 1M window"
echo ""
OUTPUT=$(echo '
{
  "model": { "display_name": "Opus 4.6 [1m]" },
  "workspace": { "current_dir": "'"$(pwd)"'" },
  "context_window": {
    "total_input_tokens": 523000,
    "total_output_tokens": 87000,
    "context_window_size": 1000000,
    "used_percentage": 52.3
  }
}
' | $BIN)
echo "$OUTPUT"
assert_contains "$OUTPUT" "Opus 4.6" "Test 9: model name"
assert_contains "$OUTPUT" "523k/1m" "Test 9: context tokens"
assert_contains "$OUTPUT" "52%" "Test 9: context percentage"
assert_contains "$OUTPUT" "5h" "Test 9: 5h bucket label"
assert_contains "$OUTPUT" "15%" "Test 9: 5h percentage"
assert_contains "$OUTPUT" "7d" "Test 9: 7d bucket label"
assert_contains "$OUTPUT" "8%" "Test 9: 7d percentage"
echo ""

# ---------------------------------------------------------------------------
# Test 10: Starship passthrough (gated — only runs if starship is installed)
# ---------------------------------------------------------------------------
if command -v starship &>/dev/null; then
    write_usage_cache 10 20
    divider
    echo "Test 10: Starship passthrough — git segment uses Starship formatting"
    echo ""
    OUTPUT=$(echo '
{
  "model": { "display_name": "Opus 4.6" },
  "workspace": { "current_dir": "'"$(pwd)"'" },
  "context_window": {
    "total_input_tokens": 42500,
    "total_output_tokens": 8700,
    "context_window_size": 200000,
    "used_percentage": 42.5
  }
}
' | $BIN)
    echo "$OUTPUT"
    # Starship replaces repo:branch with its own directory + branch format.
    # The native "repo:branch" pattern should NOT appear.
    PLAIN=$(echo "$OUTPUT" | sed 's/\x1b\[[0-9;]*m//g')
    LINE1=$(echo "$PLAIN" | head -1)
    REPO_NAME=$(basename "$(git rev-parse --show-toplevel 2>/dev/null)")
    if [[ "$LINE1" == *"${REPO_NAME}:"* ]]; then
        echo "FAIL: Test 10: native repo:branch format found — Starship should replace it"
        FAILURES=$((FAILURES + 1))
    fi
    # Diff stats should still be present if there are changes.
    echo ""
fi

divider
if [ "$FAILURES" -gt 0 ]; then
    echo "FAILED: $FAILURES assertion(s) failed"
    exit 1
fi
echo "All tests complete."
