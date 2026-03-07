# claude-statusline

A Go binary that renders a custom two-line ANSI statusline for [Claude Code](https://docs.anthropic.com/en/docs/claude-code).

```
Opus 4.6 | @code-reviewer | Refactor auth module | think:on | my-repo:main +12/-3
43k/200k 42% | 5h ●●○○○ 10% (3h 29m) | 7d ●●●○○ 43% (2d 22h) | extra $5/$50
```

## Features

- **Model & agent** — displays the active model name and subagent (e.g. `@code-reviewer`)
- **Session name** — shows the session title from stdin JSON, `sessions-index.json`, or the session JSONL transcript
- **Thinking mode** — `think:on` / `think:off` parsed from the session JSONL
- **Git info** — `repo:branch +added/-removed` with staged and unstaged diff stats
- **Context window** — `43k/200k 42%` with color-coded utilization (green/yellow/red)
- **Usage API** — 5-hour and 7-day rate-limit utilization as filled-circle bars with time-until-reset
- **Extra credit** — `$used/$limit` when extended usage is active
- **Caching** — 60s file-based cache for the usage API, 10s cache for thinking mode lookups
- **ANSI colors** — uses 16-color ANSI codes that follow your terminal's color scheme

## Prerequisites

- Go 1.25+
- Claude Code with statusline hook support

## Build & Install

```sh
make install
```

This builds the binary and copies it to `~/.claude/claude-statusline`.

## Configuration

Add the following to `~/.claude/settings.json`:

```json
{
  "statusLine": {
    "type": "command",
    "command": "~/.claude/claude-statusline"
  }
}
```

## How It Works

Claude Code invokes the statusline command with a JSON payload on stdin containing session metadata, model info, workspace paths, and context window stats. The binary parses this input, gathers git status from the working directory, fetches rate-limit data from the Anthropic usage API (via OAuth token), and outputs two lines of ANSI-colored text.

## Testing

```sh
make test
```

Runs `test.sh`, which pipes mock JSON payloads into the binary with various usage scenarios (low/mid/high utilization, extra credit, non-git directories, large context windows).
