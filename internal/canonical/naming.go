package canonical

import (
	"strings"
	"unicode"
)

// ToolName returns an MCP-safe tool name using allowed characters only.
// It uses a double-underscore separator to avoid dots (disallowed by some clients).
func ToolName(apiName, operationID string) string {
	return sanitizeName(apiName) + "__" + sanitizeName(operationID)
}

func sanitizeName(input string) string {
	if input == "" {
		return "op"
	}
	var b strings.Builder
	for _, r := range input {
		switch {
		case r == '_' || r == '-':
			b.WriteRune(r)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := b.String()
	if out == "" {
		return "op"
	}
	return out
}
