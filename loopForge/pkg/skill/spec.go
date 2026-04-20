package skill

import "path/filepath"

// SkillFrontmatter holds YAML front matter fields from SKILL.md.
type SkillFrontmatter struct {
	Name         string
	Description  string
	AllowedTools []string
	Model        string
	License      string
	Version      string
	Author       string
	Tags         []string
	Extra        map[string]any
}

// SkillSpec is a registered skill entry including injectable body text.
type SkillSpec struct {
	Name         string
	Version      string
	Description  string
	AllowedTools []string
	Model        string
	License      string
	Body         string
	SourcePath   string
	ContentHash  string
	Extra        map[string]any
}

// SkillMeta is a lightweight discovery view without Body.
type SkillMeta struct {
	Name        string
	Version     string
	Description string
	SourcePath  string
}

// BundleRoot returns the directory containing SKILL.md for this spec.
func (s *SkillSpec) BundleRoot() string {
	if s == nil || s.SourcePath == "" {
		return ""
	}
	return filepath.Dir(s.SourcePath)
}
