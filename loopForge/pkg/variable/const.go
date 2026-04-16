package variable

import "strings"

// ConstPrefix marks variables that are read-only for Agent tool calls (var_set).
const ConstPrefix = "const_"

// IsConst reports whether key is agent-read-only (const_ prefix).
func IsConst(key string) bool {
	return strings.HasPrefix(key, ConstPrefix)
}
