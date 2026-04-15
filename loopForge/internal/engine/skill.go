package engine

import "context"

// SkillLoader loads trusted skill documents for prompt injection.
type SkillLoader interface {
	Load(ctx context.Context, skillIDs []string) ([]SkillDocument, error)
}

// SkillDocument is parsed skill content for injection (path omitted in hot path).
type SkillDocument struct {
	ID      string
	Version string
	Body    string
}
