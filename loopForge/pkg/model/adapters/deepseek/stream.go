package deepseek

import (
	"fmt"
	"io"
	"strings"

	lpmodel "github.com/TasoHower/rei/loopForge/pkg/model"

	dspk "github.com/cohesion-org/deepseek-go"
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

func (a *toolCallAccumulator) addDeltas(deltas []dspk.ToolCall) {
	for _, d := range deltas {
		idx := d.Index
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
	for ord, idx := range a.order {
		p := a.byIndex[idx]
		if p == nil {
			continue
		}
		args := p.arguments.String()
		if args == "" {
			args = "{}"
		}
		id := strings.TrimSpace(p.id)
		if id == "" {
			id = fmt.Sprintf("stream_tool_%d", ord)
		}
		out = append(out, lpmodel.ToolCallPart{
			ID:        id,
			Name:      p.name.String(),
			Arguments: args,
		})
	}
	return out
}

func runChatCompletionStream(stream dspk.ChatCompletionStream, out chan<- streamPart) {
	defer close(out)

	var sbReasoning strings.Builder
	var sbContent strings.Builder
	tools := newToolCallAccumulator()
	var lastUsage *dspk.StreamUsage

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
			if rc := ch.Delta.ReasoningContent; rc != "" {
				sbReasoning.WriteString(rc)
				out <- streamPart{
					msg: &lpmodel.Message{
						Role:             lpmodel.RoleAssistant,
						ReasoningContent: rc,
					},
				}
			}
			if c := ch.Delta.Content; c != "" {
				sbContent.WriteString(c)
				out <- streamPart{
					msg: &lpmodel.Message{
						Role:    lpmodel.RoleAssistant,
						Content: c,
					},
				}
			}
			if len(ch.Delta.ToolCalls) > 0 {
				tools.addDeltas(ch.Delta.ToolCalls)
			}
		}
	}

	reasoning := sbReasoning.String()
	content := sbContent.String()
	final := &lpmodel.Message{
		Role:             lpmodel.RoleAssistant,
		Content:          content,
		ReasoningContent: reasoning,
		ToolCalls:        tools.toToolCalls(),
	}
	if lastUsage != nil {
		final.InputTokens = int64(lastUsage.PromptTokens)
		final.OutputTokens = int64(lastUsage.CompletionTokens)
	}
	out <- streamPart{msg: final}
}
