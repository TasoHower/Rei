// Package builtin 提供每个宿主都需要的工区工具：读、写、编辑、glob、grep
// 以及带护栏的 bash。实现刻意保持小而零依赖；需要更强能力的宿主自己注册工具。
//
// 工具描述与参数说明属于「发给模型的 prompt」，一律使用英文；注释与错误说明使用中文。
package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tasohower/rei/core"
	"github.com/tasohower/rei/tools"
)

// NewWorkspace 返回以 root 为根的默认工具集。
//
// 所有路径参数都被限制在 root 内；越界访问直接拒绝，而不是静默裁剪。
func NewWorkspace(root string) ([]tools.Tool, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("builtin: 解析根目录失败：%w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("builtin: 工区 %s：%w", abs, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("builtin: 工区 %s 不是目录", abs)
	}
	w := &workspace{root: abs}
	return []tools.Tool{
		&readFile{w},
		&writeFile{w},
		&editFile{w},
		&globTool{w},
		&grepTool{w},
		&bashTool{w},
	}, nil
}

// RegisterWorkspace 把默认工具集注册到引擎上。
func RegisterWorkspace(eng *tools.Engine, root string) error {
	list, err := NewWorkspace(root)
	if err != nil {
		return err
	}
	for _, t := range list {
		if err := eng.Register(t); err != nil {
			return err
		}
	}
	return nil
}

type workspace struct{ root string }

// resolve 把工具传入的路径转成 root 内的绝对路径。
func (w *workspace) resolve(p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", errors.New("path is required")
	}
	abs := p
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(w.root, p)
	}
	abs = filepath.Clean(abs)
	rel, err := filepath.Rel(w.root, abs)
	if err != nil {
		return "", fmt.Errorf("path %q: %w", p, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path %q escapes the workspace", p)
	}
	return abs, nil
}

func (w *workspace) rel(abs string) string {
	rel, err := filepath.Rel(w.root, abs)
	if err != nil {
		return abs
	}
	return rel
}

func schema(props map[string]any, required ...string) json.RawMessage {
	doc := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		doc["required"] = required
	}
	raw, _ := json.Marshal(doc)
	return raw
}

func decode(call core.ToolCall, v any) error {
	if strings.TrimSpace(call.Arguments) == "" {
		return errors.New("arguments must be a JSON object")
	}
	if err := json.Unmarshal([]byte(call.Arguments), v); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	return nil
}

// ------------------------------------------------------------------ read_file

type readFile struct{ w *workspace }

func (t *readFile) Name() string        { return "read_file" }
func (t *readFile) Description() string { return "Read a UTF-8 text file from the workspace." }
func (t *readFile) CanParallel() bool   { return true }
func (t *readFile) Parameters() json.RawMessage {
	return schema(map[string]any{
		"path": map[string]any{"type": "string", "description": "Path relative to the workspace root."},
	}, "path")
}

