package metrics

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tasohower/rei/core"
	"github.com/tasohower/rei/events"
)

func TestRecorderAggregatesAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics.jsonl")
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	rec := &Recorder{Store: store, SessionID: "s1"}

	base := events.Base{RunID: "r1", Time: time.Now()}
	rec.Handle(context.Background(), &events.LLMEnd{
		Base: base, Model: "m", RequestID: "req-1",
		Usage: core.Usage{Input: 100, CachedInput: 40, Output: 20, Cost: core.Cost{Input: 300, Output: 200}},
	})
	rec.Handle(context.Background(), &events.CompressEnd{
		Base: base, MessagesBefore: 10, MessagesAfter: 3,
		Usage: core.Usage{Input: 50, Output: 10},
	})
	// 其他事件应被忽略。
	rec.Handle(context.Background(), &events.ContentChunk{Base: base, Text: "x"})

	sum := store.Aggregate("s1")
	if sum.Calls != 2 || sum.Input != 150 || sum.Output != 30 || sum.Cached != 40 {
		t.Fatalf("聚合 = %+v", sum)
	}
	if sum.CostMicro != 500 {
		t.Fatalf("成本 = %d", sum.CostMicro)
	}
	if other := store.Aggregate("s2"); other.Calls != 0 {
		t.Fatalf("其它会话不应命中：%+v", other)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	lines := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if len(scanner.Text()) > 0 {
			lines++
		}
	}
	if lines != 2 {
		t.Fatalf("账本行数 = %d，期望 2", lines)
	}
}

func TestStoreWithoutPathKeepsInMemory(t *testing.T) {
	store, err := NewStore("")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Record(UsageToRecord("r1", "m", "llm", core.Usage{Input: 1, Output: 2})); err != nil {
		t.Fatal(err)
	}
	if got := len(store.Records()); got != 1 {
		t.Fatalf("记录数 = %d", got)
	}
	if sum := store.Aggregate(""); sum.Calls != 1 {
		t.Fatalf("聚合 = %+v", sum)
	}
}
