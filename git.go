package main

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

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
