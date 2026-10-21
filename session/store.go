package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/tasohower/rei/core"
	"github.com/tasohower/rei/todo"
)

// ErrChecksum 表示快照内容与记录的校验和不一致（文件被篡改或写坏）。
var ErrChecksum = errors.New("session: 快照校验和不匹配")

// Snapshot 是会话的持久化形态：消息、用量、待办与单调递增的 revision。
type Snapshot struct {
	SessionID string         `json:"session_id"`
	Revision  int64          `json:"revision"`
	Messages  []core.Message `json:"messages"`
	Usage     core.Usage     `json:"usage"`
	// Todos 随检查点落盘：它不参与压缩，长会话里始终可见。
	Todos    []todo.Item `json:"todos,omitempty"`
	Checksum string      `json:"checksum,omitempty"`
}

// computeChecksum 对「不含 checksum 的 JSON 字节」求 sha256。
// 先序列化再摘要，保证同一份内容在任何平台上得到同一个值。
func (s Snapshot) computeChecksum() (string, error) {
	probe := s
	probe.Checksum = ""
	raw, err := json.Marshal(probe)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// Store 持久化会话快照。实现方只负责存取，不参与 Agent 生命周期。
type Store interface {
	Save(ctx context.Context, snap Snapshot) error
	// Load 在会话不存在时返回零值快照而不是错误（冷启动语义）。
	Load(ctx context.Context, sessionID string) (Snapshot, error)
}

// MemoryStore 是进程内存储，用于测试与短生命周期宿主。
type MemoryStore struct {
	mu   sync.RWMutex
	data map[string]Snapshot
}

// NewMemoryStore 构造内存存储。
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{data: map[string]Snapshot{}}
}

// Save 实现 Store。
func (m *MemoryStore) Save(_ context.Context, snap Snapshot) error {
	if snap.SessionID == "" {
		return errors.New("session: 快照缺少 SessionID")
	}
	next, err := nextRevision(snap, m.load(snap.SessionID))
	if err != nil {
		return err
	}
	checksum, err := next.computeChecksum()
	if err != nil {
		return err
	}
	next.Checksum = checksum
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[snap.SessionID] = next
	return nil
}

// Load 实现 Store。
func (m *MemoryStore) Load(_ context.Context, sessionID string) (Snapshot, error) {
	snap := m.load(sessionID)
	if snap.Checksum == "" {
		return Snapshot{SessionID: sessionID}, nil
	}
	want, err := snap.computeChecksum()
	if err != nil {
		return Snapshot{}, err
	}
	if want != snap.Checksum {
		return Snapshot{}, ErrChecksum
	}
	return snap, nil
}

func (m *MemoryStore) load(sessionID string) Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.data[sessionID]
}

// nextRevision 保证 revision 严格递增。
//
// 内容完全相同一律视为幂等重放（无论调用方给的 revision 是多少）：
// 崩溃恢复后重放同一个检查点是常态，不该因此把 revision 推高。
func nextRevision(in Snapshot, prev Snapshot) (Snapshot, error) {
	if prev.Revision == 0 {
		in.Revision = 1
		return in, nil
	}
	if sameMessages(in.Messages, prev.Messages) && sameUsage(in.Usage, prev.Usage) {
		return prev, nil
	}
	if in.Revision > prev.Revision {
		return in, nil
	}
	in.Revision = prev.Revision + 1
	return in, nil
}

func sameUsage(a, b core.Usage) bool {
	return a.Input == b.Input && a.Output == b.Output &&
		a.CachedInput == b.CachedInput && a.Reasoning == b.Reasoning &&
		a.Cost.Total() == b.Cost.Total()
}

func sameMessages(a, b []core.Message) bool {
	if len(a) != len(b) {
		return false
	}
	ra, err1 := json.Marshal(a)
	rb, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && string(ra) == string(rb)
}

// FileStore 把每个会话写成一个 JSON 文件：临时文件 → fsync → rename → fsync(dir)。
type FileStore struct {
	dir string
	mu  sync.Mutex
}

// NewFileStore 创建目录并返回文件存储。
func NewFileStore(dir string) (*FileStore, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, err
	}
	return &FileStore{dir: abs}, nil
}

func (f *FileStore) path(sessionID string) string {
	return filepath.Join(f.dir, sessionID+".json")
}

func (f *FileStore) backupPath(sessionID string) string {
	return filepath.Join(f.dir, sessionID+".json.bak")
}

func (f *FileStore) lockPath(sessionID string) string {
	return filepath.Join(f.dir, sessionID+".json.lock")
}

// Save 实现 Store。
//
// 提交顺序：flock → 读旧值算 revision → 旧文件转 .bak → 写 tmp → fsync → rename → fsync(dir)。
// 任何一步失败都不会破坏已有的 .bak，读取侧因此总有可回退的版本。
func (f *FileStore) Save(_ context.Context, snap Snapshot) error {
	if snap.SessionID == "" {
		return errors.New("session: 快照缺少 SessionID")
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	release, err := lockFile(f.lockPath(snap.SessionID))
	if err != nil {
		return fmt.Errorf("session: 加锁失败：%w", err)
	}
	defer release()

	prev, err := f.loadLocked(snap.SessionID)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	next, err := nextRevision(snap, prev)
	if err != nil {
		return err
	}
	checksum, err := next.computeChecksum()
	if err != nil {
		return err
	}
	next.Checksum = checksum

	raw, err := json.Marshal(next)
	if err != nil {
		return err
	}

	// 旧文件转 .bak：读侧在 checksum 失败时回退到它。
	if oldRaw, err := os.ReadFile(f.path(snap.SessionID)); err == nil {
		if err := writeAtomicFile(f.backupPath(snap.SessionID), oldRaw); err != nil {
			return err
		}
	}

	if err := writeAtomicFile(f.path(snap.SessionID), raw); err != nil {
		return err
	}
	if d, err := os.Open(f.dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// Load 实现 Store：主文件损坏或校验失败时回退到 .bak。
func (f *FileStore) Load(_ context.Context, sessionID string) (Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	release, err := lockFile(f.lockPath(sessionID))
	if err != nil {
		return Snapshot{}, fmt.Errorf("session: 加锁失败：%w", err)
	}
	defer release()

	snap, mainErr := f.loadLocked(sessionID)
	if mainErr == nil {
		return snap, nil
	}
	if errors.Is(mainErr, os.ErrNotExist) {
		return Snapshot{SessionID: sessionID}, nil
	}

	// 主文件不可用：尝试 .bak；没有可用备份时把真实原因抛回去。
	if fallback, bakErr := f.loadLockedPath(f.backupPath(sessionID)); bakErr == nil {
		return fallback, nil
	}
	return Snapshot{}, mainErr
}

func (f *FileStore) loadLocked(sessionID string) (Snapshot, error) {
	return f.loadLockedPath(f.path(sessionID))
}

func (f *FileStore) loadLockedPath(path string) (Snapshot, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Snapshot{}, err
	}
	var snap Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return Snapshot{}, fmt.Errorf("session: 解析 %s 失败：%w", path, err)
	}
	want, err := snap.computeChecksum()
	if err != nil {
		return Snapshot{}, err
	}
	if want != snap.Checksum {
		return Snapshot{}, fmt.Errorf("%w：%s", ErrChecksum, path)
	}
	return snap, nil
}

// writeAtomicFile 写临时文件 → fsync → rename，权限 0600。
func writeAtomicFile(path string, raw []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".snapshot-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
