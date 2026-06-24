package tokenopt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

func looksLikeJSON(input string) bool {
	input = strings.TrimSpace(input)
	return strings.HasPrefix(input, "{") || strings.HasPrefix(input, "[")
}

func CompactJSON(input string, opts Options) Result {
	opts = normalizeOptions(opts)
	if len([]rune(input)) < opts.MinChars {
		return unchanged(input, "below threshold")
	}

	var value any
	decoder := json.NewDecoder(strings.NewReader(input))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return CompactText(input, opts)
	}

	compacted := compactJSONValue(value, 0)
	encoded, err := marshalCompactJSON(compacted)
	if err != nil {
		return CompactText(input, opts)
	}

	prefix := fmt.Sprintf("[VivuRouter token-optimized JSON: %d chars]\n", len([]rune(input)))
	out := trimToBudget(prefix+string(encoded), opts.MaxChars)
	if len([]rune(out)) >= len([]rune(input)) {
		return unchanged(input, "json compaction not smaller")
	}
	return result(input, out, "json compacted")
}

func compactJSONValue(value any, depth int) any {
	if depth >= 5 {
		return "…"
	}
	switch v := value.(type) {
	case map[string]any:
		out := map[string]any{}
		count := 0
		for key, child := range v {
			if count >= 80 {
				out["…"] = fmt.Sprintf("%d more keys", len(v)-count)
				break
			}
			out[key] = compactJSONValue(child, depth+1)
			count++
		}
		return out
	case []any:
		limit := minInt(len(v), 20)
		out := make([]any, 0, limit+1)
		for i := 0; i < limit; i++ {
			out = append(out, compactJSONValue(v[i], depth+1))
		}
		if len(v) > limit {
			out = append(out, fmt.Sprintf("… %d more items", len(v)-limit))
		}
		return out
	case string:
		runes := []rune(v)
		if len(runes) > 180 {
			return string(runes[:180]) + fmt.Sprintf("… [%d chars]", len(runes))
		}
		return v
	default:
		return v
	}
}

func marshalCompactJSON(value any) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSpace(buf.Bytes()), nil
}
