// Package session 提供长时运行的会话容器：输入队列、历史、用量、审批、
// 持久化与优雅关闭。
//
// 跨轮状态只有两个主人：Session 持有历史与账本，AgentLoop 持有不可变配置。
// 因此一个 Session 可以被宿主长期持有，而同一个 AgentLoop 可以被多个会话共享。
package session

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/tasohower/rei/agents"
	"github.com/tasohower/rei/core"
	"github.com/tasohower/rei/events"
)

// 会话错误。
var (
	// ErrClosed 表示会话已关闭。
	ErrClosed = errors.New("session: 会话已关闭")
	// ErrBusy 表示已有一次 Run 在跑（同一会话不允许并发运行）。
	ErrBusy = errors.New("session: 已有运行在进行中")
	// ErrQueueFull 表示输入队列已满（背压）。
	ErrQueueFull = errors.New("session: 输入队列已满")
)

// 默认输入队列长度。
const DefaultMaxQueue = 16

// Option 配置会话。
type Option func(*config)

type config struct {
	maxQueue        int
	store           Store
	approvalTimeout time.Duration
}

// WithMaxQueue 设置输入队列长度。
func WithMaxQueue(n int) Option {
	return func(c *config) {
		if n > 0 {
			c.maxQueue = n
		}
	}
}

// WithStore 设置持久化实现。
func WithStore(s Store) Option { return func(c *config) { c.store = s } }

// WithApprovalTimeout 设置审批超时。
func WithApprovalTimeout(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.approvalTimeout = d
		}
	}
}

// Session 是一个长时会话。
type Session struct {
	id     string
	loop   *agents.AgentLoop
	store  Store
	cfg    config
	approv *approvalQueue

	mu       sync.Mutex
	history  []core.Message
	inbox    []core.Message
	usage    core.Usage
	revision int64
	running  bool
	closed   bool
}

// New 创建会话。若 store 中已有快照，则按 checkpoint-first 语义恢复历史。
func New(id string, loop *agents.AgentLoop, opts ...Option) (*Session, error) {
	if id == "" {
		return nil, errors.New("session: id 不能为空")
	}
	if loop == nil {
		return nil, errors.New("session: AgentLoop 不能为空")
	}
	cfg := config{maxQueue: DefaultMaxQueue, approvalTimeout: DefaultApprovalTimeout}
	for _, opt := range opts {
		opt(&cfg)
	}
	s := &Session{
		id:     id,
		loop:   loop,
		store:  cfg.store,
		cfg:    cfg,
		approv: newApprovalQueue(cfg.approvalTimeout),
	}
	if err := s.restore(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

// restore 读取历史快照。中途失败会直接报错：带着坏历史继续跑比启动失败更糟。
func (s *Session) restore(ctx context.Context) error {
	if s.store == nil {
		return nil
	}
	snap, err := s.store.Load(ctx, s.id)
	if err != nil {
		return fmt.Errorf("session: 恢复 %s 失败：%w", s.id, err)
	}
	if len(snap.Messages) == 0 {
		return nil
	}
	if err := core.ValidateConversation(snap.Messages); err != nil {
		return fmt.Errorf("session: 快照历史非法：%w", err)
	}
	s.history = snap.Messages
	s.usage = snap.Usage
	s.revision = snap.Revision
	return nil
}

// ID 返回会话 id。
func (s *Session) ID() string { return s.id }

// Ask 把一条用户消息放进输入队列后立即返回（不阻塞）。
func (s *Session) Ask(ctx context.Context, msg core.Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := msg.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	if len(s.inbox) >= s.cfg.maxQueue {
		return ErrQueueFull
	}
	s.inbox = append(s.inbox, msg)
	return nil
}

// History 返回当前历史的副本。
func (s *Session) History() []core.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]core.Message(nil), s.history...)
}

// Usage 返回累计用量。
func (s *Session) Usage() core.Usage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.usage
}

// Run 消费输入队列并跑一轮。队列为空时直接返回 nil。
//
// 返回的 error 只代表运行失败；预算/轮次耗尽通过 AgentEnd 的结束原因表达。
func (s *Session) Run(ctx context.Context, handler events.EventHandler) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrClosed
	}
	if s.running {
		s.mu.Unlock()
		return ErrBusy
	}
	if len(s.inbox) == 0 {
		s.mu.Unlock()
		return nil
	}
	batch := s.inbox
	s.inbox = nil
	base := append(append([]core.Message(nil), s.history...), batch...)
	s.running = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	runID := fmt.Sprintf("%s-%d", s.id, time.Now().UnixNano())
	events.Emit(ctx, handler, &events.UserInputsConsumed{
		Base:  events.Base{RunID: runID, Time: time.Now()},
		Count: len(batch),
	})

	res, err := s.loop.RunStream(ctx, agents.RunRequest{
		SessionID: s.id,
		RunID:     runID,
		Messages:  base,
		Approver:  s,
	}, handler)
	if err != nil {
		events.Emit(ctx, handler, &events.SessionRunError{
			Base:    events.Base{RunID: runID, Time: time.Now()},
			Message: err.Error(),
		})
	}
	if res != nil {
		s.mu.Lock()
		s.history = res.Messages
		s.usage.Add(res.Usage)
		s.mu.Unlock()
		s.persist(ctx, handler, runID)
	}
	return err
}

// persist 提交检查点。失败不静默：发事件，由宿主决定是否中止。
func (s *Session) persist(ctx context.Context, handler events.EventHandler, runID string) {
	if s.store == nil {
		return
	}
	s.mu.Lock()
	snap := Snapshot{
		SessionID: s.id,
		Revision:  s.revision + 1,
		Messages:  append([]core.Message(nil), s.history...),
		Usage:     s.usage,
	}
	s.mu.Unlock()

	if err := s.store.Save(ctx, snap); err != nil {
		events.Emit(ctx, handler, &events.SessionPersistError{
			Base:    events.Base{RunID: runID, Time: time.Now()},
			Message: err.Error(),
		})
		return
	}
	s.mu.Lock()
	s.revision = snap.Revision
	s.mu.Unlock()
}

// Approve 实现 events.Approver：把请求放进 FIFO 并阻塞等待人类决定。
func (s *Session) Approve(ctx context.Context, req events.ApprovalRequest) (events.Decision, error) {
	return s.approv.Approve(ctx, req)
}

// ResolveApproval 由宿主调用，解决队首审批。
func (s *Session) ResolveApproval(callID string, approved bool, reason string) error {
	return s.approv.Resolve(callID, events.Decision{Approved: approved, Reason: reason})
}

// PendingApprovals 返回待审批的 call id。
func (s *Session) PendingApprovals() []string { return s.approv.Pending() }

// Shutdown 优雅关闭：拒绝新输入、收敛未决审批、发出 session_closed。
func (s *Session) Shutdown(ctx context.Context, handler events.EventHandler) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()

	s.approv.close()
	events.Emit(ctx, handler, &events.SessionClosed{Base: events.Base{Time: time.Now()}, Reason: "shutdown"})
	return nil
}
