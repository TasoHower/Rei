package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tasohower/rei/core"
	"github.com/tasohower/rei/tools"
)

// httpGetTool 抓取一个 URL 的文本内容。
//
// 带 SSRF 防护：解析全部 IP，拒绝回环/私网/链路本地/保留地址，并校验每一跳重定向。
// 它只用于取文档与只读接口；需要认证的请求由宿主自己封装。
type httpGetTool struct{}

const (
	httpMaxBytes   = 1 << 20
	httpTimeout    = 20 * time.Second
	maxURLLength   = 2048
	redirectChecks = 5
)

func (t *httpGetTool) Name() string { return "http_get" }

func (t *httpGetTool) Description() string {
	return "Fetch a public HTTP(S) URL and return its text body. Private and loopback addresses are refused (SSRF protection)."
}

func (t *httpGetTool) CanParallel() bool { return true }

func (t *httpGetTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"url":{"type":"string","description":"Absolute http(s) URL."},"headers":{"type":"object","description":"Optional request headers.","additionalProperties":{"type":"string"}}},"required":["url"]}`)
}

func (t *httpGetTool) Call(ctx context.Context, call core.ToolCall) tools.Result {
	var args struct {
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
	}
	if err := decode(call, &args); err != nil {
		return tools.ErrorResult("%v", err)
	}
	if len(args.URL) > maxURLLength {
		return tools.ErrorResult("url is too long")
	}
	if err := checkPublicURL(args.URL); err != nil {
		return tools.ErrorResult("%v", err)
	}

	client := &http.Client{
		Timeout: httpTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= redirectChecks {
				return errors.New("too many redirects")
			}
			return checkPublicURL(req.URL.String())
		},
	}

	reqCtx, cancel := context.WithTimeout(ctx, httpTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, args.URL, nil)
	if err != nil {
		return tools.ErrorResult("invalid request: %v", err)
	}
	req.Header.Set("User-Agent", "rei/0.1")
	for k, v := range args.Headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return tools.ErrorResult("fetch failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, httpMaxBytes+1))
	if err != nil {
		return tools.ErrorResult("read failed: %v", err)
	}
	truncated := len(body) > httpMaxBytes
	if truncated {
		body = body[:httpMaxBytes]
	}

	text := string(body)
	if strings.ContainsRune(text[:min(len(text), 512)], 0) {
		return tools.ErrorResult("response looks binary (%s); refusing to inline it", resp.Header.Get("Content-Type"))
	}

	header := fmt.Sprintf("HTTP %d %s\ncontent-type: %s\n\n", resp.StatusCode, http.StatusText(resp.StatusCode), resp.Header.Get("Content-Type"))
	res := tools.Result{Content: header + text, Truncated: truncated, Metadata: map[string]any{"status": resp.StatusCode}}
	if resp.StatusCode >= 400 {
		res.IsError = true
	}
	return res
}

// checkPublicURL 拒绝非 http(s)、无主机名、以及解析到内网地址的 URL。
func checkPublicURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return errors.New("url has no host")
	}
	// 直接写 IP 的情况也要检查。
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedIP(ip) {
			return fmt.Errorf("refusing to fetch private or loopback address %s", host)
		}
		return nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("cannot resolve %s: %w", host, err)
	}
	for _, ip := range ips {
		if isBlockedIP(ip) {
			return fmt.Errorf("refusing to fetch %s: resolves to blocked address %s", host, ip)
		}
	}
	return nil
}

func isBlockedIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
