package mcp

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// Transport 是一条 MCP 消息通道。每条消息是一个完整的 JSON-RPC 帧。
type Transport interface {
	Start(ctx context.Context) error
	Send(ctx context.Context, payload []byte) error
	Recv(ctx context.Context) ([]byte, error)
	Close() error
}

// queue 是所有传输共用的一条消息队列。
type queue struct {
	ch     chan []byte
	once   sync.Once
	closed chan struct{}
	err    error
	mu     sync.Mutex
}

func newQueue() *queue {
	return &queue{ch: make(chan []byte, 64), closed: make(chan struct{})}
}

func (q *queue) push(msg []byte) {
	if len(msg) == 0 {
		return
	}
	select {
	case q.ch <- msg:
	case <-q.closed:
	}
}

func (q *queue) fail(err error) {
	q.mu.Lock()
	if q.err == nil {
		q.err = err
	}
	q.mu.Unlock()
	q.once.Do(func() { close(q.closed) })
}

func (q *queue) close() { q.fail(io.EOF) }

func (q *queue) recv(ctx context.Context) ([]byte, error) {
	select {
	case msg := <-q.ch:
		return msg, nil
	case <-q.closed:
		q.mu.Lock()
		err := q.err
		q.mu.Unlock()
		if err == nil {
			err = io.EOF
		}
		// 队列里可能还有未取走的响应，优先取完再报错。
		select {
		case msg := <-q.ch:
			return msg, nil
		default:
		}
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// ---------------------------------------------------------------- stdio

// StdioTransport 通过子进程的 stdin/stdout 传输 NDJSON。
type StdioTransport struct {
	command string
	args    []string
	env     map[string]string
	dir     string

	q      *queue
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	mu     sync.Mutex
	closed bool
}

// NewStdioTransport 构造 stdio 传输。
func NewStdioTransport(command string, args []string, env map[string]string) *StdioTransport {
	return &StdioTransport{command: command, args: args, env: env, q: newQueue()}
}

// WithDir 设置子进程工作目录。
func (t *StdioTransport) WithDir(dir string) *StdioTransport {
	t.dir = dir
	return t
}

// Start 启动子进程并开始读 stdout。
func (t *StdioTransport) Start(ctx context.Context) error {
	cmd := exec.Command(t.command, t.args...)
	cmd.Dir = t.dir
	cmd.Env = append(os.Environ(), flattenEnv(t.env)...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("mcp: 启动 %s 失败：%w", t.command, err)
	}
	t.cmd = cmd
	t.stdin = stdin

	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 64<<10), 8<<20)
		for scanner.Scan() {
			line := bytes.TrimSpace(scanner.Bytes())
			if len(line) == 0 {
				continue
			}
			t.q.push(append([]byte(nil), line...))
		}
		if err := scanner.Err(); err != nil {
			t.q.fail(err)
			return
		}
		t.q.close()
	}()

	go func() {
		_ = cmd.Wait()
		t.q.close()
	}()
	// 注意：这里不监听 ctx。启动用的 ctx 通常只是「连接超时」，
	// 一旦它到期就关闭传输，会让后续调用全部失败——生命周期应由 Close 控制。
	_ = ctx
	return nil
}

// Send 写一行 JSON。
func (t *StdioTransport) Send(_ context.Context, payload []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || t.stdin == nil {
		return errors.New("mcp: stdio 传输已关闭")
	}
	if _, err := t.stdin.Write(append(payload, '\n')); err != nil {
		return err
	}
	return nil
}

// Recv 读一行 JSON。
func (t *StdioTransport) Recv(ctx context.Context) ([]byte, error) { return t.q.recv(ctx) }

// Close 关闭 stdin 并结束子进程。
func (t *StdioTransport) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	stdin := t.stdin
	cmd := t.cmd
	t.mu.Unlock()

	if stdin != nil {
		_ = stdin.Close()
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	t.q.close()
	return nil
}

func flattenEnv(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}

// ---------------------------------------------------------------- HTTP

// HTTPTransport 用「一问一答」的 POST 实现 Streamable HTTP：
// 每次 Send 发一个 JSON-RPC 请求，响应可以是 application/json 或 text/event-stream。
type HTTPTransport struct {
	url     string
	headers map[string]string
	client  *http.Client
	q       *queue
}

