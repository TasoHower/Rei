//go:build !unix

package session

// lockFile 在非 unix 平台退化为进程内互斥（FileStore 自身已有 mutex）。
func lockFile(string) (func(), error) { return func() {}, nil }
