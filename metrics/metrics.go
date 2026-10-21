// Package metrics 提供跨会话的调用账本：每次模型调用一行，可落盘为 JSONL。
//
// 它与 trace 的分工：trace 回答「这次运行慢在哪」，metrics 回答「这个月花了多少」。
package metrics

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/tasohower/rei/core"
	"github.com/tasohower/rei/events"
)

// Record 是一次模型调用。
type Record struct {
	Time        time.Time `json:"time"`
	SessionID   string    `json:"session_id,omitempty"`
	RunID       string    `json:"run_id,omitempty"`
	RequestID   string    `json:"request_id,omitempty"`
	Model       string    `json:"model,omitempty"`
	Input       int       `json:"input"`
	CachedInput int       `json:"cached_input,omitempty"`
	Output      int       `json:"output"`
	Reasoning   int       `json:"reasoning,omitempty"`
	CostMicro   int64     `json:"cost_micro"`
	Kind        string    `json:"kind"` // llm / compress / tool
}

// Summary 是按会话或全局聚合的结果。
type Summary struct {
	Calls     int   `json:"calls"`
	Input     int   `json:"input"`
	Output    int   `json:"output"`
	Cached    int   `json:"cached_input"`
	Reasoning int   `json:"reasoning"`
	CostMicro int64 `json:"cost_micro"`
}

// Add 把一条记录并入汇总。
func (s *Summary) Add(r Record) {
	s.Calls++
	s.Input += r.Input
	s.Output += r.Output
	s.Cached += r.CachedInput
	s.Reasoning += r.Reasoning
	s.CostMicro += r.CostMicro
}

// Store 是账本。path 为空时只在内存里保留。
type Store struct {
	mu      sync.Mutex
	path    string
	records []Record
}

// NewStore 创建账本；path 非空时每次 Record 追加一行（0600）。
func NewStore(path string) (*Store, error) {
	if path != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
	}
	return &Store{path: path}, nil
}

// Record 追加一条记录。
func (s *Store) Record(r Record) error {
	if r.Time.IsZero() {
		r.Time = time.Now()
	}
	s.mu.Lock()
	s.records = append(s.records, r)
	path := s.path
	s.mu.Unlock()

	if path == "" {
		return nil
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.Write(append(raw, '\n'))
	return err
}

// Aggregate 汇总账本；sessionID 为空表示全部。
func (s *Store) Aggregate(sessionID string) Summary {
	s.mu.Lock()
	defer s.mu.Unlock()
	var sum Summary
	for _, r := range s.records {
		if sessionID != "" && r.SessionID != sessionID {
			continue
		}
		sum.Add(r)
	}
	return sum
}

// Records 返回记录副本。
func (s *Store) Records() []Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Record(nil), s.records...)
}

// Recorder 是一个把事件写进账本的 EventHandler。
type Recorder struct {
	Store     *Store
	SessionID string
}

// Handle 实现 events.EventHandler：只关心 LLMEnd 与压缩用量，其它事件忽略。
func (r *Recorder) Handle(_ context.Context, e events.Event) {
	if r == nil || r.Store == nil {
		return
	}
	switch ev := e.(type) {
	case *events.LLMEnd:
		_ = r.Store.Record(Record{
			Time: ev.Time, SessionID: r.SessionID, RunID: ev.RunID,
			RequestID: ev.RequestID, Model: ev.Model,
			Input: ev.Usage.Input, CachedInput: ev.Usage.CachedInput,
			Output: ev.Usage.Output, Reasoning: ev.Usage.Reasoning,
			CostMicro: ev.Usage.Cost.Total(), Kind: "llm",
		})
	case *events.CompressEnd:
		_ = r.Store.Record(Record{
			Time: ev.Time, SessionID: r.SessionID, RunID: ev.RunID,
			Input: ev.Usage.Input, Output: ev.Usage.Output,
			CostMicro: ev.Usage.Cost.Total(), Kind: "compress",
		})
	}
}

var _ events.EventHandler = (&Recorder{}).Handle

// UsageToRecord 便于宿主把 core.Usage 直接写进账本。
func UsageToRecord(runID, model, kind string, u core.Usage) Record {
	return Record{
		Time: time.Now(), RunID: runID, Model: model, Kind: kind,
		Input: u.Input, CachedInput: u.CachedInput, Output: u.Output,
		Reasoning: u.Reasoning, CostMicro: u.Cost.Total(),
	}
}
