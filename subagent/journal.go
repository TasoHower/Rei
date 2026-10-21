package subagent

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tasohower/rei/core"
)

// 记录类型。
const (
	RecordStart   = "start"
	RecordMessage = "message"
	RecordEnd     = "end"
)

// 截断上限（按 rune 计，避免中文被切坏）。
const (
	turnTextCap = 4000
	taskTextCap = 8000
)

// JournalRecord 是后台任务持久化的一行。
//
// 只记录「后代如何继续工作」所需的最小事实：任务身份、父运行、每轮文本摘要、
// 终态与用量。完整对话由任务自己的 Session/AgentLoop 负责，不在这里复制。
type JournalRecord struct {
	Time        time.Time  `json:"time"`
	Type        string     `json:"type"`
	TaskID      string     `json:"task_id"`
	ParentRunID string     `json:"parent_run_id,omitempty"`
	Name        string     `json:"name,omitempty"`
	Text        string     `json:"text,omitempty"`
	Status      string     `json:"status,omitempty"`
	Err         string     `json:"err,omitempty"`
	Usage       core.Usage `json:"usage,omitempty"`
}

// Journal 把任务记录按任务写入独立 JSONL 文件。
type Journal struct{ dir string }

// NewJournal 创建目录并返回日志。
func NewJournal(dir string) (*Journal, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, err
	}
	return &Journal{dir: abs}, nil
}

// Path 返回某个任务的日志路径。
func (j *Journal) Path(taskID string) string {
	return filepath.Join(j.dir, taskID+".jsonl")
}

// Append 追加一条记录：O_APPEND 单写，只在终态时 fsync。
// 中间记录丢了只是少一条摘要，终态丢了才是真的丢事实。
func (j *Journal) Append(rec JournalRecord) error {
	if rec.TaskID == "" {
		return errors.New("subagent: 记录缺少 TaskID")
	}
	if rec.Time.IsZero() {
		rec.Time = time.Now()
	}
	if len([]rune(rec.Text)) > taskTextCap {
		rec.Text = truncateRunes(rec.Text, taskTextCap)
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(j.Path(rec.TaskID), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(append(raw, '\n')); err != nil {
		return err
	}
	if rec.Type == RecordEnd {
		if err := f.Sync(); err != nil {
			return err
		}
	}
	return nil
}

// ReadAll 读取某个任务的全部记录。
func (j *Journal) ReadAll(taskID string) ([]JournalRecord, error) {
	f, err := os.Open(j.Path(taskID))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var out []JournalRecord
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var rec JournalRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			// 半写行（崩溃）跳过，而不是让整个任务不可读。
			continue
		}
		out = append(out, rec)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("subagent: 读取 %s 失败：%w", taskID, err)
	}
	return out, nil
}

func truncateRunes(s string, limit int) string {
	if limit <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit]) + "…"
}
