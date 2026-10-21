package compress

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/tasohower/rei/core"
	"github.com/tasohower/rei/provider"
)

// fakeStream 逐条吐出预设增量，用于在不联网的情况下测压缩器。
type fakeStream struct {
	chunks []provider.Chunk
	i      int
}

func (f *fakeStream) Recv() (provider.Chunk, error) {
	if f.i >= len(f.chunks) {
		return provider.Chunk{}, io.EOF
	}
	ch := f.chunks[f.i]
	f.i++
	return ch, nil
}

func (f *fakeStream) Close() error { return nil }

var _ provider.Stream = (*fakeStream)(nil)

func TestEstimateTokensGrowsWithContent(t *testing.T) {
	short := []core.Message{core.NewUserTextMessage("你好")}
	long := []core.Message{core.NewUserTextMessage(strings.Repeat("你好世界", 200))}
	if EstimateTokens(long) <= EstimateTokens(short) {
		t.Fatalf("更长内容应估算出更多 token：%d <= %d", EstimateTokens(long), EstimateTokens(short))
	}
}

func TestTriggerUsesRealUsageFirst(t *testing.T) {
	tr := Trigger{ContextWindow: 1000, Threshold: 0.8, MinMessages: 2}
	msgs := manyMessages(10)

	// 真实用量已超阈值：即使估算值很小也应触发。
	if ok, reason := tr.Decide(msgs, core.Usage{Input: 900}); !ok || reason != "真实用量" {
		t.Fatalf("应按真实用量触发：ok=%v reason=%q", ok, reason)
	}
	// 真实用量低：不触发。
	if ok, _ := tr.Decide(msgs, core.Usage{Input: 100}); ok {
		t.Fatal("用量低时不应触发")
	}
	// 没有真实用量时退回估算。
	if ok, reason := tr.Decide(manyMessages(200), core.Usage{}); !ok || reason != "字符估算" {
		t.Fatalf("应退回估算触发：ok=%v reason=%q", ok, reason)
	}
}

func TestTriggerSkipsShortConversation(t *testing.T) {
	tr := Trigger{ContextWindow: 10, Threshold: 0.8, MinMessages: 8}
	if ok, _ := tr.Decide(manyMessages(3), core.Usage{Input: 9999}); ok {
		t.Fatal("短会话不应压缩")
	}
}

func TestRecentWindowKeepsRecentAndAnchor(t *testing.T) {
	msgs := manyMessages(12)
	c := RecentWindow{KeepRecent: 4}

	res, err := c.Compress(context.Background(), msgs)
	if err != nil {
		t.Fatalf("压缩失败：%v", err)
	}
	if len(res.Messages) > len(msgs) || len(res.Messages) != 5 {
		t.Fatalf("压缩后条数 = %d，期望 5（1 条摘要 + 4 条近窗）", len(res.Messages))
	}
	if res.Messages[0].Role != core.System || res.Messages[0].Content[0].Type != core.ContentTypeSummary {
		t.Fatalf("首条应为摘要消息：%+v", res.Messages[0])
	}
	if !strings.Contains(res.Messages[0].Text(), "Conversation summary") {
		t.Fatalf("摘要内容异常：%q", res.Messages[0].Text())
	}
}

