package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Kind 是错误语义标签。上层据此决定「重试 / 压缩后重试 / 直接失败」，
// 而不是去匹配裸字符串。
type Kind string

const (
	KindRateLimit       Kind = "rate_limit"
	KindUpstream        Kind = "upstream_unavailable"
	KindContextExceeded Kind = "context_exceeded"
	KindPermanent       Kind = "permanent"
	KindCanceled        Kind = "canceled"
	KindGeneric         Kind = "generic"
)

// Error 是 provider 层统一错误。
type Error struct {
	Kind       Kind
	StatusCode int
	Message    string
	RetryAfter time.Duration
}

func (e *Error) Error() string {
	if e.StatusCode > 0 {
		return fmt.Sprintf("provider %s (%d): %s", e.Kind, e.StatusCode, e.Message)
	}
	return fmt.Sprintf("provider %s: %s", e.Kind, e.Message)
}

// Retryable 表示这次失败是否值得重试。
func (e *Error) Retryable() bool {
	switch e.Kind {
	case KindRateLimit, KindUpstream:
		return true
	default:
		return false
	}
}

// contextHints 是各家上游表达「上下文超限」的常见措辞。
var contextHints = []string{
	"context length", "context_length", "maximum context",
	"too many tokens", "prompt is too long", "input is too long",
	"exceeds the maximum", "上下文长度",
}

// ClassifyHTTPError 把 HTTP 状态码与响应体翻译成错误语义。
func ClassifyHTTPError(status int, body string) *Error {
	msg := strings.TrimSpace(body)
	if msg == "" {
		msg = http.StatusText(status)
	}
	if len(msg) > 2000 {
		msg = msg[:2000] + "…"
	}
	lower := strings.ToLower(msg)

	kind := KindPermanent
	switch {
	case status == http.StatusTooManyRequests:
		kind = KindRateLimit
	case status >= 500:
		kind = KindUpstream
	case status == http.StatusRequestTimeout:
		kind = KindUpstream
	case status == http.StatusUnprocessableEntity:
		kind = KindPermanent
	}
	for _, hint := range contextHints {
		if strings.Contains(lower, hint) {
			kind = KindContextExceeded
			break
		}
	}
	return &Error{Kind: kind, StatusCode: status, Message: msg}
}

// ParseRetryAfter 解析 Retry-After 头（秒数或 HTTP 日期）。
func ParseRetryAfter(h string) time.Duration {
	h = strings.TrimSpace(h)
	if h == "" {
		return 0
	}
	if secs, err := strconv.Atoi(h); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(h); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// Classify 归一化任意错误。
func Classify(err error) *Error {
	if err == nil {
		return nil
	}
	var pe *Error
	if errors.As(err, &pe) {
		return pe
	}
	if errors.Is(err, context.Canceled) {
		return &Error{Kind: KindCanceled, Message: "canceled"}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &Error{Kind: KindGeneric, Message: err.Error()}
	}
	return &Error{Kind: KindGeneric, Message: err.Error()}
}
