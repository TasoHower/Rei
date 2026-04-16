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
// normal completion or error. On completion or terminal error, EventQueryEnd is
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

	if req == nil {
		emitError("invalid_request", "request is nil", 0)
		return nil
	}
	if a.ChatModel == nil {
		emitError("invalid_config", "Agent.ChatModel is nil", 0)
		return nil
	}

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

	m, setupErr := a.bindModel(varTools...)
	if setupErr != nil {
		emitError(setupErr.Code, setupErr.Msg, 0)
		return nil
	}

	var msgs []*model.Message
	var userTemplate, displayUser string
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

	var totalInputTokens, totalOutputTokens int64

	buildMetrics := func(steps int) outcome.RunMetrics {
		return a.runLoopMetrics(state, modelName, totalInputTokens, totalOutputTokens, steps)
	}
	
	buildOutcome := func(finalText string, termination outcome.TerminationReason, steps int) *outcome.RuntimeOutcome {
		return a.runLoopOutcome(runID, state, vstore, finalText, termination, buildMetrics(steps))
	}

	if state == nil || !state.SuppressBookends {
		emit(0, &event.StartPayload{})
		qm := req.UserMessage
		if freshSession {
			qm = displayUser
		}
		emit(0, &event.QuestionPayload{UserMessage: qm})
	}

	var lastText string
	for step := range maxSteps {
		if freshSession {
			userStep := ReplaceDoubleBraceParams(userTemplate, stringParamsFromVarStore(vstore))
			msgs = replaceFirstUserMessage(msgs, userStep)
		}

		fullSystem, err := a.runLoopFullSystem(ctx, req, vstore, baseSystem, step, runID)
		if err != nil {
			emitError("system_prompt_builder", err.Error(), step)
			return nil
		}

		msgs = replaceSystemMessage(msgs, fullSystem)

		emit(step, &event.CallLLMStartPayload{
			Model:        modelName,
			Temperature:  callCfg.Temperature,
			MaxTokens:    callCfg.MaxTokens,
			TopP:         callCfg.TopP,
			SystemPrompt: fullSystem,
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

		if len(sr.ToolCalls) == 0 {
			pendingQueryEnd = buildOutcome(lastText, outcome.TerminationCompleted, step+1)
			pendingQueryEndStep = step
			return nil
		}

		if ic := a.runLoopInterceptIfNeeded(msgs, sr, vstore, modelName, totalInputTokens, totalOutputTokens, step); ic != nil {
			pendingQueryEnd = nil
			return ic
		}

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

// runLoopFullSystem builds system instructions for one step: optional builder,
// variable block, then VarStore {{}} replacement.
func (a *Agent) runLoopFullSystem(
	ctx context.Context,
	req *request.RuntimeRequest,
	vstore *variable.VarStore,
	baseSystem string,
	step int,
	runID string,
) (string, error) {
	blockBeforeBuilder := ""
	if a.Variable {
		blockBeforeBuilder = vstore.PromptBlock()
	}

	full := baseSystem
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
		full, err = a.SystemPromptBuilder(sb)
		if err != nil {
			return "", err
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
		if full != "" {
			full = full + "\n\n" + block
		} else {
			full = block
		}
	}

	if p := stringParamsFromVarStore(vstore); len(p) > 0 {
		full = ReplaceDoubleBraceParams(full, p)
	}
	return full, nil
}

func (a *Agent) runLoopMetrics(
	state *LoopState,
	modelName string,
	totalIn, totalOut int64,
	steps int,
) outcome.RunMetrics {
	rm := outcome.RunMetrics{
		Model:        modelName,
		InputTokens:  totalIn,
		OutputTokens: totalOut,
		TotalTokens:  totalIn + totalOut,
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

func (a *Agent) runLoopOutcome(
	runID string,
	state *LoopState,
	vstore *variable.VarStore,
	finalText string,
	termination outcome.TerminationReason,
	metrics outcome.RunMetrics,
) *outcome.RuntimeOutcome {
	oc := &outcome.RuntimeOutcome{
		RunID:       runID,
		FinalText:   finalText,
		Termination: termination,
		Metrics:     metrics,
		VarStore:    vstore,
	}
	if state != nil {
		oc.TransferChain = append(state.TransferChain, a.Name)
	}
	return oc
}

// runLoopInterceptIfNeeded returns a non-nil *InterceptedCall when ToolInterceptor
// handles a tool call (e.g. transfer). sr must be the stream result after token
// totals for this round are applied to the running sums passed in as totalIn/totalOut.
func (a *Agent) runLoopInterceptIfNeeded(
	msgs []*model.Message,
	sr streamResult,
	vstore *variable.VarStore,
	modelName string,
	totalIn, totalOut int64,
	step int,
) *InterceptedCall {
	if a.ToolInterceptor == nil {
		return nil
	}
	for i := range sr.ToolCalls {
		tc := sr.ToolCalls[i]
		if a.ToolInterceptor(tc) {
			ic := &InterceptedCall{
				ToolCall: tc,
				Msgs:     msgs,
				Metrics: outcome.RunMetrics{
					Model:        modelName,
					InputTokens:  totalIn,
					OutputTokens: totalOut,
					TotalTokens:  totalIn + totalOut,
					Steps:        step + 1,
				},
			}
			if vstore != nil {
				ic.VarSnapshot = vstore.Snapshot()
			}
			return ic
		}
	}
	return nil
}
