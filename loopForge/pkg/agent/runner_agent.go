package agent

import (
	"context"
	"fmt"
	"io"
	"strings"

	"loopforge/pkg/model"
	"loopforge/pkg/runtime/event"
	"loopforge/pkg/runtime/outcome"
	"loopforge/pkg/runtime/request"
	"loopforge/pkg/tool"
)

// RunnerAgent is a concrete Agent that drives a tool-calling loop using pkg/model only.
// Model access (HTTP/SDK provider wiring) stays in pkg/model adapters; this type does not use agent-sdk-go Agent or Runner.
//
// Function calling: register ToolInfos (schema for the model). For each tool, set ToolInfo.Handle to bind the
// local Go function, and/or set Executor as a fallback (see loopforge/pkg/tool).
type RunnerAgent struct {
	Name               string
	ModelName          string
	SystemInstructions string
	ChatModel          model.ToolCallingChatModel
	ToolInfos          []*model.ToolInfo
	Executor           tool.ToolExecutor

	MaxSteps int
	// CallOptions are passed to every Generate (e.g. model.WithTemperature); per-request WithModel is merged from RuntimeRequest.Options.Model.
	CallOptions []model.CallOption
}

var _ Agent = (*RunnerAgent)(nil)

// Run starts the agent loop in a goroutine and returns a channel that streams RuntimeEvents.
// The channel is closed when the run finishes. The final event is always EventQueryEnd
// whose QueryEndPayload carries the RuntimeOutcome. Errors are sent as EventError
// followed by EventQueryEnd with TerminationError.
func (a *RunnerAgent) Run(ctx context.Context, req *request.RuntimeRequest) <-chan *event.RuntimeEvent {
	ch := make(chan *event.RuntimeEvent, 8)

	go func() {
		defer close(ch)
		a.runLoop(ctx, req, ch)
	}()

	return ch
}

