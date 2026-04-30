package openai

import (
	"io"
	"strings"

	lpmodel "loopforge/pkg/model"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/packages/ssestream"
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
	byIndex map[int64]*partialToolCall
	order   []int64
}

type partialToolCall struct {
	id        string
	name      strings.Builder
	arguments strings.Builder
}

func newToolCallAccumulator() *toolCallAccumulator {
	return &toolCallAccumulator{byIndex: make(map[int64]*partialToolCall)}
}

func (a *toolCallAccumulator) addDeltas(deltas []openai.ChatCompletionChunkChoiceDeltaToolCall) {
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

func runChatCompletionStream(stream *ssestream.Stream[openai.ChatCompletionChunk], out chan<- streamPart) {
	defer close(out)
	defer stream.Close()

	var sbContent strings.Builder
	tools := newToolCallAccumulator()
	var lastUsage *openai.CompletionUsage

	for stream.Next() {
		chunk := stream.Current()
		if chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0 {
			u := chunk.Usage
			lastUsage = &u
		}
		for _, choice := range chunk.Choices {
			if c := choice.Delta.Content; c != "" {
				sbContent.WriteString(c)
				out <- streamPart{
					msg: &lpmodel.Message{
						Role:    lpmodel.RoleAssistant,
						Content: c,
					},
				}
			}
			if len(choice.Delta.ToolCalls) > 0 {
				tools.addDeltas(choice.Delta.ToolCalls)
			}
		}
	}

	if err := stream.Err(); err != nil {
		out <- streamPart{err: err}
		return
	}

	final := &lpmodel.Message{
		Role:      lpmodel.RoleAssistant,
		Content:   sbContent.String(),
		ToolCalls: tools.toToolCalls(),
	}
	if lastUsage != nil {
		final.InputTokens = lastUsage.PromptTokens
		final.OutputTokens = lastUsage.CompletionTokens
	}
	out <- streamPart{msg: final}
}
