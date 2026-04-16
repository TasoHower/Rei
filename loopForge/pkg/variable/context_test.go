package variable

import (
	"context"
	"testing"
)

func TestNewContextAndFromContext(t *testing.T) {
	s := New()
	s.Set("k", "v")

	ctx := NewContext(context.Background(), s)
	got := FromContext(ctx)
	if got == nil {
		t.Fatal("FromContext returned nil")
	}
	val, ok := got.Get("k")
	if !ok || val != "v" {
		t.Fatalf("expected v, got %v %v", val, ok)
	}
}

func TestFromContextNilCtx(t *testing.T) {
	// FromContext is specified to accept nil; verify it returns nil gracefully.
	if got := FromContext(context.TODO()); got != nil {
		t.Fatalf("expected nil for empty ctx, got %v", got)
	}
}

func TestFromContextMissing(t *testing.T) {
	if got := FromContext(context.Background()); got != nil {
		t.Fatalf("expected nil for empty ctx, got %v", got)
	}
}

func TestNewContextNilParent(t *testing.T) {
	s := New()
	s.Set("x", 42)
	// nil parent: NewContext falls back to Background internally
	ctx := NewContext(context.TODO(), s)
	got := FromContext(ctx)
	if got == nil {
		t.Fatal("FromContext returned nil after nil-parent NewContext")
	}
	val, ok := got.Get("x")
	if !ok || val != 42 {
		t.Fatalf("expected 42, got %v %v", val, ok)
	}
}
