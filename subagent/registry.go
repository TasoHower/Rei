package subagent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tasohower/rei/agents"
	"github.com/tasohower/rei/core"
	"github.com/tasohower/rei/events"
)

// 任务相关错误。
var (
	ErrTaskNotFound  = errors.New("subagent: 任务不存在")
	ErrTaskFinished  = errors.New("subagent: 任务已结束")
	ErrTaskRunning   = errors.New("subagent: 任务正在运行")
	ErrTaskInboxFull = errors.New("subagent: 任务收件箱已满")
	ErrRegistryClose = errors.New("subagent: 注册表已关闭")
	ErrDepthExceeded = errors.New("subagent: 超过子 agent 深度上限")
)

// 默认值。
const (
	DefaultTaskTimeout = 12 * time.Hour
	DefaultTaskMaxTurn = 8
	taskInboxSize      = 16
)

var seq atomic.Int64

func nextSeq() int64 { return seq.Add(1) }

// TaskSnapshot 是任务的只读视图。
type TaskSnapshot struct {
	ID          string     `json:"task_id"`
	Name        string     `json:"name"`
	ParentRunID string     `json:"parent_run_id,omitempty"`
	Status      string     `json:"status"`
	Result      string     `json:"result,omitempty"`
	Err         string     `json:"err,omitempty"`
	Usage       core.Usage `json:"usage"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	OutputFile  string     `json:"output_file,omitempty"`
}

// SpawnRequest 是一次后台任务的启动参数。
type SpawnRequest struct {
	Name   string
	Prompt string
	Loop   *agents.AgentLoop
	// Handler 是父运行的处理器：任务事件会带着 ParentRunID 复用同一个出口。
	Handler     events.EventHandler
	ParentRunID string
	Depth       int
	// MaxTurns 限制后续 agent_send 的轮次，默认 8。
	MaxTurns int
	// Deliver 指定终态投递目标（发起它的那个运行）。为 nil 时走 Registry.OnDone。
	Deliver func(events.TaskEnd)
}

type task struct {
	snap    TaskSnapshot
	loop    *agents.AgentLoop
	handler events.EventHandler
	deliver func(events.TaskEnd)
	history []core.Message
	inbox   chan string
	cancel  context.CancelFunc
	depth   int
	maxTurn int
	done    chan struct{}
	// deliver 指定终态投递目标；nil 时退回 Registry 的全局回调。
}

// Registry 是后台任务注册表。它自己持有一个可取消的根 ctx：
// Close 时所有任务被取消，wg 等它们收尾。
type Registry struct {
	mu       sync.Mutex
	tasks    map[string]*task
	journal  *Journal
	timeout  time.Duration
	maxDepth int
	baseCtx  context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	onDone   []func(events.TaskEnd)
	closed   bool
}

// RegistryOption 配置注册表。
type RegistryOption func(*Registry)

// WithJournalDir 设置 journal 目录（不设置则只在内存里保留状态）。
func WithJournalDir(dir string) RegistryOption {
	return func(r *Registry) {
		if j, err := NewJournal(dir); err == nil {
			r.journal = j
		}
	}
}

// WithTaskTimeout 设置单任务超时。
func WithTaskTimeout(d time.Duration) RegistryOption {
	return func(r *Registry) {
		if d > 0 {
			r.timeout = d
		}
	}
}

// WithMaxDepth 设置后台任务的最大下钻深度。
func WithMaxDepth(n int) RegistryOption {
	return func(r *Registry) {
		if n > 0 {
			r.maxDepth = n
		}
	}
}

// NewRegistry 构造注册表。
func NewRegistry(opts ...RegistryOption) *Registry {
	ctx, cancel := context.WithCancel(context.Background())
	r := &Registry{
		tasks:    map[string]*task{},
		timeout:  DefaultTaskTimeout,
		maxDepth: DefaultMaxDepth,
		baseCtx:  ctx,
		cancel:   cancel,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// OnDone 注册任务终态回调（结果回填主会话用）。可注册多个。
func (r *Registry) OnDone(fn func(events.TaskEnd)) {
	if fn == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onDone = append(r.onDone, fn)
}

// Spawn 登记并启动一个后台任务，立即返回快照。
func (r *Registry) Spawn(ctx context.Context, req SpawnRequest) (TaskSnapshot, error) {
	if req.Loop == nil {
		return TaskSnapshot{}, errors.New("subagent: 任务缺少 AgentLoop")
	}
	if req.Name == "" {
		req.Name = "task"
	}
	if req.Prompt == "" {
		return TaskSnapshot{}, errors.New("subagent: 任务缺少 prompt")
	}
	childDepth := req.Depth + 1
	if childDepth > r.maxDepth {
		return TaskSnapshot{}, ErrDepthExceeded
	}

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return TaskSnapshot{}, ErrRegistryClose
	}
	maxTurn := req.MaxTurns
	if maxTurn <= 0 {
		maxTurn = DefaultTaskMaxTurn
	}
	id := newTaskID("task")
	t := &task{
		snap: TaskSnapshot{
			ID:          id,
			Name:        req.Name,
			ParentRunID: req.ParentRunID,
			Status:      events.TaskRunning,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
		loop:    req.Loop,
		handler: req.Handler,
		deliver: req.Deliver,
		inbox:   make(chan string, taskInboxSize),
		depth:   childDepth,
		maxTurn: maxTurn,
		done:    make(chan struct{}),
	}
	if r.journal != nil {
		t.snap.OutputFile = r.journal.Path(id)
	}
	// 先建好可取消的 ctx：Interrupt 在任务刚启动时也必须生效。
	taskCtx, taskCancel := context.WithTimeout(r.baseCtx, r.timeout)
	t.cancel = taskCancel
	r.tasks[id] = t
	r.wg.Add(1)
	r.mu.Unlock()

	events.Emit(ctx, req.Handler, &events.TaskStarted{
		Base:   events.Base{RunID: id, ParentRunID: req.ParentRunID, Depth: childDepth, Time: time.Now()},
		TaskID: id,
		Name:   req.Name,
		Prompt: truncateRunes(req.Prompt, turnTextCap),
		Async:  true,
	})
	r.journalAppend(JournalRecord{
		Type: RecordStart, TaskID: id, ParentRunID: req.ParentRunID,
		Name: req.Name, Text: truncateRunes(req.Prompt, turnTextCap),
	})

	go r.run(taskCtx, t, t.done, []core.Message{core.NewUserTextMessage(req.Prompt)})
	return r.snapshot(t), nil
}

// run 执行任务，并在结束后处理收件箱里的后续指令。
func (r *Registry) run(ctx context.Context, t *task, done chan struct{}, history []core.Message) {
	defer r.wg.Done()
	defer close(done)

	lastText := ""
	var failure error

	for turn := 0; turn < t.maxTurn; turn++ {
		runID := newTaskID("run")
		res, err := t.loop.RunStream(ctx, agents.RunRequest{
			RunID:       runID,
			ParentRunID: t.snap.ParentRunID,
			Depth:       t.depth,
			Messages:    history,
			Deliver:     t.deliver,
		}, t.handler)
		if res != nil {
			history = res.Messages
			if res.FinalText != "" {
				lastText = res.FinalText
			}
			r.mu.Lock()
			t.snap.Usage.Add(res.Usage)
			t.history = history
			r.mu.Unlock()
		}
		if err != nil {
			failure = err
			break
		}

		select {
		case msg := <-t.inbox:
			events.Emit(ctx, t.handler, &events.TaskMessage{
				Base:   events.Base{RunID: runID, ParentRunID: t.snap.ParentRunID, Depth: t.depth, Time: time.Now()},
				TaskID: t.snap.ID,
				Text:   truncateRunes(msg, turnTextCap),
			})
			r.journalAppend(JournalRecord{Type: RecordMessage, TaskID: t.snap.ID, Text: truncateRunes(msg, turnTextCap)})
			history = append(history, core.NewUserTextMessage(msg))
			continue
		default:
		}
		break
	}

	r.finish(t, lastText, failure)
}

func (r *Registry) finish(t *task, text string, failure error) {
	status := events.TaskCompleted
	errText := ""
	if failure != nil {
		errText = failure.Error()
		if errors.Is(failure, context.Canceled) || errors.Is(failure, context.DeadlineExceeded) {
			status = events.TaskInterrupted
		} else {
			status = events.TaskFailed
		}
	}

	r.mu.Lock()
	t.snap.Status = status
	t.snap.Result = truncateRunes(text, taskTextCap)
	t.snap.Err = errText
	t.snap.UpdatedAt = time.Now()
	snap := t.snap
	deliver := t.deliver
	callbacks := append([]func(events.TaskEnd){}, r.onDone...)
	r.mu.Unlock()

	end := events.TaskEnd{
		Base:   events.Base{RunID: snap.ID, ParentRunID: snap.ParentRunID, Depth: t.depth, Time: time.Now()},
		TaskID: snap.ID,
		Status: status,
		Result: snap.Result,
		Err:    errText,
		Usage:  snap.Usage,
	}
	events.Emit(context.Background(), t.handler, &end)
	r.journalAppend(JournalRecord{
		Type: RecordEnd, TaskID: snap.ID, Status: status,
		Text: snap.Result, Err: errText, Usage: snap.Usage,
	})
	if deliver != nil {
		// 定向投递：结果回到「发起它的那个运行」，而不是主会话。
		deliver(end)
		return
	}
	for _, fn := range callbacks {
		fn(end)
	}
}

// Resume 让已结束的任务保留历史继续跑一条新指令（同一 task_id）。
//
// 语义：不新建任务、不丢上下文；运行中的任务请改用 Send，
// 避免两条 goroutine 抢同一 history。
func (r *Registry) Resume(ctx context.Context, taskID, message string) (TaskSnapshot, error) {
	r.mu.Lock()
	t, ok := r.tasks[taskID]
	if !ok {
		r.mu.Unlock()
		return TaskSnapshot{}, ErrTaskNotFound
	}
	if r.closed {
		r.mu.Unlock()
		return TaskSnapshot{}, ErrRegistryClose
	}
	select {
	case <-t.done:
	default:
		r.mu.Unlock()
		return TaskSnapshot{}, ErrTaskRunning
	}
	if len(t.history) == 0 {
		r.mu.Unlock()
		return TaskSnapshot{}, ErrTaskFinished
	}

	t.done = make(chan struct{})
	done := t.done
	t.snap.Status = events.TaskRunning
	t.snap.UpdatedAt = time.Now()
	history := append([]core.Message(nil), t.history...)
	if strings.TrimSpace(message) != "" {
		history = append(history, core.NewUserTextMessage(message))
	}
	taskCtx, cancel := context.WithTimeout(r.baseCtx, r.timeout)
	t.cancel = cancel
	snap := t.snap
	handler := t.handler
	parent := t.snap.ParentRunID
	depth := t.depth
	r.wg.Add(1)
	r.mu.Unlock()

	if strings.TrimSpace(message) != "" {
		events.Emit(ctx, handler, &events.TaskMessage{
			Base:   events.Base{RunID: taskID, ParentRunID: parent, Depth: depth, Time: time.Now()},
			TaskID: taskID,
			Text:   truncateRunes(message, turnTextCap),
		})
		r.journalAppend(JournalRecord{Type: RecordMessage, TaskID: taskID, Text: truncateRunes(message, turnTextCap)})
	}
	go r.run(taskCtx, t, done, history)
	return snap, nil
}

// Send 给运行中的任务追加一条指令。
func (r *Registry) Send(taskID, message string) error {
	r.mu.Lock()
	t, ok := r.tasks[taskID]
	r.mu.Unlock()
	if !ok {
		return ErrTaskNotFound
	}
	select {
	case <-t.done:
		return ErrTaskFinished
	default:
	}
	select {
	case t.inbox <- message:
		return nil
	default:
		return ErrTaskInboxFull
	}
}

// Interrupt 取消任务。
func (r *Registry) Interrupt(taskID string) error {
	r.mu.Lock()
	t, ok := r.tasks[taskID]
	cancel := t2cancel(t)
	r.mu.Unlock()
	if !ok {
		return ErrTaskNotFound
	}
	select {
	case <-t.done:
		return ErrTaskFinished
	default:
	}
	if cancel != nil {
		cancel()
	}
	return nil
}

func t2cancel(t *task) context.CancelFunc {
	if t == nil {
		return nil
	}
	return t.cancel
}

// Snapshot 返回单个任务快照。
func (r *Registry) Snapshot(taskID string) (TaskSnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tasks[taskID]
	if !ok {
		return TaskSnapshot{}, ErrTaskNotFound
	}
	return t.snap, nil
}

// List 返回全部任务快照（按创建时间排序）。
func (r *Registry) List() []TaskSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]TaskSnapshot, 0, len(r.tasks))
	for _, t := range r.tasks {
		out = append(out, t.snap)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// Records 读取任务 journal（mode: status / latest / all）。
func (r *Registry) Records(taskID, mode string) ([]JournalRecord, error) {
	if _, err := r.Snapshot(taskID); err != nil {
		return nil, err
	}
	if r.journal == nil {
		return nil, nil
	}
	all, err := r.journal.ReadAll(taskID)
	if err != nil {
		return nil, err
	}
	switch mode {
	case "all":
		return all, nil
	case "latest":
		for i := len(all) - 1; i >= 0; i-- {
			if all[i].Type == RecordMessage || all[i].Type == RecordEnd {
				return all[i:], nil
			}
		}
		return all, nil
	default:
		return all, nil
	}
}

// Wait 等待某个任务结束（测试与宿主收尾用）。
func (r *Registry) Wait(taskID string) {
	r.mu.Lock()
	t, ok := r.tasks[taskID]
	r.mu.Unlock()
	if ok {
		<-t.done
	}
}

// Close 取消全部任务并等待收尾。
func (r *Registry) Close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	r.mu.Unlock()

	r.cancel()
	r.wg.Wait()
}

func (r *Registry) journalAppend(rec JournalRecord) {
	if r.journal == nil {
		return
	}
	// journal 写失败不影响任务本身：事实仍在内存里，下一个检查点会带上。
	_ = r.journal.Append(rec)
}

func (r *Registry) snapshot(t *task) TaskSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	return t.snap
}

// 便于在错误里带上任务名。
func (s TaskSnapshot) String() string {
	return fmt.Sprintf("%s(%s)", s.ID, s.Status)
}
