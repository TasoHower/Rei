package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/tasohower/rei/core"
	"github.com/tasohower/rei/tools"
)

// bashTool 在工区内执行 shell 命令。
//
// 它不是沙箱，只提供沙箱最便宜、最承重的那一半：明显破坏性命令的静态拒绝、
// 由引擎超时保证的进程组终止，以及最小环境变量（provider key 不会泄漏给子进程）。
//
// 工具描述与输出文案属于「发给模型的 prompt」，使用英文。
type bashTool struct{ w *workspace }

func (t *bashTool) Name() string { return "bash" }
func (t *bashTool) Description() string {
	return "Run a shell command in the workspace and return its combined output."
}
func (t *bashTool) CanParallel() bool { return false }
func (t *bashTool) Parameters() json.RawMessage {
	return schema(map[string]any{
		"command": map[string]any{"type": "string", "description": "Shell command, executed with sh -c."},
	}, "command")
}

// dangerous 在命令执行前拒绝。它针对的是「在 agent 循环里几乎总是误操作」的模式，
// 是一道护栏而不是安全边界（不可信代码请放进真正的沙箱）。
var dangerous = []*regexp.Regexp{
	regexp.MustCompile(`\brm\s+(-[a-zA-Z]*[rf][a-zA-Z]*\s+)+(/|~|\$HOME)\s*$`),
	regexp.MustCompile(`\bmkfs(\.[a-z0-9]+)?\b`),
	regexp.MustCompile(`\bdd\s+.*\bof=/dev/`),
	regexp.MustCompile(`>\s*/dev/(sd|disk|nvme)`),
	regexp.MustCompile(`\bsudo\b`),
	regexp.MustCompile(`:\(\)\s*\{\s*:\|:&\s*\};:`),
	regexp.MustCompile(`\bchmod\s+-R\s+777\s+/\s*$`),
	regexp.MustCompile(`\bgit\s+push\b.*\s--force\b`),
}

// allowedEnv 是交给子进程的环境变量白名单。
var allowedEnv = []string{"PATH", "HOME", "LANG", "LC_ALL", "TERM", "TMPDIR", "SHELL"}

func (t *bashTool) Call(ctx context.Context, call core.ToolCall) tools.Result {
	var args struct {
		Command string `json:"command"`
	}
	if err := decode(call, &args); err != nil {
		return tools.ErrorResult("%v", err)
	}
	cmd := strings.TrimSpace(args.Command)
	if cmd == "" {
		return tools.ErrorResult("command is required")
	}
	for _, re := range dangerous {
		if re.MatchString(cmd) {
			return tools.ErrorResult("refused: command matches destructive pattern %q; run it manually if this is really intended", re.String())
		}
	}

	c := exec.CommandContext(ctx, "sh", "-c", cmd)
	c.Dir = t.w.root
	c.Env = isolatedEnv()
	out, err := c.CombinedOutput()
	text := string(out)
	exit := 0
	if err != nil {
		if ctx.Err() != nil {
			return tools.ErrorResult("command canceled or timed out: %v", ctx.Err())
		}
		if ee, ok := err.(*exec.ExitError); ok {
			exit = ee.ExitCode()
		} else {
			return tools.ErrorResult("run command: %v", err)
		}
	}
	if text == "" {
		text = fmt.Sprintf("(no output, exit %d)", exit)
	}
	if exit != 0 {
		return tools.Result{
			Content:  fmt.Sprintf("%s\n(exit %d)", strings.TrimRight(text, "\n"), exit),
			IsError:  true,
			Metadata: map[string]any{"exit_code": exit},
		}
	}
	return tools.Result{Content: strings.TrimRight(text, "\n"), Metadata: map[string]any{"exit_code": 0}}
}

func isolatedEnv() []string {
	var env []string
	for _, key := range allowedEnv {
		if v, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+v)
		}
	}
	if len(env) == 0 {
		env = []string{"PATH=/usr/local/bin:/usr/bin:/bin"}
	}
	return env
}
