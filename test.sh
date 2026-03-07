#!/usr/bin/env bash
# test.sh — Pipe mock Claude Code JSON into the statusline binary.
# We also inject a fake usage cache to test 5h/7d circle rendering.
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
echo "Expected L1: Opus 4.6 | @code-reviewer | Refactor auth module | [think/effort from your settings] | repo:branch +N/-N"
echo "Expected L2: 85k/200k 42% | 5h ●○○○○ 10% (3h 29m) | 7d ●●○○○ 43% (2d 22h)"
echo ""
echo '
{
  "session_name": "Refactor auth module",
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
' | $BIN
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
echo '
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
' | $BIN
echo ""

# ---------------------------------------------------------------------------
# Test 3: High context + high 5h/7d — circles should be mostly filled
# ---------------------------------------------------------------------------
write_usage_cache 85 72
divider
echo "Test 3: High usage — context 85% (red), 5h 85% ●●●●○ (red), 7d 72% ●●●●○ (yellow)"
echo ""
echo '
{
  "model": { "display_name": "Opus 4.6" },
  "session_name": "Debug payment flow",
  "workspace": { "current_dir": "'"$(pwd)"'" },
  "context_window": {
    "total_input_tokens": 170000,
    "total_output_tokens": 25000,
    "context_window_size": 200000,
    "used_percentage": 85.0
  }
}
' | $BIN
echo ""

# ---------------------------------------------------------------------------
# Test 4: 100% 5h, 100% 7d — all circles filled
# ---------------------------------------------------------------------------
write_usage_cache 100 100
divider
echo "Test 4: Maxed out — 5h ●●●●● 100% (red), 7d ●●●●● 100% (red)"
echo ""
echo '
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
' | $BIN
echo ""

# ---------------------------------------------------------------------------
# Test 5: Mid-range — 50% 5h, 30% 7d
# ---------------------------------------------------------------------------
write_usage_cache 50 30
divider
echo "Test 5: Mid-range — 5h ●●●○○ 50% (yellow), 7d ●●○○○ 30% (green)"
echo ""
echo '
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
' | $BIN
echo ""

# ---------------------------------------------------------------------------
# Test 6: Non-git directory
# ---------------------------------------------------------------------------
write_usage_cache 20 10
divider
echo "Test 6: Non-git directory (/tmp) — no git segment on line 1"
echo ""
echo '
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
' | $BIN
echo ""

# ---------------------------------------------------------------------------
# Test 7: Extra credit active — $12 of $50 used
# ---------------------------------------------------------------------------
write_usage_cache 90 65 true 1200 5000
divider
echo "Test 7: Extra credit active — extra \$12/\$50 (green), 5h 90% (red), 7d 65% (yellow)"
echo ""
echo '
{
  "model": { "display_name": "Opus 4.6" },
  "session_name": "Long refactor session",
  "workspace": { "current_dir": "'"$(pwd)"'" },
  "context_window": {
    "total_input_tokens": 180000,
    "total_output_tokens": 30000,
    "context_window_size": 200000,
    "used_percentage": 90.0
  }
}
' | $BIN
echo ""

# ---------------------------------------------------------------------------
# Test 8: Extra credit high — $45 of $50 used (should be red)
# ---------------------------------------------------------------------------
write_usage_cache 40 25 true 4500 5000
divider
echo "Test 8: Extra credit nearly exhausted — extra \$45/\$50 (red)"
echo ""
echo '
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
' | $BIN
echo ""

# ---------------------------------------------------------------------------
# Test 9: 1M context window
# ---------------------------------------------------------------------------
write_usage_cache 15 8
divider
echo "Test 9: Large context — 1M window"
echo ""
echo '
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
' | $BIN
echo ""

divider
echo "All tests complete."
