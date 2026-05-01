package variable

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"

	lferrors "github.com/TasoHower/Rei/loopForge/pkg/errors"
	"github.com/TasoHower/Rei/loopForge/pkg/model"
	"github.com/TasoHower/Rei/loopForge/pkg/tool/autoreg"
)

const varSetToolName = "var_set"

type varSetParams struct {
	Updates map[string]interface{} `json:"updates,omitempty" description:"Map from variable name to JSON value. Include only variables you are changing; leave others out."`
	Key     string                 `json:"key,omitempty"    description:"Legacy: single variable name (use updates for multiple keys)."`
	Value   json.RawMessage        `json:"value,omitempty"  description:"Legacy: JSON object value for key (ignored when updates is non-empty)."`
}

// VarSetTool returns a ToolInfo for LLM-driven variable assignment (const_ keys rejected).
func VarSetTool(store *VarStore) *model.ToolInfo {
	return autoreg.NewToolFromStruct(varSetToolName,
		"Set one or more shared variables in a single call. "+
			"Pass only keys you need to change inside `updates`; omit variables that stay the same. "+
			"Keys prefixed with const_ are read-only and cannot be set here. "+
			"Legacy single-field form `key` + `value` is still accepted.",
		func(ctx context.Context, p varSetParams) (string, error) {
			slog.Debug("var_set begin", "updates_len", len(p.Updates), "key", p.Key)
			if store == nil {
				s := FromContext(ctx)
				if s == nil {
					slog.Error("var_set failed", "reason", "no store in context or closure")
					return "", fmt.Errorf("%w", lferrors.ErrNoStore)
				}
				store = s
			}

			if len(p.Updates) > 0 {
				keys := make([]string, 0, len(p.Updates))
				for k := range p.Updates {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					val := p.Updates[k]
					if k == "" {
						return "", fmt.Errorf("%w: empty key in updates", lferrors.ErrInvalidRequest)
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

			if p.Key == "" {
				return "", fmt.Errorf("%w: provide non-empty `updates` or legacy `key`", lferrors.ErrInvalidRequest)
			}
			val, err := parseVarSetJSONValue(p.Value)
			if err != nil {
				slog.Warn("var_set parse value failed", "key", p.Key, "err", err)
				return "", fmt.Errorf("parse var_set value: %w", err)
			}
			slog.Info("var_set applying", "key", p.Key, "value_type", fmt.Sprintf("%T", val))
			if err := store.AgentSet(p.Key, val); err != nil {
				slog.Warn("var_set AgentSet rejected", "key", p.Key, "err", err)
				return "", err
			}
			slog.Info("var_set ok", "key", p.Key)
			return "ok", nil
		},
	)
}

func parseVarSetJSONValue(raw json.RawMessage) (any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var val any
	if err := json.Unmarshal(raw, &val); err != nil {
		return nil, err
	}
	return val, nil
}
