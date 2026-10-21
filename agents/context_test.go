package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadProjectInstructionsPrefersAgentsMD(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("用中文回答"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("不该被选中"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := LoadProjectInstructions(dir)
	if got != "用中文回答" {
		t.Fatalf("项目指令 = %q", got)
	}
}

func TestWithProjectInstructionsAppendsToSystemPrompt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("Always run go vet."), 0o600); err != nil {
		t.Fatal(err)
	}
	loop := newLoop(t, &scriptedProvider{steps: []scriptStep{textStep("ok")}},
		WithProjectInstructions(dir), WithSystemPromptExtra("产品补充"))

	if !strings.Contains(loop.system, "Always run go vet.") {
		t.Fatalf("系统提示未包含项目指令：%q", loop.system)
	}
	if !strings.Contains(loop.system, "产品补充") {
		t.Fatalf("系统提示未包含宿主补充：%q", loop.system)
	}
	if !strings.Contains(loop.system, "Project instructions (AGENTS.md):") {
		t.Fatalf("缺少指令段落标题：%q", loop.system)
	}
}

func TestLoadProjectInstructionsMissingIsEmpty(t *testing.T) {
	if got := LoadProjectInstructions(t.TempDir()); got != "" {
		t.Fatalf("无文件时应为空，得到 %q", got)
	}
}
