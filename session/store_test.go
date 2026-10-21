package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tasohower/rei/core"
)

func testSnapshot(id string) Snapshot {
	return Snapshot{
		SessionID: id,
		Messages: []core.Message{
			core.NewUserTextMessage("你好"),
			core.NewAssistantMessage([]core.Content{{Type: core.ContentTypeText, Content: "在"}}, "", nil),
		},
		Usage: core.Usage{Input: 10, Output: 5},
	}
}

func TestMemoryStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	if snap, err := store.Load(ctx, "不存在"); err != nil || len(snap.Messages) != 0 {
		t.Fatalf("冷启动应返回空快照：%+v, %v", snap, err)
	}

	if err := store.Save(ctx, testSnapshot("s1")); err != nil {
		t.Fatalf("保存失败：%v", err)
	}
	snap, err := store.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("读取失败：%v", err)
	}
	if snap.Revision != 1 || len(snap.Messages) != 2 || snap.Checksum == "" {
		t.Fatalf("快照内容异常：%+v", snap)
	}
}

func TestRevisionIsMonotonicAndIdempotent(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	snap := testSnapshot("s1")
	_ = store.Save(ctx, snap)
	// 同 revision、同内容：幂等，不报错也不推进。
	if err := store.Save(ctx, snap); err != nil {
		t.Fatalf("幂等重放不应报错：%v", err)
	}
	got, _ := store.Load(ctx, "s1")
	if got.Revision != 1 {
		t.Fatalf("幂等重放后 revision = %d", got.Revision)
	}

	// 同 revision、不同内容：自动推进。
	snap.Messages = append(snap.Messages, core.NewUserTextMessage("继续"))
	if err := store.Save(ctx, snap); err != nil {
		t.Fatal(err)
	}
	got, _ = store.Load(ctx, "s1")
	if got.Revision != 2 {
		t.Fatalf("内容变化后 revision = %d，期望 2", got.Revision)
	}
}

func TestFileStoreRoundTripAndPermissions(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "sessions")
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("创建文件存储失败：%v", err)
	}

	if err := store.Save(ctx, testSnapshot("abc")); err != nil {
		t.Fatalf("保存失败：%v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "abc.json"))
	if err != nil {
		t.Fatalf("快照文件不存在：%v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("快照权限 = %o，期望 600", perm)
	}

	snap, err := store.Load(ctx, "abc")
	if err != nil || len(snap.Messages) != 2 {
		t.Fatalf("读取失败：%+v, %v", snap, err)
	}

	// 不允许残留临时文件。
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "abc.json" {
			t.Fatalf("残留文件：%s", e.Name())
		}
	}
}

func TestFileStoreDetectsTampering(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, testSnapshot("s1")); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "s1.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(string(raw), "你好", "他好", 1)
	if err := os.WriteFile(path, []byte(tampered), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Load(ctx, "s1"); !errors.Is(err, ErrChecksum) {
		t.Fatalf("篡改后应报告校验和错误，得到 %v", err)
	}
}
