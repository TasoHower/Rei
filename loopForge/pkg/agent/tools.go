package agent

import (
	"context"

	lferrors "loopforge/pkg/errors"
	"loopforge/pkg/log"
	"loopforge/pkg/model"
	"loopforge/pkg/runtime/event"
	"loopforge/pkg/tool"
)

// executeToolCalls invokes each tool call via tool.Invoke and emits start/end
// events. Returns the resulting tool messages, or an error on the first failure.
func executeToolCalls(
	ctx context.Context,
	infos []*model.ToolInfo,
	executor tool.ToolExecutor,
	toolCalls []model.ToolCallPart,
	emit func(int, event.EventPayload),
	step int,
) ([]*model.Message, error) {
	results := make([]*model.Message, 0, len(toolCalls))

	for i := range toolCalls {
		tc := toolCalls[i]
		log.Default().Info("tool call emit start",
			"step", step,
			"tool", tc.Name,
			"tool_call_id", tc.ID,
			"args_len", len(tc.Arguments),
		)
		emit(step, &event.ToolCallStartPayload{
			ToolCallID: tc.ID,
			Name:       tc.Name,
			Arguments:  tc.Arguments,
		})

		log.Default().Debug("tool call invoke", "tool", tc.Name, "tool_call_id", tc.ID)
		content, err := tool.Invoke(ctx, infos, executor, tc)
		if err != nil {
			log.Default().Warn("tool call failed",
				"tool", tc.Name,
				"tool_call_id", tc.ID,
				"err", err,
			)
			emit(step, &event.ToolCallEndPayload{
				ToolCallID: tc.ID,
				OK:         false,
				Output:     err.Error(),
				IsError:    true,
			})
			return nil, &lferrors.ToolError{Name: tc.Name, Cause: err}
		}

		log.Default().Info("tool call ok",
			"tool", tc.Name,
			"tool_call_id", tc.ID,
			"output_len", len(content),
		)
		emit(step, &event.ToolCallEndPayload{
			ToolCallID: tc.ID,
			OK:         true,
			Output:     content,
		})
		results = append(results, &model.Message{
			Role:       model.RoleTool,
			ToolCallID: tc.ID,
			Name:       tc.Name,
			Content:    content,
		})
	}

	return results, nil
}