// NewHTTPTransport 构造 HTTP 传输。
func NewHTTPTransport(url string, headers map[string]string) *HTTPTransport {
	return &HTTPTransport{url: url, headers: headers, client: &http.Client{}, q: newQueue()}
}

// Start 实现 Transport（HTTP 无长连接）。
func (t *HTTPTransport) Start(context.Context) error { return nil }

// Send 发送请求，并把响应消息推入队列。
func (t *HTTPTransport) Send(ctx context.Context, payload []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<10))
		return fmt.Errorf("mcp: HTTP %d：%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		readSSE(resp.Body, t.q)
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(body)) > 0 {
		t.q.push(body)
	}
	return nil
}

// Recv 从队列取响应。
func (t *HTTPTransport) Recv(ctx context.Context) ([]byte, error) { return t.q.recv(ctx) }

// Close 实现 Transport。
func (t *HTTPTransport) Close() error {
	t.q.close()
	return nil
}

// ---------------------------------------------------------------- SSE

// SSETransport 实现旧式 HTTP+SSE 传输：
// GET 打开事件流并发现 POST 端点，请求通过 POST 发送，响应从事件流回来。
type SSETransport struct {
	url     string
	headers map[string]string
	client  *http.Client
	q       *queue

	mu      sync.Mutex
	postURL string
	body    io.ReadCloser
}

// NewSSETransport 构造 SSE 传输。
func NewSSETransport(url string, headers map[string]string) *SSETransport {
	return &SSETransport{url: url, headers: headers, client: &http.Client{}, q: newQueue()}
}

// Start 打开事件流，等待 endpoint 事件。
//
// 事件流必须脱离「连接超时 ctx」：调用方的 ctx 只用于握手，
// 一旦握手完成就取消，会把长连接一起cancel 掉，后续调用全部无响应。
// 生命周期改由 Close 控制。
func (t *SSETransport) Start(ctx context.Context) error {
	req, err := http.NewRequestWithContext(context.WithoutCancel(ctx), http.MethodGet, t.url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		_ = resp.Body.Close()
		return fmt.Errorf("mcp: SSE 端点返回 %d", resp.StatusCode)
	}
	t.body = resp.Body

	endpointCh := make(chan string, 1)
	go readSSEEvents(resp.Body, func(event, data string) {
		if event == "endpoint" {
			select {
			case endpointCh <- data:
			default:
			}
			return
		}
		if data != "" {
			t.q.push([]byte(data))
		}
	})

	select {
	case endpoint := <-endpointCh:
		t.mu.Lock()
		t.postURL = resolveURL(t.url, endpoint)
		t.mu.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Send 向 POST 端点投递消息（响应走事件流）。
func (t *SSETransport) Send(ctx context.Context, payload []byte) error {
	t.mu.Lock()
	postURL := t.postURL
	t.mu.Unlock()
	if postURL == "" {
		return errors.New("mcp: SSE 传输尚未就绪")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, postURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("mcp: SSE 投递返回 %d", resp.StatusCode)
	}
	return nil
}

// Recv 从事件流队列取消息。
func (t *SSETransport) Recv(ctx context.Context) ([]byte, error) { return t.q.recv(ctx) }

// Close 实现 Transport。
func (t *SSETransport) Close() error {
	if t.body != nil {
		_ = t.body.Close()
	}
	t.q.close()
	return nil
}

func resolveURL(base, ref string) string {
	b, err := url.Parse(base)
	if err != nil {
		return ref
	}
	r, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return b.ResolveReference(r).String()
}

// ---------------------------------------------------------------- SSE 解析

// readSSE 把 SSE 流里的 data 帧推入队列（HTTP 传输用）。
func readSSE(r io.Reader, q *queue) {
	readSSEEvents(r, func(_, data string) {
		if data != "" {
			q.push([]byte(data))
		}
	})
}

// readSSEEvents 是通用的 SSE 解析器。
func readSSEEvents(r io.Reader, fn func(event, data string)) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64<<10), 8<<20)
	event := ""
	var data []string
	flush := func() {
		if len(data) > 0 {
			fn(event, strings.Join(data, "\n"))
		}
		event = ""
		data = nil
	}
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			// SSE 规范允许 "data: value" 这种带一个前导空格的形式。
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	flush()
}
