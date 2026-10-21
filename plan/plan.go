// Package plan 实现 Plan 模式的最小状态机与工具：
// 模型提交计划、执行中更新步骤状态、宿主把计划注入下一轮上下文。
//
// 设计取舍：只保留「提交计划 + 更新步骤」两个工具，但显式提供 plan_* 事件
// 与可查询的 Store，让宿主（审批界面、HUD）不必从工具参数里反推计划。
package plan

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// 步骤状态。
const (
	StatusPending    = "pending"
	StatusInProgress = "in_progress"
	StatusCompleted  = "completed"
	StatusFailed     = "failed"
	StatusSkipped    = "skipped"
)

// 约束。
const (
	MaxSteps    = 20
	MaxPlanSize = 20_000
)

// 错误。
var (
	ErrEmptyPlan  = errors.New("plan: 计划正文不能为空")
	ErrNoSteps    = errors.New("plan: 计划至少需要一个步骤")
	ErrTooMany    = errors.New("plan: 步骤过多")
	ErrBadIndex   = errors.New("plan: 步骤序号不存在")
	ErrBadStatus  = errors.New("plan: 非法的步骤状态")
	ErrNotStarted = errors.New("plan: 计划尚未提交")
)

// Step 是计划中的一个步骤。
type Step struct {
	Index  int    `json:"index"`
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
	Status string `json:"status"`
	Note   string `json:"note,omitempty"`
}

// Plan 是一份计划的快照。
type Plan struct {
	Text     string    `json:"text"`
	Steps    []Step    `json:"steps"`
	Revision int       `json:"revision"`
	Updated  time.Time `json:"updated"`
}

// Clone 返回深拷贝，避免调用方改到内部状态。
func (p Plan) Clone() Plan {
	out := p
	out.Steps = append([]Step(nil), p.Steps...)
	return out
}

// Store 保存当前计划。它是进程内状态：跨进程恢复由宿主的会话存储负责，
// 「计划不单独落盘、靠上下文存活」是这里的取舍。
type Store struct {
	mu   sync.RWMutex
	plan Plan
}

// NewStore 构造计划存储。
func NewStore() *Store { return &Store{} }

// Submit 提交一份新计划并重置全部步骤状态。
func (s *Store) Submit(_ context.Context, text string, steps []Step) (Plan, error) {
	if strings.TrimSpace(text) == "" {
		return Plan{}, ErrEmptyPlan
	}
	if len([]rune(text)) > MaxPlanSize {
		return Plan{}, fmt.Errorf("plan: 计划正文超过 %d 字符", MaxPlanSize)
	}
	if len(steps) == 0 {
		return Plan{}, ErrNoSteps
	}
	if len(steps) > MaxSteps {
		return Plan{}, ErrTooMany
	}

	normalized := make([]Step, 0, len(steps))
	for i, step := range steps {
		title := strings.TrimSpace(step.Title)
		if title == "" {
			return Plan{}, fmt.Errorf("plan: 第 %d 个步骤缺少标题", i+1)
		}
		normalized = append(normalized, Step{
			Index:  i + 1,
			Title:  title,
			Detail: strings.TrimSpace(step.Detail),
			Status: StatusPending,
		})
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.plan = Plan{Text: strings.TrimSpace(text), Steps: normalized, Revision: s.plan.Revision + 1, Updated: time.Now()}
	return s.plan.Clone(), nil
}

// Start 把某一步标记为进行中。
func (s *Store) Start(_ context.Context, index int) (Plan, error) {
	return s.transition(index, StatusInProgress, "")
}

// Complete 把某一步标记为终态（completed / failed / skipped）。
func (s *Store) Complete(_ context.Context, index int, status, note string) (Plan, error) {
	switch status {
	case StatusCompleted, StatusFailed, StatusSkipped:
	default:
		return Plan{}, ErrBadStatus
	}
	return s.transition(index, status, note)
}

func (s *Store) transition(index int, status, note string) (Plan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.plan.Steps) == 0 {
		return Plan{}, ErrNotStarted
	}
	for i := range s.plan.Steps {
		if s.plan.Steps[i].Index != index {
			continue
		}
		s.plan.Steps[i].Status = status
		if note != "" {
			s.plan.Steps[i].Note = note
		}
		s.plan.Revision++
		s.plan.Updated = time.Now()
		return s.plan.Clone(), nil
	}
	return Plan{}, ErrBadIndex
}

// Snapshot 返回当前计划。
func (s *Store) Snapshot() Plan {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.plan.Clone()
}

// PromptFragment 把计划渲染成注入系统提示的英文片段。
//
// 放在系统提示最尾部而不是对话历史里：历史会被压缩，计划不该被摘要掉。
func (s *Store) PromptFragment() string {
	plan := s.Snapshot()
	if len(plan.Steps) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Current plan:\n")
	for _, step := range plan.Steps {
		fmt.Fprintf(&b, "%d. [%s] %s", step.Index, step.Status, step.Title)
		if step.Detail != "" {
			b.WriteString(" — ")
			b.WriteString(step.Detail)
		}
		if step.Note != "" {
			b.WriteString(" (")
			b.WriteString(step.Note)
			b.WriteString(")")
		}
		b.WriteString("\n")
	}
	b.WriteString("Keep this plan in sync with reality. Mark a step completed only after you verified the result.\n")
	return b.String()
}
