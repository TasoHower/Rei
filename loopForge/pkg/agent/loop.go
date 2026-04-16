package agent

import (
	"context"

	"loopforge/pkg/log"
	"loopforge/pkg/model"
	"loopforge/pkg/runtime/event"
	"loopforge/pkg/runtime/outcome"
	"loopforge/pkg/runtime/request"
	"loopforge/pkg/variable"
)

// RunLoop executes the tool-calling loop.
//
// inheritedMsgs, when non-nil, replaces the default message construction (used
// by orchestrators to pass conversation history across agent transfers).
//
// state carries accumulated metrics/chain from the orchestrator; nil for
// standalone use.
//
// Returns an *InterceptedCall if a ToolInterceptor matched, or nil for
// normal completion / error.
func (a *Agent) RunLoop(
	ctx context.Context,
	req *request.RuntimeRequest,
	ch chan<- *event.RuntimeEvent,
	inheritedMsgs []*model.Message,
	state *LoopState,
) *InterceptedCall {
	runID := runIDFrom(req)
	var vstore *variable.VarStore

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
		oc := &outcome.RuntimeOutcome{
			RunID:       runID,
			Termination: outcome.TerminationError,
		}
		if vstore != nil {
			oc.VarStore = vstore
		}
		emitQueryEnd(oc)
	}

	// --- validation ---

	if req == nil {
		emitError("invalid_request", "request is nil")
		return nil
	}
	if a.ChatModel == nil {
		emitError("invalid_config", "Agent.ChatModel is nil")
		return nil
	}

	// --- variable store ---

	if state != nil && state.VarStore != nil {
		vstore = state.VarStore
	} else {
		vstore = variable.New()
	}
	ctx = variable.NewContext(ctx, vstore)
	baseSystem := a.SystemInstructions

	var varTools []*model.ToolInfo
	if a.Variable {
		varTools = []*model.ToolInfo{variable.VarSetTool(vstore)}
	}

	// --- prepare tools and model ---

	m, setupErr := a.bindModel(varTools...)
	if setupErr != nil {
		emitError(setupErr.Code, setupErr.Msg)
		return nil
	}

	// --- resolve runtime parameters ---

	maxSteps := a.resolveMaxSteps(req)
	opts, modelName := a.resolveCallOptions(req)
	callCfg := model.ApplyCallOptions(opts...)
	msgs := a.buildMessages(inheritedMsgs, req)

	// --- metrics tracking ---

	var totalInputTokens, totalOutputTokens int64

	buildMetrics := func(steps int) outcome.RunMetrics {
		rm := outcome.RunMetrics{
			Model:        modelName,
			InputTokens:  totalInputTokens,
			OutputTokens: totalOutputTokens,
			TotalTokens:  totalInputTokens + totalOutputTokens,
			Steps:        steps,
		}
		if state != nil {
			rm.InputTokens += state.AccumulatedMetrics.InputTokens
			rm.OutputTokens += state.AccumulatedMetrics.OutputTokens
			rm.TotalTokens += state.AccumulatedMetrics.TotalTokens
			rm.Steps += state.AccumulatedMetrics.Steps
		}
		return rm
	}

	buildOutcome := func(finalText string, termination outcome.TerminationReason, steps int) *outcome.RuntimeOutcome {
		oc := &outcome.RuntimeOutcome{
			RunID:       runID,
			FinalText:   finalText,
			Termination: termination,
			Metrics:     buildMetrics(steps),
			VarStore:    vstore,
		}
		if state != nil {
			oc.TransferChain = append(state.TransferChain, a.Name)
		}
		return oc
	}

	// --- emit bookends ---

	if state == nil || !state.SuppressBookends {
		emit(0, &event.StartPayload{})
		emit(0, &event.QuestionPayload{UserMessage: req.UserMessage})
	}

	// --- main loop ---

	var lastText string
	for step := range maxSteps {
		fullSystem := baseSystem
		if a.Variable {
			block := vstore.PromptBlock()
			if block != "" {
				if fullSystem != "" {
					fullSystem = fullSystem + "\n\n" + block
				} else {
					fullSystem = block
				}
			}
			log.Default().Debug("variables prompt block",
				"agent", a.Name,
				"run_id", runID,
				"step", step,
				"block_len", len(block),
				"var_count", vstore.Len(),
			)
		}
		msgs = replaceSystemMessage(msgs, fullSystem)

		emit(step, &event.CallLLMStartPayload{
			Model:       modelName,
			Temperature: callCfg.Temperature,
			MaxTokens:   callCfg.MaxTokens,
			TopP:        callCfg.TopP,
		})

		sr := consumeStream(ctx, m, msgs, opts, emit, step)
		if sr.Err != nil {
			emit(step, &event.CallLLMEndPayload{FinishReason: event.FinishError})
			emitError("generate", sr.Err.Error())
			return nil
		}

		finishReason := event.FinishStop
		if len(sr.ToolCalls) > 0 {
			finishReason = event.FinishToolCalls
		}
		emit(step, &event.CallLLMEndPayload{
			FinishReason: finishReason,
			FullText:     sr.Text,
			InputTokens:  sr.InputTokens,
			OutputTokens: sr.OutputTokens,
		})
		totalInputTokens += sr.InputTokens
		totalOutputTokens += sr.OutputTokens

		lastText = sr.Text
		msgs = append(msgs, &model.Message{
			Role:         model.RoleAssistant,
			Content:      sr.Text,
			ToolCalls:    sr.ToolCalls,
			InputTokens:  sr.InputTokens,
			OutputTokens: sr.OutputTokens,
		})

		// No tool calls → model finished naturally.
		if len(sr.ToolCalls) == 0 {
			emitQueryEnd(buildOutcome(lastText, outcome.TerminationCompleted, step+1))
			return nil
		}

		// Check for intercepted tool calls (e.g. transfer).
		// Intercepted calls are control-flow signals — no ToolCallStart/End
		// events are emitted. The orchestrator emits its own event (e.g.
		// AgentTransfer) to signal what happened.
		if a.ToolInterceptor != nil {
			for i := range sr.ToolCalls {
				tc := sr.ToolCalls[i]
				if a.ToolInterceptor(tc) {
					ic := &InterceptedCall{
						ToolCall: tc,
						Msgs:     msgs,
						Metrics: outcome.RunMetrics{
							Model:        modelName,
							InputTokens:  totalInputTokens,
							OutputTokens: totalOutputTokens,
							TotalTokens:  totalInputTokens + totalOutputTokens,
							Steps:        step + 1,
						},
					}
					if vstore != nil {
						ic.VarSnapshot = vstore.Snapshot()
					}
					return ic
				}
			}
		}

		// Execute regular tool calls (use merged list so ExtraTools + var_set resolve).
		invokeInfos := a.mergedToolInfos(varTools...)
		toolMsgs, toolErr := executeToolCalls(ctx, invokeInfos, a.Executor, sr.ToolCalls, emit, step)
		if toolErr != nil {
			emitError("tool_exec", toolErr.Error())
			return nil
		}
		msgs = append(msgs, toolMsgs...)
	}

	emitQueryEnd(buildOutcome(lastText, outcome.TerminationMaxSteps, maxSteps))
	return nil
}
