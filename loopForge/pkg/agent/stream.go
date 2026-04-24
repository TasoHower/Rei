package agent

import (
	"context"
	"io"
	"strings"

	"loopforge/pkg/model"
	"loopforge/pkg/runtime/event"
)

// streamResult holds the accumulated output from consuming an LLM response stream.
type streamResult struct {
	Text         string
	ToolCalls    []model.ToolCallPart
	InputTokens  int64
	OutputTokens int64
	Err          error
}

// consumeStream reads all chunks from a model stream, emitting AnswerPayload
// for text deltas and accumulating tool calls and token counts.
func consumeStream(
	ctx context.Context,
	m model.ToolCallingChatModel,
	msgs []*model.Message,
	opts []model.CallOption,
	emit func(int, event.EventPayload),
	step int,
) streamResult {
	stream, err := m.Stream(ctx, msgs, opts...)
	if err != nil {
		return streamResult{Err: err}
	}

	var textBuf strings.Builder
	var toolCalls []model.ToolCallPart
	var inputTok, outputTok int64

	// Blind spot #2 fix: check ctx before each Recv() call
	for {
		select {
		case <-ctx.Done():
			return streamResult{Err: ctx.Err()}
		default:
		}
		chunk, recvErr := stream.Recv()
		if recvErr == io.EOF {
			break
		}
		if recvErr != nil {
			return streamResult{Err: recvErr}
		}
		if chunk == nil {
			continue
		}
		if chunk.Content != "" {
			textBuf.WriteString(chunk.Content)
			emit(step, &event.AnswerPayload{Delta: chunk.Content})
		}
		if len(chunk.ToolCalls) > 0 {
			toolCalls = append(toolCalls, chunk.ToolCalls...)
		}
		if chunk.InputTokens > 0 {
			inputTok = chunk.InputTokens
		}
		if chunk.OutputTokens > 0 {
			outputTok = chunk.OutputTokens
		}
	}

	return streamResult{
		Text:         textBuf.String(),
		ToolCalls:    toolCalls,
		InputTokens:  inputTok,
		OutputTokens: outputTok,
	}
}
