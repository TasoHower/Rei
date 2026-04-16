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
// normal completion / error. On completion or terminal error, EventQueryEnd is
// emitted last (after the final EventCallLLMEnd when an LLM round occurred).
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

	// QueryEnd is always the last outbound event for this RunLoop invocation
	// (after the final call_llm_end and any error payload). Transfer intercepts
	// clear pendingQueryEnd before returning so the runner can continue the chain.
	var pendingQueryEnd *outcome.RuntimeOutcome
	var pendingQueryEndStep int
	defer func() {
		if pendingQueryEnd != nil {
			emit(pendingQueryEndStep, &event.QueryEndPayload{Outcome: pendingQueryEnd})
		}
	}()

	emitError := func(code, msg string, step int) {
		emit(step, &event.ErrorPayload{Code: code, Message: msg})
		oc := &outcome.RuntimeOutcome{
			RunID:       runID,
			Termination: outcome.TerminationError,
		}
		if vstore != nil {
			oc.VarStore = vstore
		}
		pendingQueryEnd = oc
		pendingQueryEndStep = step
	}

	// --- validation ---

	if req == nil {
		emitError("invalid_request", "request is nil", 0)
		return nil
	}
	if a.ChatModel == nil {
		emitError("invalid_config", "Agent.ChatModel is nil", 0)
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
		emitError(setupErr.Code, setupErr.Msg, 0)
		return nil
	}

	// --- initial messages ---

	var msgs []*model.Message
	var userTemplate string
	var displayUser string
	freshSession := inheritedMsgs == nil

	if freshSession {
		userTemplate = req.UserMessage
		p0 := stringParamsFromVarStore(vstore)
		displayUser = ReplaceDoubleBraceParams(userTemplate, p0)
		msgs = a.buildMessages(nil, req, displayUser)
	} else {
		msgs = a.buildMessages(inheritedMsgs, req, "")
	}

	maxSteps := a.resolveMaxSteps(req)
	opts, modelName := a.resolveCallOptions(req)
	callCfg := model.ApplyCallOptions(opts...)

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
		qm := req.UserMessage
		if freshSession {
			qm = displayUser
		}
		emit(0, &event.QuestionPayload{UserMessage: qm})
	}

	// --- main loop ---

	var lastText string
	for step := range maxSteps {
		if freshSession {
			userStep := ReplaceDoubleBraceParams(userTemplate, stringParamsFromVarStore(vstore))
			msgs = replaceFirstUserMessage(msgs, userStep)
		}

		blockBeforeBuilder := ""
		if a.Variable {
			blockBeforeBuilder = vstore.PromptBlock()
		}

		fullSystem := baseSystem
		if a.SystemPromptBuilder != nil {
			sb := SystemPromptBuildContext{
				Ctx:                 ctx,
				Request:             req,
				VarStore:            vstore,
				Agent:               a,
				Step:                step,
				BaseSystemPrompt:    baseSystem,
				VariablePromptBlock: blockBeforeBuilder,
			}

			var err error

			fullSystem, err = a.SystemPromptBuilder(sb)
			if err != nil {
				emitError("system_prompt_builder", err.Error(), step)
				return nil
			}
		}

		block := ""
		if a.Variable {
			block = vstore.PromptBlock()
			log.Default().Debug("variables prompt block",
				"agent", a.Name,
				"run_id", runID,
				"step", step,
				"block_len", len(block),
				"var_count", vstore.Len(),
			)
		}
		if block != "" {
			if fullSystem != "" {
				fullSystem = fullSystem + "\n\n" + block
			} else {
				fullSystem = block
			}
		}

		if p := stringParamsFromVarStore(vstore); len(p) > 0 {
			fullSystem = ReplaceDoubleBraceParams(fullSystem, p)
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
			emitError("generate", sr.Err.Error(), step)
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

		// No tool calls → model finished naturally (call_llm_end already emitted).
		if len(sr.ToolCalls) == 0 {
			pendingQueryEnd = buildOutcome(lastText, outcome.TerminationCompleted, step+1)
			pendingQueryEndStep = step
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
					pendingQueryEnd = nil
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
			emitError("tool_exec", toolErr.Error(), step)
			return nil
		}
		msgs = append(msgs, toolMsgs...)
	}

	pendingQueryEnd = buildOutcome(lastText, outcome.TerminationMaxSteps, maxSteps)
	pendingQueryEndStep = 0
	if maxSteps > 0 {
		pendingQueryEndStep = maxSteps - 1
	}
	return nil
}
