package tokenopt

import "strings"

type Options struct {
	// MaxChars is the target upper bound for compacted text. A sensible default is used when zero.
	MaxChars int
	// MinChars is the threshold below which content is left unchanged. A sensible default is used when zero.
	MinChars int
	// PreserveErrors keeps lines that look operationally important.
	PreserveErrors bool
}

type Result struct {
	OriginalChars           int
	CompactChars            int
	EstimatedOriginalTokens int
	EstimatedCompactTokens  int
	EstimatedSavedTokens    int
	Applied                 bool
	Reason                  string
	Text                    string
}

const (
	defaultMaxChars = 12000
	defaultMinChars = 6000
)

func normalizeOptions(opts Options) Options {
	if opts.MaxChars <= 0 {
		opts.MaxChars = defaultMaxChars
	}
	if opts.MinChars <= 0 {
		opts.MinChars = defaultMinChars
	}
	if opts.MaxChars < 1000 {
		opts.MaxChars = 1000
	}
	return opts
}

func unchanged(input, reason string) Result {
	chars := len([]rune(input))
	tokens := EstimateTokens(input)
	return Result{
		OriginalChars:           chars,
		CompactChars:            chars,
		EstimatedOriginalTokens: tokens,
		EstimatedCompactTokens:  tokens,
		EstimatedSavedTokens:    0,
		Applied:                 false,
		Reason:                  reason,
		Text:                    input,
	}
}

func result(input, output, reason string) Result {
	originalChars := len([]rune(input))
	compactChars := len([]rune(output))
	originalTokens := EstimateTokens(input)
	compactTokens := EstimateTokens(output)
	return Result{
		OriginalChars:           originalChars,
		CompactChars:            compactChars,
		EstimatedOriginalTokens: originalTokens,
		EstimatedCompactTokens:  compactTokens,
		EstimatedSavedTokens:    maxInt(originalTokens-compactTokens, 0),
		Applied:                 output != input && compactChars < originalChars,
		Reason:                  reason,
		Text:                    output,
	}
}

func ResultFromCompactText(input, output, reason string) Result {
	return result(input, output, reason)
}

func CompactToolResult(input string, opts Options) Result {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return unchanged(input, "empty")
	}
	if looksLikeJSON(trimmed) {
		return CompactJSON(input, opts)
	}
	if looksLikeLog(input) {
		return CompactLog(input, opts)
	}
	return CompactText(input, opts)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
