package mcp

import (
	"encoding/json"
	"testing"
)

func TestParseInputSchema_nil(t *testing.T) {
	m, err := ParseInputSchema(nil)
	if err != nil {
		t.Fatal(err)
	}
	if m["type"] != "object" {
		t.Fatalf("got %#v", m)
	}
}

func TestParseInputSchema_string(t *testing.T) {
	raw := `{"type":"object","properties":{"q":{"type":"string"}},"additionalProperties":false}`
	m, err := ParseInputSchema(raw)
	if err != nil {
		t.Fatal(err)
	}
	if m["type"] != "object" {
		t.Fatalf("got %#v", m)
	}
}

func TestNormalizeInputSchema_stripsAdditionalProperties(t *testing.T) {
	raw := map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"x": map[string]interface{}{},
		},
	}
	m, err := NormalizeInputSchema(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m["additionalProperties"]; ok {
		t.Fatalf("additionalProperties should be stripped, got %#v", m)
	}
}

func TestParseInputSchema_jsonRawMessage(t *testing.T) {
	b := json.RawMessage(`{"type":"object"}`)
	m, err := ParseInputSchema(b)
	if err != nil {
		t.Fatal(err)
	}
	if m["type"] != "object" {
		t.Fatalf("got %#v", m)
	}
}
