package variable

import (
	"context"
	"testing"
)

func TestVarSetToolConst(t *testing.T) {
	s := New()
	ti := VarSetTool(s)
	_, err := ti.Handle(context.Background(), `{"key":"const_x","value":1}`)
	if err == nil {
		t.Fatal("expected error")
	}
	out, err := ti.Handle(context.Background(), `{"key":"y","value":"ok"}`)
	if err != nil || out != "ok" {
		t.Fatalf("got %q %v", out, err)
	}
	v, _ := s.Get("y")
	if v != "ok" {
		t.Fatalf("value %v", v)
	}
}

func TestVarSetToolUpdatesMulti(t *testing.T) {
	s := New()
	ti := VarSetTool(s)
	out, err := ti.Handle(context.Background(), `{"updates":{"a":1,"b":"two","c":null}}`)
	if err != nil || out != "ok" {
		t.Fatalf("got %q %v", out, err)
	}
	va, _ := s.Get("a")
	if va != float64(1) {
		t.Fatalf("a=%v", va)
	}
	vb, _ := s.Get("b")
	if vb != "two" {
		t.Fatalf("b=%v", vb)
	}
	vc, ok := s.Get("c")
	if !ok || vc != nil {
		t.Fatalf("c=%v ok=%v", vc, ok)
	}
}

func TestVarSetToolUpdatesConstRejected(t *testing.T) {
	s := New()
	ti := VarSetTool(s)
	_, err := ti.Handle(context.Background(), `{"updates":{"ok":1,"const_x":2}}`)
	if err == nil {
		t.Fatal("expected error")
	}
	v, ok := s.Get("ok")
	if ok && v != nil {
		t.Fatal("expected ok not committed before const failure")
	}
}
