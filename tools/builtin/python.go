package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"

	"github.com/tasohower/rei/core"
	"github.com/tasohower/rei/tools"
)

// runPythonTool 在工区内执行一段 Python 代码。
//
// 本实现直接用系统的 python3：ADK 不负责解释器分发，
// 宿主需要时自己装一个带依赖的环境。
type runPythonTool struct{ w *workspace }

func (t *runPythonTool) Name() string { return "run_python" }

func (t *runPythonTool) Description() string {
	return "Run a short Python 3 snippet inside the workspace and return its combined output."
}

func (t *runPythonTool) CanParallel() bool { return false }

func (t *runPythonTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"code":{"type":"string","description":"Python source, executed with python3 -c."}},"required":["code"]}`)
}

func (t *runPythonTool) Call(ctx context.Context, call core.ToolCall) tools.Result {
	var args struct {
		Code string `json:"code"`
	}
	if err := decode(call, &args); err != nil {
		return tools.ErrorResult("%v", err)
	}
	if strings.TrimSpace(args.Code) == "" {
		return tools.ErrorResult("code is required")
	}

	c := exec.CommandContext(ctx, "python3", "-c", args.Code)
	c.Dir = t.w.root
	c.Env = isolatedEnv()
	out, err := c.CombinedOutput()
	text := strings.TrimRight(string(out), "\n")
	if err != nil {
		if ctx.Err() != nil {
			return tools.ErrorResult("python canceled or timed out: %v", ctx.Err())
		}
		if errors.Is(err, exec.ErrNotFound) {
			return tools.ErrorResult("python3 is not installed on this host")
		}
		if text == "" {
			text = err.Error()
		}
		return tools.Result{Content: text + "\n(exit non-zero)", IsError: true}
	}
	if text == "" {
		text = "(no output)"
	}
	return tools.Result{Content: text}
}
