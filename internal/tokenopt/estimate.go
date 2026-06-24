package tokenopt

const defaultCharsPerToken = 4

// EstimateTokens returns a conservative rough token estimate for English/code-like text.
func EstimateTokens(s string) int {
	if s == "" {
		return 0
	}
	n := len([]rune(s))
	return (n + defaultCharsPerToken - 1) / defaultCharsPerToken
}
