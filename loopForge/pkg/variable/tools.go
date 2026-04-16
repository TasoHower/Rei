package variable

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	lferrors "loopforge/pkg/errors"
	"loopforge/pkg/model"
)

const varSetToolName = "var_set"

// VarSetTool returns a ToolInfo for LLM-driven variable assignment (const_ keys rejected).
func VarSetTool(store *VarStore) *model.ToolInfo {
	return &model.ToolInfo{
		Name:        varSetToolName,
		Description: "Set a shared variable value. Keys prefixed with const_ are read-only and cannot be set here.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key":   map[string]interface{}{"type": "string", "description": "Variable name"},
				"value": map[string]interface{}{"description": "JSON value to assign"},
			},
			"required": []string{"key", "value"},
		},
		Handle: func(ctx context.Context, argumentsJSON string) (string, error) {
			slog.Debug("var_set begin", "args_len", len(argumentsJSON))
			if store == nil {
				s := FromContext(ctx)
				if s == nil {
					slog.Error("var_set failed", "reason", "no store in context or closure")
					return "", fmt.Errorf("%w", lferrors.ErrNoStore)
				}
				store = s
			}
			var args struct {
				Key   string          `json:"key"`
				Value json.RawMessage `json:"value"`
			}
			if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
				slog.Warn("var_set parse arguments failed", "err", err)
				return "", fmt.Errorf("parse var_set arguments: %w", err)
			}
			if args.Key == "" {
				return "", fmt.Errorf("%w: key is required", lferrors.ErrInvalidRequest)
			}
			var val any
			if len(args.Value) == 0 || string(args.Value) == "null" {
				val = nil
			} else if err := json.Unmarshal(args.Value, &val); err != nil {
				slog.Warn("var_set parse value failed", "key", args.Key, "err", err)
				return "", fmt.Errorf("parse var_set value: %w", err)
			}
			slog.Info("var_set applying", "key", args.Key, "value_type", fmt.Sprintf("%T", val))
			if err := store.AgentSet(args.Key, val); err != nil {
				slog.Warn("var_set AgentSet rejected", "key", args.Key, "err", err)
				return "", err
			}
			slog.Info("var_set ok", "key", args.Key)
			return "ok", nil
		},
	}
}
