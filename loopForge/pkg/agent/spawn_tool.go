package agent

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/bytedance/sonic"

	"loopforge/internal/defaults"
	"loopforge/pkg/model"
	"loopforge/pkg/runtime/event"
	"loopforge/pkg/runtime/exchange"
	"loopforge/pkg/runtime/outcome"
	"loopforge/pkg/runtime/request"
)

type spawnSubagentArgs struct {
	Task            string   `json:"task"`
	SystemAddendum  string   `json:"system_addendum"`
	SkillIDs        []string `json:"skill_ids"`
	Model           string   `json:"model"`
	MaxSteps        *int     `json:"max_steps"`
	AllowChildSpawn bool     `json:"allow_child_spawn"`
	Lifecycle       string   `json:"lifecycle"`
}

// addChildRunMetricsToLoopState rolls child [outcome.RunMetrics] into the parent
// [LoopState.AccumulatedMetrics] so the parent run's final [runLoopMetrics] includes
// subtree token usage (same idea as [runner.Runner] per-transfer accumulation).
func addChildRunMetricsToLoopState(st *LoopState, child outcome.RunMetrics) {
	if st == nil {
		return
	}
	st.AccumulatedMetrics.InputTokens += child.InputTokens
	st.AccumulatedMetrics.OutputTokens += child.OutputTokens
	st.AccumulatedMetrics.TotalTokens += child.TotalTokens
	st.AccumulatedMetrics.Steps += child.Steps
	st.AccumulatedMetrics.TotalCostUSD += child.TotalCostUSD
}

// buildSpawnSubagentVarTool returns a runtime [model.ToolInfo] for the spawn builtin, or nil
// when the agent is not configured for spawn or depth disallows another child.
// eventCh, when non-nil, switches the spawn handler to async mode: the handler returns
// immediately so the parent LLM can continue, while the child runs in the background.
// The parent's RunLoop calls WaitAsyncSpawns before emitting QueryEnd, ensuring the child
// completes before the parent exits. The child's final answer is delivered as an AnswerPayload
// on the event channel when it finishes.
func buildSpawnSubagentVarTool(parent *exchange.RunRef, parentTemplate *Agent, req *request.RuntimeRequest, loop *LoopState, eventCh chan<- *event.RuntimeEvent) *model.ToolInfo {
	if parentTemplate == nil || parent == nil {
		return nil
	}
	if !parentTemplate.SpawnEnabled || parentTemplate.ChildAgentBuilder == nil {
		return nil
	}
	maxD := ResolveSpawnMaxDepth(req)
	if parent.Depth+1 >= maxD {
		return nil
	}
	sp := parentTemplate.Spawner
	if sp == nil {
		sp = NewDefaultSpawner(parentTemplate, maxD)
	}
	name := defaults.BuiltinSpawnToolName
	return &model.ToolInfo{
		Name:        name,
		Description: "Spawn a child agent to run a subtask in an isolated loop. Returns a JSON SpawnResult.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"task": map[string]interface{}{
					"type":        "string",
					"description": "User task for the child agent",
				},
				"system_addendum": map[string]interface{}{
					"type":        "string",
					"description": "Optional extra system instructions for the child",
				},
				"skill_ids": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "string",
					},
					"description": "Optional skill ids for the child",
				},
				"model": map[string]interface{}{
					"type":        "string",
					"description": "Optional model override for the child",
				},
				"max_steps": map[string]interface{}{
					"type":        "integer",
					"description": "Optional max ReAct steps for the child",
				},
				"allow_child_spawn": map[string]interface{}{
					"type":        "boolean",
					"description": "If true, the child may register spawn in its run (default false)",
				},
			},
			"required": []string{"task"},
		},
		Handle: makeSpawnHandler(parent, sp, maxD, loop, eventCh),
	}
}

func makeSpawnHandler(
	parent *exchange.RunRef,
	sp Spawner,
	maxD int,
	loop *LoopState,
	eventCh chan<- *event.RuntimeEvent,
) model.ToolCallHandler {
	return func(ctx context.Context, argumentsJSON string) (string, error) {
		var a spawnSubagentArgs
		if err := sonic.UnmarshalString(argumentsJSON, &a); err != nil {
			return "", err
		}
		if a.Task == "" {
			return "", fmt.Errorf("spawn: task is required")
		}
		spec := &exchange.SpawnSpec{
			Task:            a.Task,
			SystemAddendum:  a.SystemAddendum,
			SkillIDs:        a.SkillIDs,
			ModelOverride:   a.Model,
			AllowChildSpawn: a.AllowChildSpawn,
		}
		if a.MaxSteps != nil && *a.MaxSteps > 0 {
			spec.LoopOverrides = exchange.LoopOverrides{MaxSteps: a.MaxSteps}
		}
		if a.Lifecycle != "" {
			spec.Lifecycle = exchange.Lifecycle(a.Lifecycle)
		} else {
			spec.Lifecycle = exchange.LifecycleEphemeral
		}
		// Belt-and-suspenders: re-check depth at invoke time
		if parent != nil && parent.Depth+1 >= maxD {
			res, err := sonic.MarshalString(&exchange.SpawnResult{
				ChildRunRef: childRunRefPlanned(parent, "-rejected"),
				Status:      exchange.SpawnRejected,
				FinalText:   "",
				Error:       &exchange.SpawnError{Code: "max_depth", Message: "spawn would exceed max depth at invoke time"},
			})
			if err != nil {
				return "", err
			}
			return res, nil
		}

		if eventCh != nil {
			// Async mode: return immediately so the parent LLM can continue,
			// while the child runs in the background. The parent's RunLoop
			// calls WaitAsyncSpawns before emitting QueryEnd, ensuring the
			// child completes first. When the child finishes, its final answer
			// is delivered as a single AnswerPayload on the event channel.
			if loop != nil {
				loop.AddAsyncSpawn()
			}
			go func() {
				if loop != nil {
					defer loop.DoneAsyncSpawn()
				}
				res, err := sp.Spawn(ctx, derefRef(parent), spec)
				if err != nil {
					slog.Warn("spawn child agent failed", "error", err)
					return
				}
				if res != nil {
					addChildRunMetricsToLoopState(loop, res.Metrics)
					slog.Info("spawn child agent completed",
						"child_run_id", res.ChildRunRef.RunID,
						"status", res.Status,
						"final_text_len", len(res.FinalText),
						"input_tokens", res.Metrics.InputTokens,
						"output_tokens", res.Metrics.OutputTokens,
						"steps", res.Metrics.Steps,
					)
					loop.CollectAsyncSpawnResult(res.FinalText)
				}
			}()
			return sonic.MarshalString(&exchange.SpawnResult{
				ChildRunRef: childRunRefPlanned(parent, "-async"),
				Status:      exchange.SpawnCompleted,
				FinalText:   "",
			})
		}

		// Sync mode (no eventCh): block until child completes
		res, err := sp.Spawn(ctx, derefRef(parent), spec)
		if err != nil {
			return "", err
		}
		if res != nil {
			addChildRunMetricsToLoopState(loop, res.Metrics)
			slog.Info("spawn child agent completed",
				"child_run_id", res.ChildRunRef.RunID,
				"status", res.Status,
				"final_text_len", len(res.FinalText),
				"input_tokens", res.Metrics.InputTokens,
				"output_tokens", res.Metrics.OutputTokens,
				"steps", res.Metrics.Steps,
			)
		}
		return sonic.MarshalString(res)
	}
}

func derefRef(p *exchange.RunRef) exchange.RunRef {
	if p == nil {
		return exchange.RunRef{}
	}
	return *p
}
