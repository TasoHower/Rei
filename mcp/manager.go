package mcp

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tasohower/rei/tools"
)

// 默认值：连接超时与失败冷却。
const (
	DefaultConnectTimeout = 15 * time.Second
	DefaultCooldown       = 10 * time.Second
)

// Manager 管理多台 MCP 服务器：惰性连接、失败冷却、工具注册。
type Manager struct {
	cfg         Config
	timeout     time.Duration
	callTimeout time.Duration
	cooldown    time.Duration

	mu       sync.Mutex
	clients  map[string]*Client
	cache    map[string][]ToolInfo
	lastErr  map[string]error
	lastFail map[string]time.Time
}

// ManagerOption 配置管理器。
type ManagerOption func(*Manager)

// WithConnectTimeout 设置单台服务器的连接超时。
func WithConnectTimeout(d time.Duration) ManagerOption {
	return func(m *Manager) {
		if d > 0 {
			m.timeout = d
		}
	}
}

// WithCallTimeout 设置单次 tools/call 的超时（默认沿用连接超时）。
func WithCallTimeout(d time.Duration) ManagerOption {
	return func(m *Manager) {
		if d > 0 {
			m.callTimeout = d
		}
	}
}

// WithCooldown 设置失败冷却窗口：窗口内重复调用直接返回上次错误。
func WithCooldown(d time.Duration) ManagerOption {
	return func(m *Manager) {
		if d > 0 {
			m.cooldown = d
		}
	}
}

// NewManager 构造管理器。
func NewManager(cfg Config, opts ...ManagerOption) *Manager {
	m := &Manager{
		cfg:      cfg,
		timeout:  DefaultConnectTimeout,
		cooldown: DefaultCooldown,
		clients:  map[string]*Client{},
		cache:    map[string][]ToolInfo{},
		lastErr:  map[string]error{},
		lastFail: map[string]time.Time{},
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// RegisterTools 连接所有启用的服务器，并把它们的工具注册到引擎上。
// 单台失败不影响其它服务器：返回值为成功注册的工具数，error 汇总失败原因。
func (m *Manager) RegisterTools(ctx context.Context, eng *tools.Engine) (int, error) {
	if eng == nil {
		return 0, errors.New("mcp: 工具引擎为 nil")
	}
	names := m.cfg.ServerNames()
	var errs []string
	registered := 0

	for _, name := range names {
		srv := m.cfg.Servers[name]
		if !srv.IsEnabled() {
			continue
		}
		client, err := m.ensure(ctx, srv)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		var infos []ToolInfo
		if cached, ok := m.cachedTools(name); ok {
			infos = cached
		} else {
			infos, err = client.ListTools(ctx)
			if err != nil {
				m.fail(name, err)
				errs = append(errs, fmt.Sprintf("%s: %v", name, err))
				continue
			}
			m.cacheTools(name, infos)
		}
		for _, info := range infos {
			tool := newServerTool(client, name, info)
			tool.mgr = m
			if err := eng.Register(tool); err != nil {
				// 工具名冲突时跳过这一条，不拖垮整台服务器。
				errs = append(errs, fmt.Sprintf("%s/%s: %v", name, info.Name, err))
				continue
			}
			registered++
		}
	}

	if len(errs) > 0 {
		return registered, errors.New("mcp: " + strings.Join(errs, "; "))
	}
	return registered, nil
}

// ensure 返回可用连接；失败后在冷却窗口内直接复用上次错误。
func (m *Manager) ensure(ctx context.Context, srv ServerConfig) (*Client, error) {
	m.mu.Lock()
	if client, ok := m.clients[srv.Name]; ok {
		m.mu.Unlock()
		return client, nil
	}
	if err, ok := m.lastErr[srv.Name]; ok && time.Since(m.lastFail[srv.Name]) < m.cooldown {
		m.mu.Unlock()
		return nil, err
	}
	m.mu.Unlock()

	normalized, err := srv.Normalize()
	if err != nil {
		m.fail(srv.Name, err)
		return nil, err
	}

	connectCtx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()

	client := newClient(srv.Name, newTransport(normalized))
	if err := client.Start(connectCtx); err != nil {
		m.fail(srv.Name, err)
		return nil, err
	}

	client.timeout = m.callTimeout
	if client.timeout == 0 {
		client.timeout = m.timeout
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.lastErr, srv.Name)
	delete(m.lastFail, srv.Name)
	m.clients[srv.Name] = client
	return client, nil
}

// Reconnect 关闭旧连接并重新走一次握手（MCP 服务器崩溃后的恢复路径）。
func (m *Manager) Reconnect(ctx context.Context, name string) (*Client, error) {
	srv, ok := m.cfg.Servers[name]
	if !ok {
		return nil, fmt.Errorf("mcp: 未配置服务器 %q", name)
	}
	m.mu.Lock()
	if old, ok := m.clients[name]; ok {
		_ = old.Close()
		delete(m.clients, name)
	}
	delete(m.cache, name)
	m.mu.Unlock()
	return m.ensure(ctx, srv)
}

// Reset 断开指定服务器（测试与配置变更时使用）。
func (m *Manager) Reset(name string) {
	m.mu.Lock()
	if old, ok := m.clients[name]; ok {
		_ = old.Close()
		delete(m.clients, name)
	}
	delete(m.cache, name)
	m.mu.Unlock()
}

func (m *Manager) cachedTools(name string) ([]ToolInfo, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	infos, ok := m.cache[name]
	return infos, ok
}

func (m *Manager) cacheTools(name string, infos []ToolInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cache[name] = infos
}

func newTransport(srv ServerConfig) Transport {
	switch srv.Type {
	case TransportHTTP:
		return NewHTTPTransport(srv.URL, srv.Headers)
	case TransportSSE:
		return NewSSETransport(srv.URL, srv.Headers)
	default:
		return NewStdioTransport(srv.Command, srv.Args, srv.Env)
	}
}

func (m *Manager) fail(name string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastErr[name] = err
	m.lastFail[name] = time.Now()
}

// Servers 返回当前已连接的服务器名。
func (m *Manager) Servers() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.clients))
	for name := range m.clients {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Close 关闭全部连接。
func (m *Manager) Close() error {
	m.mu.Lock()
	clients := make([]*Client, 0, len(m.clients))
	for _, c := range m.clients {
		clients = append(clients, c)
	}
	m.clients = map[string]*Client{}
	m.mu.Unlock()

	var errs []string
	for _, c := range clients {
		if err := c.Close(); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}
