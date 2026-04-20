package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScriptPathUnderBundle_ok(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "scripts", "x.sh")
	if err := os.MkdirAll(filepath.Dir(script), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho ok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := scriptPathUnderBundle(dir, "scripts/x.sh")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(p) != "x.sh" {
		t.Fatalf("got %s", p)
	}
}

func TestScriptPathUnderBundle_escape(t *testing.T) {
	dir := t.TempDir()
	_, err := scriptPathUnderBundle(dir, "../x.sh")
	if err == nil {
		t.Fatal("expected error")
	}
}
