package builtin

import (
	"testing"
	"unicode"
)

// TestToolPromptsAreEnglish 守住「工具描述与参数说明属于 prompt」这条约定：
// 它们会随 tools 声明一起发给模型，因此必须是英文。
func TestToolPromptsAreEnglish(t *testing.T) {
	list, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatalf("构造工具集失败：%v", err)
	}
	for _, tool := range list {
		if bad := firstNonASCII(tool.Description()); bad != "" {
			t.Fatalf("%s 的描述包含非 ASCII 内容：%q", tool.Name(), bad)
		}
		if bad := firstNonASCII(string(tool.Parameters())); bad != "" {
			t.Fatalf("%s 的参数 schema 包含非 ASCII 内容：%q", tool.Name(), bad)
		}
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
