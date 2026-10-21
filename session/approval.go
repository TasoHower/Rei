package session

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/tasohower/rei/events"
)

// 审批相关错误。
var (
	// ErrApprovalNotFound 表示没有这个 call id 的待审批项。
	ErrApprovalNotFound = errors.New("session: 找不到该待审批项")
	// ErrApprovalNotHead 表示只有队首的审批可以被决定。
	ErrApprovalNotHead = errors.New("session: 只能决定队首的审批")
	// ErrApprovalTimeout 表示人类在超时前没有应答，按拒绝处理。
	ErrApprovalTimeout = errors.New("session: 审批超时，已按拒绝处理")
)

// 默认审批超时。
const DefaultApprovalTimeout = 30 * time.Minute

// approvalQueue 是单一会话内的审批 FIFO。
//
// 为什么是 FIFO 且只允许决定队首：主 agent 与后台任务共享同一个队列，
// 如果允许乱序决定，人类看到的顺序和实际放行的顺序会不一致，审计就失去意义。
type approvalQueue struct {
	mu      sync.Mutex
	entries []*approvalEntry
	timeout time.Duration
	closed  bool
}

type approvalEntry struct {
	req events.ApprovalRequest
	ch  chan events.Decision
}

func newApprovalQueue(timeout time.Duration) *approvalQueue {
	if timeout <= 0 {
		timeout = DefaultApprovalTimeout
	}
	return &approvalQueue{timeout: timeout}
}

// Approve 实现 events.Approver：入队后阻塞，直到被解决、超时或 ctx 结束。
func (q *approvalQueue) Approve(ctx context.Context, req events.ApprovalRequest) (events.Decision, error) {
	entry := &approvalEntry{req: req, ch: make(chan events.Decision, 1)}

	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return events.Decision{Reason: "会话已关闭"}, errors.New("session: 会话已关闭")
	}
	q.entries = append(q.entries, entry)
	q.mu.Unlock()

	timer := time.NewTimer(q.timeout)
	defer timer.Stop()

	select {
	case d := <-entry.ch:
		return d, nil
	case <-timer.C:
		q.remove(entry)
		return events.Decision{Reason: ErrApprovalTimeout.Error()}, ErrApprovalTimeout
	case <-ctx.Done():
		q.remove(entry)
		return events.Decision{Reason: "已取消"}, ctx.Err()
	}
}

// Resolve 用 callID 解决队首审批。非队首返回 ErrApprovalNotHead。
func (q *approvalQueue) Resolve(callID string, decision events.Decision) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.entries) == 0 {
		return ErrApprovalNotFound
	}
	head := q.entries[0]
	if head.req.CallID != callID {
		for _, e := range q.entries[1:] {
			if e.req.CallID == callID {
				return ErrApprovalNotHead
			}
		}
		return ErrApprovalNotFound
	}
	q.entries = q.entries[1:]
	head.ch <- decision
	return nil
}

// Pending 返回待审批的调用 id（按队列顺序）。
func (q *approvalQueue) Pending() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]string, 0, len(q.entries))
	for _, e := range q.entries {
		out = append(out, e.req.CallID)
	}
	return out
}

// close 把所有等待中的审批收敛为拒绝。
func (q *approvalQueue) close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	q.closed = true
	for _, e := range q.entries {
		e.ch <- events.Decision{Reason: "会话已关闭"}
	}
	q.entries = nil
}

func (q *approvalQueue) remove(target *approvalEntry) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i, e := range q.entries {
		if e == target {
			q.entries = append(q.entries[:i], q.entries[i+1:]...)
			return
		}
	}
}