func (t *readFile) Call(_ context.Context, call core.ToolCall) tools.Result {
	var args struct {
		Path string `json:"path"`
	}
	if err := decode(call, &args); err != nil {
		return tools.ErrorResult("%v", err)
	}
	abs, err := t.w.resolve(args.Path)
	if err != nil {
		return tools.ErrorResult("%v", err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return tools.ErrorResult("read %s: %v", args.Path, err)
	}
	probe := data
	if len(probe) > 512 {
		probe = probe[:512]
	}
	if strings.ContainsRune(string(probe), 0) {
		return tools.ErrorResult("%s looks binary; refusing to inline it", args.Path)
	}
	return tools.Result{Content: string(data), Metadata: map[string]any{"bytes": len(data)}}
}

// ----------------------------------------------------------------- write_file

type writeFile struct{ w *workspace }

func (t *writeFile) Name() string        { return "write_file" }
func (t *writeFile) Description() string { return "Create or overwrite a workspace file atomically." }
func (t *writeFile) CanParallel() bool   { return false }
func (t *writeFile) Parameters() json.RawMessage {
	return schema(map[string]any{
		"path":    map[string]any{"type": "string", "description": "Path relative to the workspace root."},
		"content": map[string]any{"type": "string", "description": "Full file content."},
	}, "path", "content")
}

func (t *writeFile) Call(_ context.Context, call core.ToolCall) tools.Result {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := decode(call, &args); err != nil {
		return tools.ErrorResult("%v", err)
	}
	abs, err := t.w.resolve(args.Path)
	if err != nil {
		return tools.ErrorResult("%v", err)
	}
	if err := writeAtomic(abs, []byte(args.Content)); err != nil {
		return tools.ErrorResult("write %s: %v", args.Path, err)
	}
	return tools.Result{Content: fmt.Sprintf("wrote %d bytes to %s", len(args.Content), t.w.rel(abs))}
}

// writeAtomic 先写同目录临时文件再 rename，崩溃时不会留下半截文件。
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".rei-tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// ------------------------------------------------------------------ edit_file

type editFile struct{ w *workspace }

func (t *editFile) Name() string { return "edit_file" }
func (t *editFile) Description() string {
	return "Replace every occurrence of old_text with new_text in a workspace file."
}
func (t *editFile) CanParallel() bool { return false }
func (t *editFile) Parameters() json.RawMessage {
	return schema(map[string]any{
		"path":     map[string]any{"type": "string"},
		"old_text": map[string]any{"type": "string", "description": "Exact text to find."},
		"new_text": map[string]any{"type": "string", "description": "Replacement text."},
	}, "path", "old_text", "new_text")
}

func (t *editFile) Call(_ context.Context, call core.ToolCall) tools.Result {
	var args struct {
		Path    string `json:"path"`
		OldText string `json:"old_text"`
		NewText string `json:"new_text"`
	}
	if err := decode(call, &args); err != nil {
		return tools.ErrorResult("%v", err)
	}
	if args.OldText == "" {
		return tools.ErrorResult("old_text must not be empty")
	}
	abs, err := t.w.resolve(args.Path)
	if err != nil {
		return tools.ErrorResult("%v", err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return tools.ErrorResult("read %s: %v", args.Path, err)
	}
	text := string(data)
	count := strings.Count(text, args.OldText)
	if count == 0 {
		return tools.ErrorResult("old_text not found in %s", args.Path)
	}
	updated := strings.ReplaceAll(text, args.OldText, args.NewText)
	if err := writeAtomic(abs, []byte(updated)); err != nil {
		return tools.ErrorResult("write %s: %v", args.Path, err)
	}
	return tools.Result{Content: fmt.Sprintf("replaced %d occurrence(s) in %s", count, t.w.rel(abs))}
}

// ---------------------------------------------------------------------- glob

type globTool struct{ w *workspace }

func (t *globTool) Name() string { return "glob" }
func (t *globTool) Description() string {
	return "List workspace files matching a shell glob pattern; ** is supported."
}
func (t *globTool) CanParallel() bool { return true }
func (t *globTool) Parameters() json.RawMessage {
	return schema(map[string]any{
		"pattern": map[string]any{"type": "string", "description": `e.g. "**/*.go" or "docs/*.md".`},
	}, "pattern")
}

func (t *globTool) Call(_ context.Context, call core.ToolCall) tools.Result {
	var args struct {
		Pattern string `json:"pattern"`
	}
	if err := decode(call, &args); err != nil {
		return tools.ErrorResult("%v", err)
	}
	if strings.TrimSpace(args.Pattern) == "" {
		return tools.ErrorResult("pattern is required")
	}
	pattern := args.Pattern
	if !filepath.IsAbs(pattern) {
		pattern = filepath.Join(t.w.root, pattern)
	}
	matches, err := doublestarGlob(pattern)
	if err != nil {
		return tools.ErrorResult("glob %s: %v", args.Pattern, err)
	}
	var out []string
	for _, m := range matches {
		out = append(out, t.w.rel(m))
		if len(out) >= 500 {
			break
		}
	}
	if len(out) == 0 {
		return tools.Result{Content: "no files matched " + args.Pattern}
	}
	return tools.Result{Content: strings.Join(out, "\n"), Metadata: map[string]any{"count": len(out)}}
}

// ---------------------------------------------------------------------- grep

type grepTool struct{ w *workspace }

func (t *grepTool) Name() string { return "grep" }
func (t *grepTool) Description() string {
	return "Search workspace files with a Go regular expression."
}
func (t *grepTool) CanParallel() bool { return true }
func (t *grepTool) Parameters() json.RawMessage {
	return schema(map[string]any{
		"pattern": map[string]any{"type": "string"},
		"path":    map[string]any{"type": "string", "description": "Optional subdirectory to search."},
	}, "pattern")
}

func (t *grepTool) Call(_ context.Context, call core.ToolCall) tools.Result {
	var args struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
	}
	if err := decode(call, &args); err != nil {
		return tools.ErrorResult("%v", err)
	}
	if args.Pattern == "" {
		return tools.ErrorResult("pattern is required")
	}
	root := t.w.root
	if args.Path != "" {
		abs, err := t.w.resolve(args.Path)
		if err != nil {
			return tools.ErrorResult("%v", err)
		}
		root = abs
	}
	out, err := grepFiles(root, args.Pattern)
	if err != nil {
		return tools.ErrorResult("%v", err)
	}
	if len(out) == 0 {
		return tools.Result{Content: "no matches"}
	}
	return tools.Result{Content: strings.Join(out, "\n"), Metadata: map[string]any{"matches": len(out)}}
}