func TestRecentWindowDoesNotSplitToolPairs(t *testing.T) {
	msgs := []core.Message{
		core.NewUserTextMessage("第一步"),
		core.NewAssistantMessage(nil, "", []core.ToolCall{{ID: "c1", Name: "read_file", Arguments: `{"path":"a.txt"}`}}),
		core.NewToolMessage("c1", "read_file", "内容"),
		core.NewUserTextMessage("第二步"),
		core.NewUserTextMessage("第三步"),
	}
	c := RecentWindow{KeepRecent: 3}
	res, err := c.Compress(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	// 近窗不能以 tool 消息开头（否则 assistant 的调用与回执被切开）。
	if res.Messages[1].Role == core.Tool {
		t.Fatalf("压缩切断了工具调用对：%+v", res.Messages)
	}
}

func TestLLMCompressorUsesSummaryAndKeepsRecent(t *testing.T) {
	prov := &fakeProvider{chunks: []provider.Chunk{
		{Kind: provider.ChunkText, Text: goodSummary},
		{Kind: provider.ChunkDone, FinishReason: "stop", Usage: &core.Usage{Input: 500, Output: 30}},
	}}
	c := LLM{Provider: prov, Model: "m", KeepRecent: 2}

	res, err := c.Compress(context.Background(), manyMessages(8))
	if err != nil {
		t.Fatalf("压缩失败：%v", err)
	}
	if len(res.Messages) != 3 {
		t.Fatalf("条数 = %d，期望 3", len(res.Messages))
	}
	if !strings.Contains(res.Messages[0].Text(), "继续改造压缩器") {
		t.Fatalf("摘要未写入：%q", res.Messages[0].Text())
	}
	if res.Usage.Input != 500 {
		t.Fatalf("压缩用量未回传：%+v", res.Usage)
	}
}

func TestLLMCompressorReturnsErrorOnFailure(t *testing.T) {
	c := LLM{Provider: &fakeProvider{err: errors.New("上游挂了")}, Model: "m", KeepRecent: 2}
	if _, err := c.Compress(context.Background(), manyMessages(8)); err == nil {
		t.Fatal("上游失败时压缩器必须返回错误，让调用方保留原上下文")
	}
}

func TestLLMCompressorRejectsEmptySummary(t *testing.T) {
	prov := &fakeProvider{chunks: []provider.Chunk{{Kind: provider.ChunkDone, FinishReason: "stop"}}}
	c := LLM{Provider: prov, Model: "m", KeepRecent: 2}
	if _, err := c.Compress(context.Background(), manyMessages(8)); err == nil {
		t.Fatal("空摘要应当报错")
	}
}

func TestLLMCompressorRequiresProvider(t *testing.T) {
	if _, err := (LLM{}).Compress(context.Background(), manyMessages(8)); err == nil {
		t.Fatal("缺少 Provider 应当报错")
	}
}

// ---------------------------------------------------------------- 测试夹具

type fakeProvider struct {
	chunks    []provider.Chunk
	responses [][]provider.Chunk
	err       error
	calls     int
	last      provider.Request
}

func (f *fakeProvider) Stream(_ context.Context, req provider.Request) (provider.Stream, error) {
	f.calls++
	f.last = req
	if f.err != nil {
		return nil, f.err
	}
	if len(f.responses) > 0 {
		idx := f.calls - 1
		if idx >= len(f.responses) {
			idx = len(f.responses) - 1
		}
		return &fakeStream{chunks: f.responses[idx]}, nil
	}
	return &fakeStream{chunks: f.chunks}, nil
}

func manyMessages(n int) []core.Message {
	msgs := make([]core.Message, 0, n)
	for i := 0; i < n; i++ {
		if i%2 == 0 {
			msgs = append(msgs, core.NewUserTextMessage("用户第 "+strings.Repeat("x", i+1)+" 轮"))
			continue
		}
		msgs = append(msgs, core.NewAssistantMessage([]core.Content{{Type: core.ContentTypeText, Content: "助手回复"}}, "", nil))
	}
	return msgs
}

// goodSummary 覆盖了摘要要求的五个段落。
const goodSummary = `Task and goal
继续改造压缩器。

Completed changes
- compress/llm.go

Key facts and decisions
- 近窗按 token 预算保留

Remaining work and next steps
None

Constraints and user preferences
None`

func TestLLMCompressorRetriesBadSummary(t *testing.T) {
	bad := []provider.Chunk{
		{Kind: provider.ChunkText, Text: "太短了，没有段落。"},
		{Kind: provider.ChunkDone, FinishReason: "stop", Usage: &core.Usage{Input: 100, Output: 10}},
	}
	good := []provider.Chunk{
		{Kind: provider.ChunkText, Text: goodSummary},
		{Kind: provider.ChunkDone, FinishReason: "stop", Usage: &core.Usage{Input: 120, Output: 40}},
	}
	prov := &fakeProvider{responses: [][]provider.Chunk{bad, good}}
	c := LLM{Provider: prov, Model: "m", KeepRecent: 2}

	res, err := c.Compress(context.Background(), manyMessages(8))
	if err != nil {
		t.Fatalf("第二次摘要应当被接受：%v", err)
	}
	if prov.calls != 2 {
		t.Fatalf("应重试一次，实际调用 %d 次", prov.calls)
	}
	if res.Usage.Input != 220 || res.Usage.Output != 50 {
		t.Fatalf("两次摘要的用量应累加：%+v", res.Usage)
	}
	if !strings.Contains(prov.last.Messages[0].Text(), "was rejected") {
		t.Fatal("重试请求应说明上次摘要被拒")
	}
}

func TestLLMCompressorRejectsPersistentlyBadSummary(t *testing.T) {
	bad := []provider.Chunk{
		{Kind: provider.ChunkText, Text: "还是不合格"},
		{Kind: provider.ChunkDone, FinishReason: "stop"},
	}
	c := LLM{Provider: &fakeProvider{responses: [][]provider.Chunk{bad, bad}}, Model: "m", KeepRecent: 2}
	if _, err := c.Compress(context.Background(), manyMessages(8)); err == nil {
		t.Fatal("两次都不合格时应报错（保留原上下文）")
	}
}

func TestLLMCompressorKeepsTokenBudget(t *testing.T) {
	msgs := []core.Message{
		core.NewUserTextMessage("旧消息 " + strings.Repeat("旧", 4000)),
		core.NewAssistantMessage([]core.Content{{Type: core.ContentTypeText, Content: strings.Repeat("答", 4000)}}, "", nil),
		core.NewUserTextMessage("最近一条"),
	}
	prov := &fakeProvider{chunks: []provider.Chunk{
		{Kind: provider.ChunkText, Text: goodSummary},
		{Kind: provider.ChunkDone, FinishReason: "stop"},
	}}
	c := LLM{Provider: prov, Model: "m", KeepTokens: 200}

	res, err := c.Compress(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	// token 预算很小：近窗只应保留最后一条用户消息。
	if len(res.Messages) != 2 {
		t.Fatalf("压缩后条数 = %d，期望 2（摘要 + 最近一条）", len(res.Messages))
	}
	if !strings.Contains(res.Messages[1].Text(), "最近一条") {
		t.Fatalf("最近消息未保留：%q", res.Messages[1].Text())
	}
}

func TestLLMCompressorIncludesFilesAndAnchor(t *testing.T) {
	msgs := []core.Message{
		core.NewUserTextMessage("把 a.txt 改一下"),
		core.NewAssistantMessage(nil, "", []core.ToolCall{
			{ID: "c1", Name: "write_file", Arguments: `{"path":"a.txt","content":"x"}`},
		}),
		core.NewToolMessage("c1", "write_file", "ok"),
		core.NewUserTextMessage("继续"),
	}
	prov := &fakeProvider{chunks: []provider.Chunk{
		{Kind: provider.ChunkText, Text: goodSummary},
		{Kind: provider.ChunkDone, FinishReason: "stop"},
	}}
	c := LLM{Provider: prov, Model: "m", KeepRecent: 1}
	if _, err := c.Compress(context.Background(), msgs); err != nil {
		t.Fatal(err)
	}
	input := prov.last.Messages[0].Text()
	if !strings.Contains(input, "write_file a.txt") {
		t.Fatalf("摘要输入缺少文件清单：%s", input)
	}
	if !strings.Contains(input, "Most recent user instruction") {
		t.Fatalf("摘要输入缺少任务锚点：%s", input)
	}
}
