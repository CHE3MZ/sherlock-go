package engine

import "strings"

// interpolateString recursively replaces "{}" with username across strings,
// maps and lists (used for URLs and nested request payloads).
func interpolateString(input any, username string) any {
	switch t := input.(type) {
	case string:
		return strings.ReplaceAll(t, "{}", username)
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, v := range t {
			out[k] = interpolateString(v, username)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, v := range t {
			out[i] = interpolateString(v, username)
		}
		return out
	default:
		return input
	}
}
