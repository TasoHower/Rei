package lark

import (
	"io"
	"strings"

	lpmodel "github.com/TasoHower/rei/loopForge/pkg/model"

	arkmodel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
	"github.com/volcengine/volcengine-go-sdk/service/arkruntime/utils"
)

type streamPart struct {
	msg *lpmodel.Message
	err error
}

type chanStreamReader struct {
	ch <-chan streamPart
}

func newChanStreamReader(ch <-chan streamPart) lpmodel.MessageStreamReader {
	return &chanStreamReader{ch: ch}
}

func (r *chanStreamReader) Recv() (*lpmodel.Message, error) {
	item, ok := <-r.ch
	if !ok {
		return nil, io.EOF
	}
	if item.err != nil {
		return nil, item.err
	}
	return item.msg, nil
}

var _ lpmodel.MessageStreamReader = (*chanStreamReader)(nil)

// toolCallAccumulator merges OpenAI-style streaming tool_call fragments (by index).
type toolCallAccumulator struct {
	byIndex map[int]*partialToolCall
	order   []int
}

type partialToolCall struct {
	id        string
	name      strings.Builder
	arguments strings.Builder
}

func newToolCallAccumulator() *toolCallAccumulator {
	return &toolCallAccumulator{byIndex: make(map[int]*partialToolCall)}
}

func (a *toolCallAccumulator) addDeltas(deltas []*arkmodel.ToolCall) {
	for _, d := range deltas {
		if d == nil {
			continue
		}
		idx := 0
		if d.Index != nil {
			idx = *d.Index
		}
		p := a.byIndex[idx]
		if p == nil {
			p = &partialToolCall{}
			a.byIndex[idx] = p
			a.order = append(a.order, idx)
		}
		if d.ID != "" {
			p.id = d.ID
		}
		if d.Function.Name != "" {
			p.name.WriteString(d.Function.Name)
		}
		if d.Function.Arguments != "" {
			p.arguments.WriteString(d.Function.Arguments)
		}
	}
}

func (a *toolCallAccumulator) toToolCalls() []lpmodel.ToolCallPart {
	if len(a.order) == 0 {
		return nil
	}
	out := make([]lpmodel.ToolCallPart, 0, len(a.order))
	for _, idx := range a.order {
		p := a.byIndex[idx]
		if p == nil {
			continue
		}
		args := p.arguments.String()
		if args == "" {
			args = "{}"
		}
		out = append(out, lpmodel.ToolCallPart{
			ID:        p.id,
			Name:      p.name.String(),
			Arguments: args,
		})
	}
	return out
}

func runChatCompletionStream(
	stream *utils.ChatCompletionStreamReader,
	out chan<- streamPart,
) {
	defer close(out)

	var sbReasoning strings.Builder
	var sbContent strings.Builder
	tools := newToolCallAccumulator()
	var lastUsage *arkmodel.Usage

	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			out <- streamPart{err: err}
			return
		}
		if resp.Usage != nil {
			u := *resp.Usage
			lastUsage = &u
		}
		for _, ch := range resp.Choices {
			if ch == nil {
				continue
			}
			if rc := ch.Delta.ReasoningContent; rc != nil && *rc != "" {
				sbReasoning.WriteString(*rc)
				out <- streamPart{
					msg: &lpmodel.Message{
						Role:             lpmodel.RoleAssistant,
						ReasoningContent: *rc,
					},
				}
			}
			if d := ch.Delta.Content; d != "" {
				sbContent.WriteString(d)
				out <- streamPart{
					msg: &lpmodel.Message{
						Role:    lpmodel.RoleAssistant,
						Content: d,
					},
				}
			}
			if len(ch.Delta.ToolCalls) > 0 {
				tools.addDeltas(ch.Delta.ToolCalls)
			}
		}
	}

	final := &lpmodel.Message{
		Role:      lpmodel.RoleAssistant,
		Content:   sbContent.String(),
		ToolCalls: tools.toToolCalls(),
	}
	if rc := sbReasoning.String(); rc != "" {
		final.ReasoningContent = rc
	}
	if lastUsage != nil {
		final.InputTokens = int64(lastUsage.PromptTokens)
		final.OutputTokens = int64(lastUsage.CompletionTokens)
	}
	out <- streamPart{msg: final}
}
