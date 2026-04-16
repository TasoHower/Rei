package agent

import (
	"context"
	"fmt"

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
		emit(step, &event.ToolCallStartPayload{
			ToolCallID: tc.ID,
			Name:       tc.Name,
			Arguments:  tc.Arguments,
		})

		content, err := tool.Invoke(ctx, infos, executor, tc)
		if err != nil {
			emit(step, &event.ToolCallEndPayload{
				ToolCallID: tc.ID,
				OK:         false,
				Output:     err.Error(),
				IsError:    true,
			})
			return nil, fmt.Errorf("tool %q: %w", tc.Name, err)
		}

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
