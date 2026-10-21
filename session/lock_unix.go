//go:build unix

package session

import (
	"os"
	"syscall"
)

// lockFile 用 flock 对会话文件加排他锁，防止两个进程同时写同一会话。
// 返回的 release 必须调用；进程退出时内核也会自动释放。
func lockFile(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
