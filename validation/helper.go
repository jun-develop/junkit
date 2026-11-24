package validation

import (
	"strings"

	"github.com/jun-develop/junkit/stringx"
)

// normalizeFieldPath converts a validator namespace into a snake_case field path, dropping the
// root struct name and any embedded "Request" segment.
//
// Usage:
//
//	normalizeFieldPath("CreateUserRequest.Password") // "password"
func normalizeFieldPath(namespace string) string {
	root := strings.Split(namespace, ".")[0]
	rawPath := strings.TrimPrefix(namespace, root+".")

	parts := strings.Split(rawPath, ".")
	filteredParts := make([]string, 0, len(parts))

	for _, part := range parts {
		if part == "Request" {
			continue
		}

		if idx := strings.Index(part, "["); idx != -1 {
			base := stringx.ToSnake(part[:idx])
			index := part[idx:]
			filteredParts = append(filteredParts, base+index)
		} else {
			filteredParts = append(filteredParts, stringx.ToSnake(part))
		}
	}

	return strings.Join(filteredParts, ".")
}
