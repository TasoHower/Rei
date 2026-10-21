package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tasohower/rei/core"
	"github.com/tasohower/rei/tools"
)

func newEngine(t *testing.T) (*tools.Engine, string) {
	t.Helper()
	root := t.TempDir()
	eng := tools.NewEngine()
	if err := RegisterWorkspace(eng, root); err != nil {
		t.Fatalf("注册工区工具失败：%v", err)
	}
	return eng, root
}

func run(t *testing.T, eng *tools.Engine, name, args string) tools.Result {
	t.Helper()
	res := eng.Execute(context.Background(), []core.ToolCall{{ID: "c1", Name: name, Arguments: args}})
	if len(res) != 1 {
		t.Fatalf("执行结果数 = %d", len(res))
	}
	return res[0].Result
}

func TestNewWorkspaceRejectsBadRoot(t *testing.T) {
	if _, err := NewWorkspace(filepath.Join(t.TempDir(), "不存在")); err == nil {
		t.Fatal("不存在的工区应当报错")
	}
	file := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewWorkspace(file); err == nil {
		t.Fatal("文件路径不应被当作工区")
	}
}

func TestWriteReadRoundTrip(t *testing.T) {
	eng, root := newEngine(t)

	if res := run(t, eng, "write_file", `{"path":"a/b.txt","content":"你好，Rei"}`); res.IsError {
		t.Fatalf("写入失败：%s", res.Content)
	}
	got, err := os.ReadFile(filepath.Join(root, "a", "b.txt"))
	if err != nil || string(got) != "你好，Rei" {
		t.Fatalf("落盘内容 = %q, err = %v", got, err)
	}

	res := run(t, eng, "read_file", `{"path":"a/b.txt"}`)
	if res.IsError || res.Content != "你好，Rei" {
		t.Fatalf("读取结果 = %+v", res)
	}
}

func TestReadRejectsPathEscape(t *testing.T) {
	eng, _ := newEngine(t)
	res := run(t, eng, "read_file", `{"path":"../../etc/passwd"}`)
	if !res.IsError || !strings.Contains(res.Content, "escapes the workspace") {
		t.Fatalf("越界路径应被拒绝：%+v", res)
	}
}

func TestEditFileReplacesAll(t *testing.T) {
	eng, root := newEngine(t)
	if err := os.WriteFile(filepath.Join(root, "x.txt"), []byte("a b a"), 0o600); err != nil {
		t.Fatal(err)
	}
	res := run(t, eng, "edit_file", `{"path":"x.txt","old_text":"a","new_text":"c"}`)
	if res.IsError || !strings.Contains(res.Content, "2 occurrence") {
		t.Fatalf("替换结果 = %+v", res)
	}
	data, _ := os.ReadFile(filepath.Join(root, "x.txt"))
	if string(data) != "c b c" {
		t.Fatalf("文件内容 = %q", data)
	}

	missing := run(t, eng, "edit_file", `{"path":"x.txt","old_text":"zzz","new_text":"y"}`)
	if !missing.IsError || !strings.Contains(missing.Content, "not found") {
		t.Fatalf("找不到原文应报错：%+v", missing)
	}
}

func TestGlobAndGrep(t *testing.T) {
	eng, root := newEngine(t)
	for _, p := range []string{"a.txt", "sub/b.txt", "sub/c.go"} {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("关键字 hello\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	res := run(t, eng, "glob", `{"pattern":"**/*.txt"}`)
	if res.IsError || !strings.Contains(res.Content, "a.txt") || !strings.Contains(res.Content, "sub/b.txt") {
		t.Fatalf("glob 结果 = %+v", res)
	}
	if strings.Contains(res.Content, "c.go") {
		t.Fatalf("glob 不应匹配 .go：%+v", res)
	}

	res = run(t, eng, "grep", `{"pattern":"关键字"}`)
	if res.IsError || !strings.Contains(res.Content, "a.txt:1") {
		t.Fatalf("grep 结果 = %+v", res)
	}
}

func TestBash(t *testing.T) {
	eng, _ := newEngine(t)

	res := run(t, eng, "bash", `{"command":"echo hello"}`)
	if res.IsError || strings.TrimSpace(res.Content) != "hello" {
		t.Fatalf("bash 输出 = %+v", res)
	}

	res = run(t, eng, "bash", `{"command":"exit 3"}`)
	if !res.IsError || !strings.Contains(res.Content, "exit 3") {
		t.Fatalf("非零退出码应标记为错误：%+v", res)
	}

	res = run(t, eng, "bash", `{"command":"rm -rf /"}`)
	if !res.IsError || !strings.Contains(res.Content, "refused") {
		t.Fatalf("破坏性命令应被拒绝：%+v", res)
	}
}

func TestBashEnvironmentIsIsolated(t *testing.T) {
	t.Setenv("REI_SECRET", "不该泄漏")
	eng, _ := newEngine(t)
	res := run(t, eng, "bash", `{"command":"echo ${REI_SECRET:-未设置}"}`)
	if res.IsError || strings.TrimSpace(res.Content) != "未设置" {
		t.Fatalf("子进程不应继承未白名单的环境变量：%+v", res)
	}
}