func (a *RunnerAgent) runLoop(ctx context.Context, req *request.RuntimeRequest, ch chan<- *event.RuntimeEvent) {
	runID := runIDFrom(req)

	emit := func(step int, payload event.EventPayload) {
		select {
		case ch <- event.Emit(runID, step, payload):
		case <-ctx.Done():
		}
	}

	emitQueryEnd := func(oc *outcome.RuntimeOutcome) {
		emit(0, &event.QueryEndPayload{Outcome: oc})
	}

	emitError := func(code, msg string) {
		emit(0, &event.ErrorPayload{Code: code, Message: msg})
		emitQueryEnd(&outcome.RuntimeOutcome{
			RunID:       runID,
			Termination: outcome.TerminationError,
		})
	}

	if req == nil {
		emitError("invalid_request", "request is nil")
		return
	}
	if a.ChatModel == nil {
		emitError("invalid_config", "RunnerAgent.ChatModel is nil")
		return
	}
	if len(a.ToolInfos) > 0 {
		if err := tool.ValidateBindings(a.ToolInfos, a.Executor); err != nil {
			emitError("invalid_config", err.Error())
			return
		}
	}

	m := a.ChatModel
	if len(a.ToolInfos) > 0 {
		next, err := a.ChatModel.WithTools(a.ToolInfos)
		if err != nil {
			emitError("tool_bind", err.Error())
			return
		}
		m = next
	}

	maxSteps := a.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 16
	}
	if req.Options.MaxSteps != nil && *req.Options.MaxSteps > 0 {
		maxSteps = *req.Options.MaxSteps
	}

	opts := append([]model.CallOption(nil), a.CallOptions...)
	modelName := a.ModelName
	if req.Options.Model != "" {
		modelName = req.Options.Model
		opts = append(opts, model.WithModel(modelName))
	}

	callCfg := model.ApplyCallOptions(opts...)

	msgs := make([]*model.Message, 0, 4)
	if strings.TrimSpace(a.SystemInstructions) != "" {
		msgs = append(msgs, &model.Message{Role: model.RoleSystem, Content: strings.TrimSpace(a.SystemInstructions)})
	}
	msgs = append(msgs, &model.Message{Role: model.RoleUser, Content: req.UserMessage})

	var totalInputTokens, totalOutputTokens int64

	buildMetrics := func(steps int) outcome.RunMetrics {
		total := totalInputTokens + totalOutputTokens
		return outcome.RunMetrics{
			Model:        modelName,
			InputTokens:  totalInputTokens,
			OutputTokens: totalOutputTokens,
			TotalTokens:  total,
			Steps:        steps,
		}
	}

	emit(0, &event.StartPayload{})
	emit(0, &event.QuestionPayload{UserMessage: req.UserMessage})

	var lastText string
	for step := 0; step < maxSteps; step++ {
		emit(step, &event.CallLLMStartPayload{
			Model:       modelName,
			Temperature: callCfg.Temperature,
			MaxTokens:   callCfg.MaxTokens,
			TopP:        callCfg.TopP,
		})

		stream, err := m.Stream(ctx, msgs, opts...)
		if err != nil {
			emit(step, &event.CallLLMEndPayload{FinishReason: event.FinishError})
			emitError("generate", err.Error())
			return
		}

		var textBuf strings.Builder
		var toolCalls []model.ToolCallPart
		var inputTok, outputTok int64
		var streamErr error

		for {
			chunk, recvErr := stream.Recv()
			if recvErr == io.EOF {
				break
			}
			if recvErr != nil {
				streamErr = recvErr
				break
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

		if streamErr != nil {
			emit(step, &event.CallLLMEndPayload{FinishReason: event.FinishError})
			emitError("generate", streamErr.Error())
			return
		}

		fullText := textBuf.String()
		finishReason := event.FinishStop
		if len(toolCalls) > 0 {
			finishReason = event.FinishToolCalls
		}
		emit(step, &event.CallLLMEndPayload{
			FinishReason: finishReason,
			FullText:     fullText,
			InputTokens:  inputTok,
			OutputTokens: outputTok,
		})
		totalInputTokens += inputTok
		totalOutputTokens += outputTok

		lastText = fullText
		assistant := &model.Message{
			Role:         model.RoleAssistant,
			Content:      fullText,
			ToolCalls:    toolCalls,
			InputTokens:  inputTok,
			OutputTokens: outputTok,
		}
		msgs = append(msgs, assistant)

		if len(toolCalls) == 0 {
			emitQueryEnd(&outcome.RuntimeOutcome{
				RunID:       runID,
				FinalText:   lastText,
				Termination: outcome.TerminationCompleted,
				Metrics:     buildMetrics(step + 1),
			})
			return
		}

		for i := range toolCalls {
			tc := toolCalls[i]
			emit(step, &event.ToolCallStartPayload{
				ToolCallID: tc.ID,
				Name:       tc.Name,
				Arguments:  tc.Arguments,
			})

			content, err := tool.Invoke(ctx, a.ToolInfos, a.Executor, tc)
			if err != nil {
				emit(step, &event.ToolCallEndPayload{
					ToolCallID: tc.ID,
					OK:         false,
					Output:     err.Error(),
					IsError:    true,
				})
				emitError("tool_exec", fmt.Sprintf("tool %q: %v", tc.Name, err))
				return
			}

			emit(step, &event.ToolCallEndPayload{
				ToolCallID: tc.ID,
				OK:         true,
				Output:     content,
			})
			msgs = append(msgs, &model.Message{
				Role:       model.RoleTool,
				ToolCallID: tc.ID,
				Name:       tc.Name,
				Content:    content,
			})
		}
	}

	emitQueryEnd(&outcome.RuntimeOutcome{
		RunID:       runID,
		FinalText:   lastText,
		Termination: outcome.TerminationMaxSteps,
		Metrics:     buildMetrics(maxSteps),
	})
}

func runIDFrom(req *request.RuntimeRequest) string {
	if req.SessionID != "" {
		return req.SessionID
	}
	return "run"
}
