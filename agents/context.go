package agents

import (
	"os"
	"path/filepath"
	"strings"
)

// 项目指令文件与长度上限。
const (
	maxInstructionsBytes = 32 << 10
	instructionsHeader   = "Project instructions (AGENTS.md):"
)

// instructionFiles 按优先级排列：先 AGENTS.md，再兼容 CLAUDE.md。
var instructionFiles = []string{"AGENTS.md", "CLAUDE.md"}

// LoadProjectInstructions 读取工作区根目录的项目指令。
//
// 只读根目录、不递归：子目录的 AGENTS.md 往往描述「子模块内部约定」，
// 一次性注入全部会互相冲突。需要时由宿主自己拼接后传入。
func LoadProjectInstructions(root string) string {
	if strings.TrimSpace(root) == "" {
		return ""
	}
	for _, name := range instructionFiles {
		raw, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			continue
		}
		text := strings.TrimSpace(string(raw))
		if text == "" {
			continue
		}
		if len(text) > maxInstructionsBytes {
			text = text[:maxInstructionsBytes] + "\n… (truncated)"
		}
		return text
	}
	return ""
}

// WithProjectInstructions 把工作区的 AGENTS.md / CLAUDE.md 追加到系统提示。
//
// 注入点在「基础契约」之后、宿主补充之前，字节稳定；文件不存在时等价于不配置。
func WithProjectInstructions(root string) Option {
	return func(l *AgentLoop) {
		docs := LoadProjectInstructions(root)
		if docs == "" {
			return
		}
		extra := instructionsHeader + "\n" + docs
		if strings.TrimSpace(l.instructions) == "" {
			l.instructions = extra
			return
		}
		l.instructions = l.instructions + "\n\n" + extra
	}
}
