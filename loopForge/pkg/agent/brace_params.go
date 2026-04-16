package agent

import (
	"fmt"
	"github.com/bytedance/sonic"
	"strings"

	"loopforge/pkg/variable"
)

// ReplaceDoubleBraceParams replaces segments of the form "{{name}}" in s using params.
// Matching is case-insensitive: keys and placeholders are compared after
// strings.ToLower. If two param keys lower to the same string, the last one in
// the input map wins. If no key matches a placeholder, the original "{{...}}"
// segment is left unchanged. Unclosed "{{" without a following "}}" leaves the
// rest of the string unchanged from that point.
func ReplaceDoubleBraceParams(s string, params map[string]string) string {
	if len(params) == 0 {
		return s
	}
	folded := make(map[string]string, len(params))
	for k, v := range params {
		folded[strings.ToLower(k)] = v
	}

	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		j := strings.Index(s[i:], "{{")
		if j < 0 {
			b.WriteString(s[i:])
			break
		}
		j += i
		b.WriteString(s[i:j])
		rest := s[j+2:]
		k := strings.Index(rest, "}}")
		if k < 0 {
			b.WriteString(s[j:])
			break
		}
		name := strings.TrimSpace(rest[:k])
		segEnd := j + 2 + k + 2
		if name == "" {
			b.WriteString(s[j:segEnd])
			i = segEnd
			continue
		}
		if val, ok := folded[strings.ToLower(name)]; ok {
			b.WriteString(val)
		} else {
			b.WriteString(s[j:segEnd])
		}
		i = segEnd
	}
	return b.String()
}

// stringParamsFromVarStore builds a map of variable keys to string values for
// placeholder substitution. Nil values become "<unset>"; other values use JSON
// encoding when possible, consistent with variable.PromptBlock rendering.
func stringParamsFromVarStore(vs *variable.VarStore) map[string]string {
	if vs == nil {
		return nil
	}
	all := vs.All()
	if len(all) == 0 {
		return nil
	}
	out := make(map[string]string, len(all))
	for k, e := range all {
		if k == "" || e == nil {
			continue
		}
		out[k] = varValueString(e.Value)
	}
	return out
}

func varValueString(v any) string {
	if v == nil {
		return "<unset>"
	}
	if s, ok := v.(string); ok {
		return s
	}
	raw, err := sonic.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(raw)
}
