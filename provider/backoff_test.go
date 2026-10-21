package provider

import (
	"context"
	"testing"
	"time"
)

func TestBackoffDelayGrowsAndCaps(t *testing.T) {
	b := Backoff{Min: 100 * time.Millisecond, Max: 400 * time.Millisecond}
	prev := time.Duration(0)
	for attempt := 1; attempt <= 5; attempt++ {
		d := b.Delay(attempt)
		if d < prev && d != b.Max {
			t.Fatalf("第 %d 次延迟 %s 小于上一次 %s", attempt, d, prev)
		}
		if d > b.Max {
			t.Fatalf("第 %d 次延迟 %s 超过上限 %s", attempt, d, b.Max)
		}
		prev = d
	}
	if got := b.Delay(10); got != b.Max {
		t.Fatalf("封顶值 = %s，期望 %s", got, b.Max)
	}
}

func TestBackoffJitterStaysInRange(t *testing.T) {
	b := DefaultBackoff()
	for attempt := 1; attempt <= 4; attempt++ {
		base := Backoff{Min: b.Min, Max: b.Max}.Delay(attempt)
		d := b.Delay(attempt)
		if d < base/2 || d > base {
			t.Fatalf("抖动后的延迟 %s 超出 [%s, %s]", d, base/2, base)
		}
	}
}

func TestNoJitterBackoffSleepCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := NoJitter().Sleep(ctx, 1); err == nil {
		t.Fatal("已取消的 ctx 应当返回错误")
	}
}
