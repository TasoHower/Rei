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
