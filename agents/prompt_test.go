package agents

import (
	"strings"
	"testing"
	"unicode"
)

// TestBasePromptIsEnglish 守住「内置 prompt 一律英文」这条约定：
// 非 ASCII 字符进入系统提示，通常意味着有人把中文注释或文案写进了 prompt。
func TestBasePromptIsEnglish(t *testing.T) {
	if bad := firstNonASCII(baseSystemPrompt); bad != "" {
		t.Fatalf("内置系统提示包含非 ASCII 内容：%q", bad)
	}
}

func TestBuildSystemPromptAppendsHostExtra(t *testing.T) {
	if got := buildSystemPrompt("   "); got != baseSystemPrompt {
		t.Fatal("空白补充应当等于基础提示")
	}
	got := buildSystemPrompt("Project: Rei")
	if !strings.HasPrefix(got, baseSystemPrompt) {
		t.Fatal("补充内容必须追加在基础提示之后（前缀字节稳定）")
	}
	if !strings.HasSuffix(got, "Project: Rei") {
		t.Fatalf("补充内容未追加：%q", got)
	}
}

func firstNonASCII(s string) string {
	for _, r := range s {
		if r > unicode.MaxASCII {
			return string(r)
		}
	}
	return ""
}
