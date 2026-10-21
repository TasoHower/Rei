// Package todo 提供会话级待办：它随检查点落盘、不参与压缩，
// 因此在长会话里始终可见（这是它和「模型自己写在正文里的计划」的关键区别）。
package todo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/tasohower/rei/core"
	"github.com/tasohower/rei/tools"
)

// 状态。
const (
	StatusPending    = "pending"
	StatusInProgress = "in_progress"
	StatusDone       = "done"
)

// Item 是一条待办。
type Item struct {
	ID     int    `json:"id"`
	Text   string `json:"text"`
	Status string `json:"status"`
}

// 错误。
var (
	ErrEmptyText = errors.New("todo: 内容不能为空")
	ErrNotFound  = errors.New("todo: 待办不存在")
	ErrBadStatus = errors.New("todo: 非法状态")
)

// Store 是待办存储。
type Store struct {
	mu    sync.Mutex
	next  int
	items []Item
}

// NewStore 构造存储。
func NewStore() *Store { return &Store{} }

// Add 追加一条待办。
func (s *Store) Add(text string) (Item, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Item{}, ErrEmptyText
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	item := Item{ID: s.next, Text: text, Status: StatusPending}
	s.items = append(s.items, item)
	return item, nil
}

// Update 修改状态。
func (s *Store) Update(id int, status string) (Item, error) {
	switch status {
	case StatusPending, StatusInProgress, StatusDone:
	default:
		return Item{}, ErrBadStatus
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.items {
		if s.items[i].ID == id {
			s.items[i].Status = status
			return s.items[i], nil
		}
	}
	return Item{}, ErrNotFound
}

// List 返回全部待办副本。
func (s *Store) List() []Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Item(nil), s.items...)
}

// Restore 用快照恢复（兼容旧快照：next 取最大 id）。
func (s *Store) Restore(items []Item) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = append([]Item(nil), items...)
	s.next = 0
	for _, it := range items {
		if it.ID > s.next {
			s.next = it.ID
		}
	}
}

// PromptFragment 渲染成英文系统提示片段。
func (s *Store) PromptFragment() string {
	items := s.List()
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Current todo list:\n")
	for _, it := range items {
		fmt.Fprintf(&b, "%d. [%s] %s\n", it.ID, it.Status, it.Text)
	}
	b.WriteString("Keep it updated: mark items done only after you verified them.\n")
	return b.String()
}

// ---------------------------------------------------------------- 工具

// Tools 返回 todo_add / todo_update / todo_list。
func Tools(store *Store) []tools.Tool {
	return []tools.Tool{&addTool{store}, &updateTool{store}, &listTool{store}}
}

// Register 注册待办工具。
func Register(eng *tools.Engine, store *Store) error {
	for _, t := range Tools(store) {
		if err := eng.Register(t); err != nil {
			return err
		}
	}
	return nil
}

type addTool struct{ store *Store }

func (t *addTool) Name() string        { return "todo_add" }
func (t *addTool) Description() string { return "Add one item to the session todo list." }
func (t *addTool) CanParallel() bool   { return false }
func (t *addTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`)
}

func (t *addTool) Call(_ context.Context, call core.ToolCall) tools.Result {
	var args struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(defaultJSON(call.Arguments)), &args); err != nil {
		return tools.ErrorResult("invalid arguments: %v", err)
	}
	item, err := t.store.Add(args.Text)
	if err != nil {
		return tools.ErrorResult("%v", err)
	}
	raw, _ := json.Marshal(item)
	return tools.Result{Content: string(raw)}
}

type updateTool struct{ store *Store }

func (t *updateTool) Name() string { return "todo_update" }
func (t *updateTool) Description() string {
	return "Update a todo item status: pending, in_progress or done."
}
func (t *updateTool) CanParallel() bool { return false }
func (t *updateTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"id":{"type":"integer"},"status":{"type":"string","enum":["pending","in_progress","done"]}},"required":["id","status"]}`)
}

func (t *updateTool) Call(_ context.Context, call core.ToolCall) tools.Result {
	var args struct {
		ID     int    `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(defaultJSON(call.Arguments)), &args); err != nil {
		return tools.ErrorResult("invalid arguments: %v", err)
	}
	item, err := t.store.Update(args.ID, args.Status)
	if err != nil {
		return tools.ErrorResult("%v", err)
	}
	raw, _ := json.Marshal(item)
	return tools.Result{Content: string(raw)}
}

type listTool struct{ store *Store }

func (t *listTool) Name() string        { return "todo_list" }
func (t *listTool) Description() string { return "List the session todo items." }
func (t *listTool) CanParallel() bool   { return true }
func (t *listTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}

func (t *listTool) Call(context.Context, core.ToolCall) tools.Result {
	raw, _ := json.Marshal(t.store.List())
	return tools.Result{Content: string(raw)}
}

func defaultJSON(s string) string {
	if strings.TrimSpace(s) == "" {
		return "{}"
	}
	return s
}
