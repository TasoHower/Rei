// Package mcp 实现最小可用的 MCP（Model Context Protocol）客户端：
// 四种传输（stdio / http / sse 合并为三种形态）、JSON-RPC 客户端、
// 工具适配器与惰性连接管理。
//
// 刻意不做的事：不做热重载 watcher、不做 OAuth、不做 resources/prompts。
// 这些属于宿主职责；内核只保证「配置 → 工具注册 → 工具调用」这条链路可靠。
package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

// 传输类型。
const (
	TransportStdio = "stdio"
	TransportHTTP  = "http"
	TransportSSE   = "sse"
)

// ServerConfig 是单个 MCP 服务器的配置。
type ServerConfig struct {
	Name string `json:"-"`
	// Type 取 stdio / http / sse；空值按 stdio 处理。
	// streamable_http / streamable-http 归一为 http。
	Type    string            `json:"type"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	// Enabled 为 nil 表示启用。
	Enabled *bool `json:"enabled"`
}

// Config 是一份 MCP 配置。
type Config struct {
	Servers map[string]ServerConfig `json:"mcpServers"`
}

// Normalize 归一化传输类型并校验必填字段。
func (c ServerConfig) Normalize() (ServerConfig, error) {
	out := c
	switch strings.ToLower(strings.TrimSpace(c.Type)) {
	case "", TransportStdio:
		out.Type = TransportStdio
	case TransportHTTP, "streamable_http", "streamable-http":
		out.Type = TransportHTTP
	case TransportSSE:
		out.Type = TransportSSE
	default:
		return ServerConfig{}, fmt.Errorf("mcp: 服务器 %q 的传输类型 %q 不支持", c.Name, c.Type)
	}
	if out.IsEnabled() {
		switch out.Type {
		case TransportStdio:
			if strings.TrimSpace(out.Command) == "" {
				return ServerConfig{}, fmt.Errorf("mcp: 服务器 %q 使用 stdio 时必须配置 command", c.Name)
			}
		default:
			if strings.TrimSpace(out.URL) == "" {
				return ServerConfig{}, fmt.Errorf("mcp: 服务器 %q 使用 %s 时必须配置 url", c.Name, out.Type)
			}
		}
	}
	return out, nil
}

// IsEnabled 判断服务器是否启用（nil 视为启用）。
func (c ServerConfig) IsEnabled() bool { return c.Enabled == nil || *c.Enabled }

// LoadFile 读取单个配置文件。
func LoadFile(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("mcp: 解析 %s 失败：%w", path, err)
	}
	for name, srv := range cfg.Servers {
		srv.Name = name
		cfg.Servers[name] = srv
	}
	return cfg, nil
}

// LoadLayers 按优先级从低到高合并多份配置：同名服务器后者覆盖前者。
// 坏文件被跳过（返回的 error 汇总了跳过的原因），避免一个坏配置拖垮全部服务器。
func LoadLayers(paths ...string) (Config, error) {
	merged := Config{Servers: map[string]ServerConfig{}}
	var errs []string
	for _, path := range paths {
		cfg, err := LoadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			errs = append(errs, err.Error())
			continue
		}
		for name, srv := range cfg.Servers {
			merged.Servers[name] = srv
		}
	}
	if len(errs) > 0 {
		return merged, errors.New(strings.Join(errs, "; "))
	}
	return merged, nil
}

// ServerNames 返回排序后的服务器名，便于稳定输出。
func (c Config) ServerNames() []string {
	names := make([]string, 0, len(c.Servers))
	for name := range c.Servers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
