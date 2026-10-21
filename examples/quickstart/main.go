// quickstart 演示如何把 Rei 嵌进一个命令行宿主：一个会话、一套工区工具、
// 一个压缩器，消费事件流并把正文打印到终端。
//
// 用法：
//
//	export REI_API_KEY=sk-...
//	export REI_BASE_URL=https://api.deepseek.com/v1   # 可选
//	export REI_MODEL=deepseek-chat                    # 可选
//	go run ./examples/quickstart "统计本仓库有多少个 Go 文件"
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/tasohower/rei/agents"
	"github.com/tasohower/rei/compress"
	"github.com/tasohower/rei/core"
	"github.com/tasohower/rei/events"
	"github.com/tasohower/rei/provider"
	"github.com/tasohower/rei/session"
	"github.com/tasohower/rei/tools"
	"github.com/tasohower/rei/tools/builtin"
)

func main() {
	prompt := strings.Join(os.Args[1:], " ")
	if strings.TrimSpace(prompt) == "" {
		prompt = "用一句话说明这个仓库是什么，并列出顶层目录。"
	}

	baseURL := envOr("REI_BASE_URL", "https://api.deepseek.com/v1")
	model := envOr("REI_MODEL", "deepseek-chat")
	apiKey := os.Getenv("REI_API_KEY")

	prov, err := provider.NewOpenAI(provider.OpenAIConfig{BaseURL: baseURL, APIKey: apiKey})
	must(err)

	engine := tools.NewEngine(tools.WithToolTimeout(2 * time.Minute))
	must(builtin.RegisterWorkspace(engine, "."))

	loop, err := agents.NewAgentLoop(prov,
		agents.WithModel(model),
		agents.WithToolEngine(engine),
		agents.WithMaxIterations(16),
		agents.WithCompressor(compress.RecentWindow{
			Trigger:    compress.Trigger{ContextWindow: 64_000},
			KeepRecent: 8,
		}),
	)
	must(err)

	store, err := session.NewFileStore("./.rei-sessions")
	must(err)
	s, err := session.New("quickstart", loop, session.WithStore(store))
	must(err)
	defer func() { _ = s.Shutdown(context.Background(), nil) }()

	must(s.Ask(context.Background(), core.NewUserTextMessage(prompt)))

	handler := func(_ context.Context, e events.Event) {
		switch ev := e.(type) {
		case *events.ContentChunk:
			fmt.Print(ev.Text)
		case *events.ReasoningChunk:
			// 思考增量默认不打印，需要时在这里输出。
		case *events.ToolRunStart:
			fmt.Printf("\n[工具] %s %s\n", ev.Name, ev.Arguments)
		case *events.ToolRunEnd:
			status := "成功"
			if ev.IsError {
				status = "失败"
			}
			fmt.Printf("[工具结果] %s：%s（%dms）\n", status, firstLine(ev.Content), ev.DurationMS)
		case *events.AgentEnd:
			fmt.Printf("\n\n[结束] 原因=%s 输入=%d 输出=%d 成本=%dµUSD 轮次=%d\n",
				ev.FinishReason, ev.Usage.Input, ev.Usage.Output, ev.Usage.Cost.Total(), ev.DurationMS)
		}
	}

	if err := s.Run(context.Background(), handler); err != nil {
		fmt.Fprintf(os.Stderr, "\n运行失败：%v\n", err)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return s[:idx]
	}
	if len([]rune(s)) > 60 {
		return string([]rune(s)[:60]) + "…"
	}
	return s
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
