package skill

import (
	"fmt"
	"strings"
)

// ValidateSkillBindings ensures every name exists in the registry when names are non-empty.
func ValidateSkillBindings(names []string, reg *SkillRegistry) error {
	if len(names) == 0 {
		return nil
	}
	if reg == nil {
		return fmt.Errorf("skill registry is required when skill names are set")
	}
	for _, raw := range names {
		n := strings.TrimSpace(raw)
		if n == "" {
			return fmt.Errorf("skill name must not be empty")
		}
		if _, err := reg.Get(n); err != nil {
			return fmt.Errorf("skill %q: %w", n, err)
		}
	}
	return nil
}
