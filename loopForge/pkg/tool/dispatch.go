package tool

import (
	"context"
	"fmt"

	lferrors "github.com/TasoHower/rei/loopForge/pkg/errors"
	"github.com/TasoHower/rei/loopForge/pkg/model"
)

// Invoke resolves and runs one tool call, in order:
//  1. ToolCallPart.Handle on the assistant turn (if attached),
//  2. ToolInfo.Handle for the same name in infos,
//  3. ToolExecutor.Execute as fallback.
func Invoke(ctx context.Context, infos []*model.ToolInfo, ex ToolExecutor, tc model.ToolCallPart) (string, error) {
	if tc.Handle != nil {
		return tc.Handle(ctx, tc.Arguments)
	}

	for _, info := range infos {
		if info == nil || info.Name != tc.Name {
			continue
		}
		if info.Handle != nil {
			return info.Handle(ctx, tc.Arguments)
		}
	}

	if ex != nil {
		return ex.Execute(ctx, tc.Name, tc.Arguments)
	}

	return "", fmt.Errorf("%w: %q (set ToolInfo.Handle, ToolCallPart.Handle, or a ToolExecutor)", lferrors.ErrNoHandler, tc.Name)
}

// ValidateBindings ensures every registered tool has either ToolInfo.Handle or a fallback ToolExecutor.
func ValidateBindings(infos []*model.ToolInfo, ex ToolExecutor) error {
	for _, t := range infos {
		if t == nil || t.Name == "" {
			continue
		}
		if t.Handle == nil && ex == nil {
			return fmt.Errorf("%w: tool %q must have ToolInfo.Handle or a ToolExecutor", lferrors.ErrInvalidConfig, t.Name)
		}
	}
	return nil
}
