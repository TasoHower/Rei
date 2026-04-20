package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSKILLFile_ok(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "SKILL.md")
	content := "---\nname: demo-skill\ndescription: A demo skill for tests.\nversion: \"1.0.0\"\nallowed-tools: Read, Write\n---\n\n# Body\n\nHello.\n"
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	spec, err := ParseSKILLFile(p)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if spec.Name != "demo-skill" {
		t.Fatalf("name: got %q", spec.Name)
	}
	if spec.Version != "1.0.0" {
		t.Fatalf("version: got %q", spec.Version)
	}
	if len(spec.AllowedTools) != 2 || spec.AllowedTools[0] != "Read" || spec.AllowedTools[1] != "Write" {
		t.Fatalf("allowed-tools: %#v", spec.AllowedTools)
	}
	if spec.Body == "" || !strings.Contains(spec.Body, "Hello.") {
		t.Fatalf("body: %q", spec.Body)
	}
	if len(spec.ContentHash) != 64 {
		t.Fatalf("content hash: %q", spec.ContentHash)
	}
}

func TestParseSKILLFile_invalidName(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "SKILL.md")
	content := "---\nname: bad name\ndescription: x\n---\n\n"
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := ParseSKILLFile(p)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadFromPaths_firstWins(t *testing.T) {
	ctx := t.Context()
	base := t.TempDir()
	a := filepath.Join(base, "a", "SKILL.md")
	b := filepath.Join(base, "b", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(a), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(b), 0o700); err != nil {
		t.Fatal(err)
	}
	ca := "---\nname: same\ndescription: first\n---\n\nFirst body\n"
	cb := "---\nname: same\ndescription: second\n---\n\nSecond body\n"
	if err := os.WriteFile(a, []byte(ca), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte(cb), 0o600); err != nil {
		t.Fatal(err)
	}

	reg := NewRegistry()
	if err := reg.LoadFromPaths(ctx, []string{filepath.Join(base, "a"), filepath.Join(base, "b")}); err != nil {
		t.Fatal(err)
	}
	sp, err := reg.Get("same")
	if err != nil {
		t.Fatal(err)
	}
	if sp.Description != "first" {
		t.Fatalf("expected first description, got %q", sp.Description)
	}
	if !strings.Contains(sp.Body, "First body") {
		t.Fatalf("expected first body: %q", sp.Body)
	}
}
