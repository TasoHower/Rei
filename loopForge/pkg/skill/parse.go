package skill

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

var namePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

const maxDescriptionLen = 1024

// ParseSKILLFile reads a SKILL.md file and returns a SkillSpec.
func ParseSKILLFile(path string) (*SkillSpec, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(raw) {
		return nil, fmt.Errorf("skill file %s: not valid UTF-8", path)
	}
	sum := sha256.Sum256(raw)
	hashHex := hex.EncodeToString(sum[:])

	fm, body, err := splitFrontMatter(string(raw))
	if err != nil {
		return nil, fmt.Errorf("skill file %s: %w", path, err)
	}
	if err := validateFrontmatter(fm); err != nil {
		return nil, fmt.Errorf("skill file %s: %w", path, err)
	}

	spec := &SkillSpec{
		Name:         fm.Name,
		Version:      fm.Version,
		Description:  fm.Description,
		AllowedTools: append([]string(nil), fm.AllowedTools...),
		Model:        fm.Model,
		License:      fm.License,
		Body:         strings.TrimSpace(body),
		SourcePath:   path,
		ContentHash:  hashHex,
		Extra:        cloneExtra(fm.Extra),
	}
	return spec, nil
}

func splitFrontMatter(content string) (SkillFrontmatter, string, error) {
	content = strings.TrimPrefix(content, "\uFEFF")
	s := strings.TrimLeft(content, " \t")
	if !strings.HasPrefix(s, "---") {
		return SkillFrontmatter{}, "", fmt.Errorf("missing YAML front matter delimiter")
	}
	firstNL := strings.IndexByte(s, '\n')
	if firstNL < 0 {
		return SkillFrontmatter{}, "", fmt.Errorf("unterminated YAML front matter")
	}
	afterOpen := s[firstNL+1:]

	closeSep := "\n---\n"
	closeIdx := strings.Index(afterOpen, closeSep)
	bodyOffset := closeIdx + len(closeSep)
	if closeIdx < 0 {
		closeSep = "\r\n---\r\n"
		closeIdx = strings.Index(afterOpen, closeSep)
		bodyOffset = closeIdx + len(closeSep)
	}
	if closeIdx < 0 {
		closeSep = "\n---\r\n"
		closeIdx = strings.Index(afterOpen, closeSep)
		bodyOffset = closeIdx + len(closeSep)
	}
	if closeIdx < 0 {
		return SkillFrontmatter{}, "", fmt.Errorf("unterminated YAML front matter")
	}

	yamlBlock := afterOpen[:closeIdx]
	body := afterOpen[bodyOffset:]

	fm, err := parseFrontmatterYAML([]byte(yamlBlock))
	if err != nil {
		return SkillFrontmatter{}, "", err
	}
	return fm, body, nil
}

func parseFrontmatterYAML(raw []byte) (SkillFrontmatter, error) {
	var root map[string]interface{}
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return SkillFrontmatter{}, err
	}
	if root == nil {
		root = map[string]interface{}{}
	}

	fm := SkillFrontmatter{
		Extra: map[string]any{},
	}

	known := map[string]struct{}{
		"name": {}, "description": {}, "allowed-tools": {}, "model": {}, "license": {},
		"version": {}, "author": {}, "tags": {},
	}

	for k, v := range root {
		if _, ok := known[k]; !ok {
			fm.Extra[k] = v
		}
	}

	if v, ok := root["name"].(string); ok {
		fm.Name = v
	}
	if v, ok := root["description"].(string); ok {
		fm.Description = v
	}
	if v, ok := root["model"].(string); ok {
		fm.Model = v
	}
	if v, ok := root["license"].(string); ok {
		fm.License = v
	}
	if v, ok := root["version"].(string); ok {
		fm.Version = v
	}
	if v, ok := root["author"].(string); ok {
		fm.Author = v
	}

	if v, ok := root["allowed-tools"]; ok {
		tools, err := normalizeAllowedTools(v)
		if err != nil {
			return SkillFrontmatter{}, err
		}
		fm.AllowedTools = tools
	}

	if v, ok := root["tags"]; ok {
		tags, err := normalizeStringSlice(v)
		if err != nil {
			return SkillFrontmatter{}, err
		}
		fm.Tags = tags
	}

	return fm, nil
}

func normalizeAllowedTools(v interface{}) ([]string, error) {
	switch t := v.(type) {
	case nil:
		return nil, nil
	case string:
		return splitCommaList(t), nil
	case []interface{}:
		var out []string
		for _, item := range t {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("allowed-tools: expected string entries")
			}
			s = strings.TrimSpace(s)
			if s == "" {
				return nil, fmt.Errorf("allowed-tools: empty entry")
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("allowed-tools: unsupported type")
	}
}

func normalizeStringSlice(v interface{}) ([]string, error) {
	switch t := v.(type) {
	case nil:
		return nil, nil
	case []interface{}:
		var out []string
		for _, item := range t {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("tags: expected string entries")
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("tags: unsupported type")
	}
}

func splitCommaList(s string) []string {
	parts := strings.Split(s, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

func validateFrontmatter(fm SkillFrontmatter) error {
	if fm.Name == "" {
		return fmt.Errorf("front matter: name is required")
	}
	if !namePattern.MatchString(fm.Name) {
		return fmt.Errorf("front matter: invalid name %q", fm.Name)
	}
	if strings.TrimSpace(fm.Description) == "" {
		return fmt.Errorf("front matter: description is required")
	}
	if len(fm.Description) > maxDescriptionLen {
		return fmt.Errorf("front matter: description exceeds %d characters", maxDescriptionLen)
	}
	for _, t := range fm.AllowedTools {
		if strings.TrimSpace(t) == "" {
			return fmt.Errorf("front matter: allowed-tools contains empty entry")
		}
	}
	return nil
}

func cloneExtra(m map[string]any) map[string]any {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
