package skills

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	"github.com/tasohower/rei/core"
	"github.com/tasohower/rei/tools"
)

func writeSkill(t *testing.T, dir, name, body string) {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.MkdirAll(full, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(full, "SKILL.md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadDirsParsesFrontMatter(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "pdf", "---\nname: pdf\ndescription: 处理 PDF 文档\n---\n正文步骤")
	writeSkill(t, dir, "xlsx", "没有 front matter 的技能\n第二行")

	reg, err := LoadDirs(dir)
	if err != nil {
		t.Fatal(err)
	}
	list := reg.List()
	if len(list) != 2 {
		t.Fatalf("技能数 = %d", len(list))
	}
	pdf, ok := reg.Get("pdf")
	if !ok || pdf.Description != "处理 PDF 文档" || !strings.Contains(pdf.Body, "正文步骤") {
		t.Fatalf("pdf 解析 = %+v", pdf)
	}
	xlsx, _ := reg.Get("xlsx")
	if xlsx.Description != "没有 front matter 的技能" {
		t.Fatalf("缺省描述 = %q", xlsx.Description)
	}
}

func TestPromptFragmentIsEnglishAndMinimal(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(Skill{Name: "pdf", Description: "Work with PDF files", Body: "长正文"})
	frag := reg.PromptFragment()
	if !strings.Contains(frag, "load_skill") || !strings.Contains(frag, "pdf: Work with PDF files") {
		t.Fatalf("片段 = %q", frag)
	}
	if strings.Contains(frag, "长正文") {
		t.Fatal("系统提示只应包含名字与描述（渐进披露）")
	}
	for _, r := range frag {
		if r > unicode.MaxASCII {
			t.Fatalf("片段必须英文：%q", string(r))
		}
	}
}

func TestLoadSkillTool(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(Skill{Name: "pdf", Description: "d", Body: "完整指令"})
	eng := tools.NewEngine()
	if err := reg.RegisterTool(eng); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	res := eng.Execute(ctx, []core.ToolCall{{Name: "load_skill", Arguments: `{"name":"pdf"}`}})[0]
	if res.Result.IsError || res.Result.Content != "完整指令" {
		t.Fatalf("加载结果 = %+v", res.Result)
	}
	missing := eng.Execute(ctx, []core.ToolCall{{Name: "load_skill", Arguments: `{"name":"nope"}`}})[0]
	if !missing.Result.IsError || !strings.Contains(missing.Result.Content, "available") {
		t.Fatalf("未知技能应报错并给出清单：%+v", missing.Result)
	}
}

func TestLoadDirsIgnoresMissingDirectory(t *testing.T) {
	if _, err := LoadDirs("/不存在的目录"); err != nil {
		t.Fatalf("缺失目录不应报错：%v", err)
	}
}
