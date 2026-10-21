package compress

import (
	"strings"
	"testing"
	"unicode"

	"github.com/tasohower/rei/core"
)

// TestSummaryPromptIsEnglish 守住「内置 prompt 一律英文」这条约定。
func TestSummaryPromptIsEnglish(t *testing.T) {
	for _, r := range summarySystemPrompt {
		if r > unicode.MaxASCII {
			t.Fatalf("压缩提示包含非 ASCII 内容：%q", string(r))
		}
	}
	if !strings.Contains(summarySystemPrompt, "Output only the summary body") {
		t.Fatal("压缩提示缺少「只输出摘要正文」约束")
	}
}

// TestTranscriptUsesEnglishLabels 保证送进压缩模型的转写文本由我们自己生成的
// 标签也是英文（用户正文保持原样，可以是任何语言）。
func TestTranscriptUsesEnglishLabels(t *testing.T) {
	msgs := []core.Message{
		core.NewUserTextMessage("hello"),
		core.NewAssistantMessage([]core.Content{{Type: core.ContentTypeText, Content: "hi"}}, "", nil),
	}
	text := transcript(msgs, 10_000)
	for _, want := range []string{"User: hello", "Assistant: hi"} {
		if !strings.Contains(text, want) {
			t.Fatalf("转写缺少 %q：%s", want, text)
		}
	}
	for _, r := range text {
		if r > unicode.MaxASCII {
			t.Fatalf("转写文本含非 ASCII 内容：%q", string(r))
		}
	}

	omitted := transcript(manyMessages(20), 10)
	if !strings.Contains(omitted, "(earlier content omitted)") {
		t.Fatalf("超长转写应带省略标记：%s", omitted)
	}
}
