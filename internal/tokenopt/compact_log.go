package tokenopt

import (
	"fmt"
	"strings"
)

func looksLikeLog(input string) bool {
	lower := strings.ToLower(input)
	markers := []string{"\nerror", "\nwarn", "\nfailed", "\ntraceback", "\npanic", "level=", "timestamp", " stack ", " exception"}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return len(splitLines(input)) > 80
}

func CompactLog(input string, opts Options) Result {
	opts = normalizeOptions(opts)
	if len([]rune(input)) < opts.MinChars {
		return unchanged(input, "below threshold")
	}

	lines := splitLines(input)
	counts := map[string]int{}
	order := []string{}
	for _, line := range lines {
		key := normalizeLogLine(line)
		if key == "" {
			continue
		}
		if counts[key] == 0 {
			order = append(order, key)
		}
		counts[key]++
	}

	important := importantLines(lines)
	var b strings.Builder
	fmt.Fprintf(&b, "[VivuRouter token-optimized log: %d lines, %d unique patterns]\n", len(lines), len(order))
	if len(important) > 0 {
		writeSection(&b, "important", important)
	}
	b.WriteString("\n--- repeated patterns ---\n")
	limit := minInt(len(order), 120)
	for _, key := range order[:limit] {
		count := counts[key]
		if count > 1 {
			fmt.Fprintf(&b, "x%d %s\n", count, key)
		} else {
			b.WriteString(key)
			b.WriteByte('\n')
		}
	}
	if len(order) > limit {
		fmt.Fprintf(&b, "... %d more unique patterns omitted\n", len(order)-limit)
	}

	out := trimToBudget(b.String(), opts.MaxChars)
	if len([]rune(out)) >= len([]rune(input)) {
		return CompactText(input, opts)
	}
	return result(input, out, "log compacted")
}

func normalizeLogLine(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	fields := strings.Fields(line)
	if len(fields) > 24 {
		fields = fields[:24]
	}
	return strings.Join(fields, " ")
}
