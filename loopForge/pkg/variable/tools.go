package variable

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"

	"github.com/bytedance/sonic"
	lferrors "loopforge/pkg/errors"
	"loopforge/pkg/model"
)

const varSetToolName = "var_set"

// VarSetTool returns a ToolInfo for LLM-driven variable assignment (const_ keys rejected).
func VarSetTool(store *VarStore) *model.ToolInfo {
	return &model.ToolInfo{
		Name: varSetToolName,
		Description: "Set one or more shared variables in a single call. " +
			"Pass only keys you need to change inside `updates`; omit variables that stay the same. " +
			"Keys prefixed with const_ are read-only and cannot be set here. " +
			"Legacy single-field form `key` + `value` is still accepted.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"updates": map[string]interface{}{
					"type":        "object",
					"description": "Map from variable name to JSON value. Include only variables you are changing; leave others out.",
				},
				"key": map[string]interface{}{
					"type":        "string",
					"description": "Legacy: single variable name (use `updates` for multiple keys).",
				},
				"value": map[string]interface{}{
					"type":        "object",
					"description": "Legacy: JSON object value for `key` (ignored when `updates` is non-empty).",
				},
			},
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
			var envelope struct {
				Updates map[string]json.RawMessage `json:"updates"`
				Key     string                     `json:"key"`
				Value   json.RawMessage            `json:"value"`
			}
			if err := sonic.UnmarshalString(argumentsJSON, &envelope); err != nil {
				slog.Warn("var_set parse arguments failed", "err", err)
				return "", fmt.Errorf("parse var_set arguments: %w", err)
			}

			if len(envelope.Updates) > 0 {
				keys := make([]string, 0, len(envelope.Updates))
				for k := range envelope.Updates {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					raw := envelope.Updates[k]
					if k == "" {
						return "", fmt.Errorf("%w: empty key in updates", lferrors.ErrInvalidRequest)
					}
					val, err := parseVarSetJSONValue(raw)
					if err != nil {
						slog.Warn("var_set parse value failed", "key", k, "err", err)
						return "", fmt.Errorf("parse var_set value for %q: %w", k, err)
					}
					slog.Info("var_set applying", "key", k, "value_type", fmt.Sprintf("%T", val))
					if err := store.AgentSet(k, val); err != nil {
						slog.Warn("var_set AgentSet rejected", "key", k, "err", err)
						return "", err
					}
					slog.Info("var_set ok", "key", k)
				}
				return "ok", nil
			}

			if envelope.Key == "" {
				return "", fmt.Errorf("%w: provide non-empty `updates` or legacy `key`", lferrors.ErrInvalidRequest)
			}
			val, err := parseVarSetJSONValue(envelope.Value)
			if err != nil {
				slog.Warn("var_set parse value failed", "key", envelope.Key, "err", err)
				return "", fmt.Errorf("parse var_set value: %w", err)
			}
			slog.Info("var_set applying", "key", envelope.Key, "value_type", fmt.Sprintf("%T", val))
			if err := store.AgentSet(envelope.Key, val); err != nil {
				slog.Warn("var_set AgentSet rejected", "key", envelope.Key, "err", err)
				return "", err
			}
			slog.Info("var_set ok", "key", envelope.Key)
			return "ok", nil
		},
	}
}

func parseVarSetJSONValue(raw json.RawMessage) (any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var val any
	if err := sonic.Unmarshal(raw, &val); err != nil {
		return nil, err
	}
	return val, nil
}
