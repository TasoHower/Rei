package variable

import (
	"errors"
	"testing"
)

func TestGetTyped(t *testing.T) {
	s := New()
	s.Set("count", 42)
	s.Set("name", "alice")
	s.Set("flag", true)

	if v, ok := Get[int](s, "count"); !ok || v != 42 {
		t.Fatalf("Get[int]: got %v %v", v, ok)
	}
	if v, ok := Get[string](s, "name"); !ok || v != "alice" {
		t.Fatalf("Get[string]: got %v %v", v, ok)
	}
	if v, ok := Get[bool](s, "flag"); !ok || v != true {
		t.Fatalf("Get[bool]: got %v %v", v, ok)
	}
}

func TestGetTypedMismatch(t *testing.T) {
	s := New()
	s.Set("count", 42)
	// Type mismatch should return zero + false
	if _, ok := Get[string](s, "count"); ok {
		t.Fatal("expected type mismatch to return false")
	}
}

func TestGetTypedMissingKey(t *testing.T) {
	s := New()
	if _, ok := Get[string](s, "missing"); ok {
		t.Fatal("expected missing key to return false")
	}
}

func TestGetTypedNilStore(t *testing.T) {
	if _, ok := Get[string](nil, "k"); ok {
		t.Fatal("expected nil store to return false")
	}
}

func TestGetRequiredError(t *testing.T) {
	s := New()
	_, err := GetRequired[string](s, "no_such_key")
	if err == nil {
		t.Fatal("expected error for missing key")
	}
	if !errors.Is(err, ErrNotPresent) {
		t.Fatalf("expected ErrNotPresent: %v", err)
	}
}

func TestGetRequiredOK(t *testing.T) {
	s := New()
	s.Set("greeting", "hello")
	v, err := GetRequired[string](s, "greeting")
	if err != nil {
		t.Fatal(err)
	}
	if v != "hello" {
		t.Fatalf("got %q", v)
	}
}
