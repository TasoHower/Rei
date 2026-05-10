package agent

import (
	"context"
	"io"
	"strings"

	"github.com/TasoHower/rei/loopForge/pkg/model"
	"github.com/TasoHower/rei/loopForge/pkg/runtime/event"
)

// streamResult holds the accumulated output from consuming an LLM response stream.
type streamResult struct {
	Text          string
	ReasoningText string
	ToolCalls     []model.ToolCallPart
	InputTokens   int64
	OutputTokens  int64
	Err           error
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
	var reasoningBuf strings.Builder
	var toolCalls []model.ToolCallPart
	var inputTok, outputTok int64

	// Blind spot #2 fix: check ctx before each Recv() call
	// Deferred emission: a chunk's Content/ReasoningContent is only emitted
	// when the *next* chunk arrives, confirming the previous wasn't final.
	// The final chunk (confirmed by io.EOF) is emitted with IsFinal=true,
	// telling the frontend to *replace* the bubble content (final consistency).
	var prevChunk *model.Message

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

		// Emit the *previous* chunk as a regular delta (now confirmed not final).
		if prevChunk != nil {
			if prevChunk.ReasoningContent != "" {
				emit(step, &event.AnswerPayload{Delta: prevChunk.ReasoningContent, IsReasoning: true})
			}
			if prevChunk.Content != "" {
				emit(step, &event.AnswerPayload{Delta: prevChunk.Content})
			}
		}

		if chunk.ReasoningContent != "" {
			reasoningBuf.WriteString(chunk.ReasoningContent)
		}
		if chunk.Content != "" {
			textBuf.WriteString(chunk.Content)
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

		prevChunk = chunk
	}

	// After the loop, prevChunk is the final accumulated message.
	// Emit the complete accumulated text with IsFinal=true so the frontend
	// replaces the bubble content (final consistency).
	if prevChunk != nil {
		if prevChunk.ReasoningContent != "" {
			emit(step, &event.AnswerPayload{Delta: reasoningBuf.String(), IsReasoning: true, IsFinal: true})
		}
			if prevChunk.Content != "" {
			emit(step, &event.AnswerPayload{Delta: textBuf.String(), IsFinal: true})
		}
	}

	text := deduplicateContent(textBuf.String())
	if text != textBuf.String() {
		// Deduplication happened; re-emit with clean text so the frontend
		// display matches the final FullText.
		if prevChunk != nil && prevChunk.Content != "" {
			emit(step, &event.AnswerPayload{Delta: text, IsFinal: true, IsReasoning: false})
		}
	}
	reasoning := reasoningBuf.String()

	return streamResult{
		Text:          text,
		ReasoningText: reasoning,
		ToolCalls:     toolCalls,
		InputTokens:   inputTok,
		OutputTokens:  outputTok,
	}
}

// deduplicateContent removes content duplication at the halfway point.
// DeepSeek V4 sometimes repeats the same content twice in the Content stream
// (the model outputs reasoning_content again as content). This function detects
// exact 50/50 duplication and truncates to the first half.
func deduplicateContent(s string) string {
	n := len(s)
	if n < 20 {
		return s
	}
	half := n / 2
	// Only handle exact 50/50 split (byte-level), which matches the observed pattern.
	if n%2 == 0 && s[:half] == s[half:] {
		return s[:half]
	}
	return s
}
