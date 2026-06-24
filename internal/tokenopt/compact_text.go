package tokenopt

import (
	"fmt"
	"strings"
)

var importantLineMarkers = []string{
	"error", "failed", "failure", "panic", "traceback", "exception", "fatal",
	"warning", "warn", "denied", "unauthorized", "forbidden", "timeout", "timed out",
	"cannot", "undefined", "not found", "no such file", "killed", "oom",
}

func CompactText(input string, opts Options) Result {
	opts = normalizeOptions(opts)
	if len([]rune(input)) < opts.MinChars {
		return unchanged(input, "below threshold")
	}

	lines := splitLines(input)
	if len(lines) <= 8 {
		return compactByHeadTail(input, opts.MaxChars, "text truncated")
	}

	important := importantLines(lines)
	budget := opts.MaxChars
	headCount := minInt(30, len(lines))
	tailCount := minInt(30, maxInt(len(lines)-headCount, 0))

	var b strings.Builder
	fmt.Fprintf(&b, "[VivuRouter token-optimized text: %d lines, %d chars]\n", len(lines), len([]rune(input)))
	writeSection(&b, "head", lines[:headCount])
	if opts.PreserveErrors && len(important) > 0 {
		writeSection(&b, "important", important)
	}
	if tailCount > 0 {
		writeSection(&b, "tail", lines[len(lines)-tailCount:])
	}

	out := trimToBudget(b.String(), budget)
	if len([]rune(out)) >= len([]rune(input)) {
		return unchanged(input, "compaction not smaller")
	}
	return result(input, out, "text compacted")
}

func compactByHeadTail(input string, maxChars int, reason string) Result {
	runes := []rune(input)
	if len(runes) <= maxChars {
		return unchanged(input, "below max chars")
	}
	head := maxChars / 2
	tail := maxChars - head
	out := string(runes[:head]) + "\n\n[... token-optimized: middle omitted ...]\n\n" + string(runes[len(runes)-tail:])
	return result(input, out, reason)
}

func splitLines(input string) []string {
	input = strings.ReplaceAll(input, "\r\n", "\n")
	input = strings.ReplaceAll(input, "\r", "\n")
	return strings.Split(input, "\n")
}

func importantLines(lines []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, line := range lines {
		lower := strings.ToLower(line)
		matched := false
		for _, marker := range importantLineMarkers {
			if strings.Contains(lower, marker) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		key := strings.TrimSpace(line)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, line)
		if len(out) >= 80 {
			break
		}
	}
	return out
}

func writeSection(b *strings.Builder, name string, lines []string) {
	if len(lines) == 0 {
		return
	}
	fmt.Fprintf(b, "\n--- %s ---\n", name)
	for _, line := range lines {
		b.WriteString(line)
		b.WriteByte('\n')
	}
}

func trimToBudget(s string, maxChars int) string {
	runes := []rune(s)
	if len(runes) <= maxChars {
		return s
	}
	marker := "\n[... token-optimized: output truncated to budget ...]\n"
	markerRunes := []rune(marker)
	keep := maxChars - len(markerRunes)
	if keep <= 0 {
		return string(runes[:maxChars])
	}
	return string(runes[:keep]) + marker
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
