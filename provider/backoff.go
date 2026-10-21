package provider

import (
	"context"
	"math/rand"
	"time"
)

// Backoff 是指数退避策略。重试次数与上下限都可配置，
// 抖动默认是「半区间随机」：避免多个会话同时重试造成二次拥塞。
type Backoff struct {
	Min        time.Duration
	Max        time.Duration
	MaxRetries int
	// Jitter 可选：输入基础延迟，返回抖动后的延迟。
	Jitter func(time.Duration) time.Duration
}

// DefaultBackoff：500ms 起，15s 封顶，最多重试 5 次。
func DefaultBackoff() Backoff {
	return Backoff{Min: 500 * time.Millisecond, Max: 15 * time.Second, MaxRetries: 5, Jitter: halfJitter}
}

// NoJitter 返回确定性退避，便于测试。
func NoJitter() Backoff {
	return Backoff{Min: 10 * time.Millisecond, Max: 100 * time.Millisecond, MaxRetries: 3, Jitter: nil}
}

func halfJitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	half := int64(d / 2)
	return time.Duration(half + rand.Int63n(half+1))
}

// Delay 返回第 attempt 次重试（从 1 开始）前应等待的时间。
func (b Backoff) Delay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	min := b.Min
	if min <= 0 {
		min = 200 * time.Millisecond
	}
	max := b.Max
	if max <= 0 {
		max = 15 * time.Second
	}
	d := min
	for i := 1; i < attempt; i++ {
		d *= 2
		if d >= max {
			d = max
			break
		}
	}
	if d > max {
		d = max
	}
	if b.Jitter != nil {
		d = b.Jitter(d)
	}
	return d
}

// Sleep 等待退避时间，并感知 ctx 取消。
func (b Backoff) Sleep(ctx context.Context, attempt int) error {
	timer := time.NewTimer(b.Delay(attempt))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
