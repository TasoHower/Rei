package mcp

import (
	"context"
	"testing"

	"github.com/TasoHower/Rei/loopForge/pkg/mcp/cfg"
)

func TestExposedToolName(t *testing.T) {
	if got := ExposedToolName("pre__", "echo"); got != "pre__echo" {
		t.Fatalf("got %q", got)
	}
}

func TestAllowedByAllowlist(t *testing.T) {
	if !AllowedByAllowlist(nil, "a") {
		t.Fatal("nil allowlist should allow")
	}
	if !AllowedByAllowlist([]string{}, "a") {
		t.Fatal("empty allowlist should allow")
	}
	if AllowedByAllowlist([]string{"b"}, "a") {
		t.Fatal("should deny")
	}
	if !AllowedByAllowlist([]string{"a", "b"}, "a") {
		t.Fatal("should allow")
	}
}

func TestBuildMappedToolParts(t *testing.T) {
	parts, err := BuildMappedToolParts("srv1", "p__", "tool1", "d", map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"q": map[string]interface{}{"type": "string"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if parts.Mapped.ServerID != "srv1" || parts.Mapped.MCPToolName != "tool1" || parts.Mapped.ExposedName != "p__tool1" {
		t.Fatalf("mapped: %+v", parts.Mapped)
	}
	if parts.Description != "d" {
		t.Fatalf("description %q", parts.Description)
	}
	if parts.Parameters == nil {
		t.Fatal("params")
	}
}

func TestMappedToolParts_ToToolInfo(t *testing.T) {
	parts := &MappedToolParts{
		Mapped: &cfg.MCPMappedTool{
			ServerID:    "s",
			MCPToolName: "t",
			ExposedName: "pfx__t",
			InputSchema: `{}`,
		},
		Parameters:  map[string]interface{}{"type": "object"},
		Description: "desc",
	}
	h := func(context.Context, string) (string, error) { return "", nil }
	ti := parts.ToToolInfo(h)
	if ti == nil || ti.Name != "pfx__t" || ti.Description != "desc" || ti.Handle == nil {
		t.Fatalf("toolinfo: %+v", ti)
	}
}
