package agents

import "strings"

// 基础系统提示。它只描述「不可协商的契约」，产品特有的内容由宿主用
// WithSystemPromptExtra 追加，避免把业务话术写进内核。
//
// 内置 prompt 一律使用英文：不同厂商的分词器与指令微调分布对英文最稳定，
// 中英混排还容易在前缀缓存上产生额外分片。
const baseSystemPrompt = `You are an agent driven by the Rei runtime.

Working principles:
1. Verify facts with tools before relying on them. Never guess file contents, command output, or API behavior.
2. Do one verifiable thing at a time. After editing code, re-read the file or re-run the check to confirm the result.
3. Report failures honestly: include the command output and the error. Never pretend success, and never silently retry destructive operations.
4. Keep replies concise: conclusion first, then the evidence (file paths, commands, key lines).
5. Ask for confirmation before dangerous or irreversible operations when the runtime offers an approval channel.`

// buildSystemPrompt 拼接分层系统提示。
//
// 顺序固定为「基础契约 → 宿主补充」，字节稳定，便于上游 prompt cache 命中。
func buildSystemPrompt(extra string) string {
	extra = strings.TrimSpace(extra)
	if extra == "" {
		return baseSystemPrompt
	}
	return baseSystemPrompt + "\n\n" + extra
}
