package skill

import (
	"strings"
)

// ComposeSystemSegments joins base system text, an optional MCP prompt fragment, and
// skill bodies in the order: base, MCP, then skills (each skill as a markdown section).
func ComposeSystemSegments(base string, mcpPromptFragment string, skills []SkillSpec) string {
	var parts []string
	if s := strings.TrimSpace(base); s != "" {
		parts = append(parts, s)
	}
	if s := strings.TrimSpace(mcpPromptFragment); s != "" {
		parts = append(parts, s)
	}
	for _, sk := range skills {
		body := strings.TrimSpace(sk.Body)
		blk := "## Skill: " + sk.Name + "\n\n" + body
		parts = append(parts, blk)
	}
	return strings.Join(parts, "\n\n")
}
