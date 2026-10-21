// Package skills 实现 Agent Skills 的注册表与加载工具。
//
// 一个技能就是 <dir>/<name>/SKILL.md：可选的 front matter 提供 name/description，
// 正文是该技能的完整指令。系统提示里只放「名字 + 一行描述」（渐进披露），
// 模型需要时再通过 load_skill 工具取全文。
package skills

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/tasohower/rei/core"
	"github.com/tasohower/rei/tools"
)

// 约束。
const (
	maxNameLen = 64
	maxBodyLen = 64 << 10
)

// Skill 是一个技能。
type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Body        string `json:"-"`
	Path        string `json:"path,omitempty"`
}

// Registry 是技能注册表。
type Registry struct {
	mu     sync.RWMutex
	skills map[string]Skill
}

// NewRegistry 构造空注册表。
func NewRegistry() *Registry { return &Registry{skills: map[string]Skill{}} }

// LoadDirs 扫描多个目录（后者覆盖同名技能）。
func LoadDirs(dirs ...string) (*Registry, error) {
	r := NewRegistry()
	for _, dir := range dirs {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			path := filepath.Join(dir, entry.Name(), "SKILL.md")
			raw, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			skill, err := Parse(entry.Name(), string(raw))
			if err != nil {
				continue
			}
			skill.Path = path
			_ = r.Register(skill)
		}
	}
	return r, nil
}

// Parse 解析 SKILL.md 内容。front matter 可选：
//
//	---
//	name: pdf
//	description: 处理 PDF
//	---
//	正文……
func Parse(dirName, raw string) (Skill, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Skill{}, errors.New("skills: 技能内容为空")
	}
	skill := Skill{Name: dirName}
	body := raw

	if strings.HasPrefix(raw, "---") {
		rest := strings.TrimPrefix(raw, "---")
		if idx := strings.Index(rest, "\n---"); idx >= 0 {
			header := rest[:idx]
			body = strings.TrimSpace(rest[idx+len("\n---"):])
			for _, line := range strings.Split(header, "\n") {
				key, value, ok := strings.Cut(line, ":")
				if !ok {
					continue
				}
				switch strings.ToLower(strings.TrimSpace(key)) {
				case "name":
					if v := strings.TrimSpace(value); v != "" {
						skill.Name = v
					}
				case "description":
					skill.Description = strings.TrimSpace(value)
				}
			}
		}
	}

	skill.Name = strings.TrimSpace(skill.Name)
	if skill.Name == "" || len(skill.Name) > maxNameLen {
		return Skill{}, fmt.Errorf("skills: 非法技能名 %q", skill.Name)
	}
	if skill.Description == "" {
		// 没有描述时取正文第一行，保证提示里总有可用信息。
		first, _, _ := strings.Cut(body, "\n")
		skill.Description = truncate(strings.TrimSpace(first), 120)
	}
	if len(body) > maxBodyLen {
		body = body[:maxBodyLen]
	}
	skill.Body = body
	return skill, nil
}

// Register 注册技能（同名覆盖）。
func (r *Registry) Register(s Skill) error {
	if strings.TrimSpace(s.Name) == "" {
		return errors.New("skills: 技能缺少名称")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.skills[s.Name] = s
	return nil
}

// Get 查找技能。
func (r *Registry) Get(name string) (Skill, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.skills[name]
	return s, ok
}

// List 按名字排序返回全部技能。
func (r *Registry) List() []Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Skill, 0, len(r.skills))
	for _, s := range r.skills {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// PromptFragment 渲染成英文系统提示片段（只含名字与描述）。
func (r *Registry) PromptFragment() string {
	list := r.List()
	if len(list) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Available skills (call load_skill to read the full instructions):\n")
	for _, s := range list {
		fmt.Fprintf(&b, "- %s: %s\n", s.Name, s.Description)
	}
	return b.String()
}

// Tool 返回 load_skill 工具。
func (r *Registry) Tool() tools.Tool { return &loadTool{reg: r} }

// RegisterTool 把 load_skill 注册到引擎上。
func (r *Registry) RegisterTool(eng *tools.Engine) error {
	return eng.Register(r.Tool())
}

type loadTool struct{ reg *Registry }

func (t *loadTool) Name() string        { return "load_skill" }
func (t *loadTool) Description() string { return "Load the full instructions of a skill by name." }
func (t *loadTool) CanParallel() bool   { return true }
func (t *loadTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`)
}

func (t *loadTool) Call(_ context.Context, call core.ToolCall) tools.Result {
	var args struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
		return tools.ErrorResult("invalid arguments: %v", err)
	}
	skill, ok := t.reg.Get(args.Name)
	if !ok {
		var names []string
		for _, s := range t.reg.List() {
			names = append(names, s.Name)
		}
		return tools.ErrorResult("unknown skill %q; available: %s", args.Name, strings.Join(names, ", "))
	}
	return tools.Result{Content: skill.Body, Metadata: map[string]any{"skill": skill.Name}}
}

func truncate(s string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit]) + "…"
}
