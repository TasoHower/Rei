package autoreg

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestSchemaFromStruct_Basic(t *testing.T) {
	type S struct {
		Name string `json:"name" description:"The name"`
		Age  int    `json:"age"`
	}
	s := SchemaFromStruct[S]()
	if s["type"] != "object" {
		t.Errorf("expected type=object, got %v", s["type"])
	}
	props, ok := s["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("properties missing or wrong type")
	}
	nameProp := props["name"].(map[string]interface{})
	if nameProp["type"] != "string" {
		t.Errorf("name.type expected string, got %v", nameProp["type"])
	}
	if nameProp["description"] != "The name" {
		t.Errorf("name.description expected 'The name', got %v", nameProp["description"])
	}
	ageProp := props["age"].(map[string]interface{})
	if ageProp["type"] != "integer" {
		t.Errorf("age.type expected integer, got %v", ageProp["type"])
	}
	req, ok := s["required"].([]string)
	if !ok {
		t.Fatal("required missing or wrong type")
	}
	if len(req) != 2 || req[0] != "name" || req[1] != "age" {
		t.Errorf("required expected [name age], got %v", req)
	}
}

func TestSchemaFromStruct_Optional(t *testing.T) {
	type S struct {
		Name string `json:"name,omitempty"`
	}
	s := SchemaFromStruct[S]()
	_, has := s["required"]
	if has {
		t.Errorf("expected no required key when all fields optional, got %v", s["required"])
	}
}

func TestSchemaFromStruct_Pointer(t *testing.T) {
	type S struct {
		Count *int `json:"count" description:"Optional count"`
	}
	s := SchemaFromStruct[S]()
	props := s["properties"].(map[string]interface{})
	countProp := props["count"].(map[string]interface{})
	if countProp["type"] != "integer" {
		t.Errorf("count.type expected integer, got %v", countProp["type"])
	}
	_, has := s["required"]
	if has {
		t.Errorf("expected no required key when field is pointer, got %v", s["required"])
	}
}

func TestSchemaFromStruct_Nested(t *testing.T) {
	type Inner struct {
		Value string `json:"value"`
	}
	type Outer struct {
		Inner Inner `json:"inner"`
	}
	s := SchemaFromStruct[Outer]()
	props := s["properties"].(map[string]interface{})
	innerProp := props["inner"].(map[string]interface{})
	if innerProp["type"] != "object" {
		t.Errorf("inner.type expected object, got %v", innerProp["type"])
	}
	innerProps := innerProp["properties"].(map[string]interface{})
	valueProp := innerProps["value"].(map[string]interface{})
	if valueProp["type"] != "string" {
		t.Errorf("inner.value.type expected string, got %v", valueProp["type"])
	}
}

func TestSchemaFromStruct_Slice(t *testing.T) {
	type S struct {
		Items []string `json:"items"`
	}
	s := SchemaFromStruct[S]()
	props := s["properties"].(map[string]interface{})
	itemsProp := props["items"].(map[string]interface{})
	if itemsProp["type"] != "array" {
		t.Errorf("items.type expected array, got %v", itemsProp["type"])
	}
	items := itemsProp["items"].(map[string]interface{})
	if items["type"] != "string" {
		t.Errorf("items.items.type expected string, got %v", items["type"])
	}
}

func TestSchemaFromStruct_Map(t *testing.T) {
	type S struct {
		Labels map[string]string `json:"labels"`
	}
	s := SchemaFromStruct[S]()
	props := s["properties"].(map[string]interface{})
	labelsProp := props["labels"].(map[string]interface{})
	if labelsProp["type"] != "object" {
		t.Errorf("labels.type expected object, got %v", labelsProp["type"])
	}
	ap := labelsProp["additionalProperties"].(map[string]interface{})
	if ap["type"] != "string" {
		t.Errorf("labels.additionalProperties.type expected string, got %v", ap["type"])
	}
}

func TestSchemaFromStruct_MapStringInterface(t *testing.T) {
	type S struct {
		Data map[string]interface{} `json:"data"`
	}
	s := SchemaFromStruct[S]()
	props := s["properties"].(map[string]interface{})
	dataProp := props["data"].(map[string]interface{})
	if dataProp["type"] != "object" {
		t.Errorf("data.type expected object, got %v", dataProp["type"])
	}
	_, has := dataProp["additionalProperties"]
	if has {
		t.Errorf("expected no additionalProperties for map[string]interface{}, got %v", dataProp["additionalProperties"])
	}
}

func TestSchemaFromStruct_JSONRawMessage(t *testing.T) {
	type S struct {
		Data json.RawMessage `json:"data"`
	}
	s := SchemaFromStruct[S]()
	props := s["properties"].(map[string]interface{})
	dataProp := props["data"].(map[string]interface{})
	if dataProp["type"] != "object" {
		t.Errorf("data.type expected object for json.RawMessage, got %v", dataProp["type"])
	}
}

func TestSchemaFromStruct_EmptyRequired(t *testing.T) {
	type S struct {
		Name string `json:"name,omitempty"`
	}
	s := SchemaFromStruct[S]()
	_, has := s["required"]
	if has {
		t.Errorf("expected no required key when empty, got %v", s["required"])
	}
}

func TestSchemaFromStruct_TypeAlias(t *testing.T) {
	type MyStr string
	type S struct {
		Value MyStr `json:"value"`
	}
	s := SchemaFromStruct[S]()
	props := s["properties"].(map[string]interface{})
	valueProp := props["value"].(map[string]interface{})
	if valueProp["type"] != "string" {
		t.Errorf("value.type expected string for type alias, got %v", valueProp["type"])
	}
}

func TestSchemaFromStruct_Float(t *testing.T) {
	type S struct {
		Price float64 `json:"price"`
	}
	s := SchemaFromStruct[S]()
	props := s["properties"].(map[string]interface{})
	priceProp := props["price"].(map[string]interface{})
	if priceProp["type"] != "number" {
		t.Errorf("price.type expected number, got %v", priceProp["type"])
	}
}

func TestSchemaFromStruct_Bool(t *testing.T) {
	type S struct {
		Enabled bool `json:"enabled"`
	}
	s := SchemaFromStruct[S]()
	props := s["properties"].(map[string]interface{})
	enabledProp := props["enabled"].(map[string]interface{})
	if enabledProp["type"] != "boolean" {
		t.Errorf("enabled.type expected boolean, got %v", enabledProp["type"])
	}
}

func TestNewToolFromStruct(t *testing.T) {
	type AddParams struct {
		A float64 `json:"a"`
		B float64 `json:"b"`
	}
	tool := NewToolFromStruct("add", "Adds two numbers",
		func(ctx context.Context, p AddParams) (string, error) {
			return fmt.Sprintf("%f", p.A+p.B), nil
		},
	)
	if tool.Name != "add" {
		t.Errorf("Name expected 'add', got %q", tool.Name)
	}
	if tool.Description != "Adds two numbers" {
		t.Errorf("Description expected 'Adds two numbers', got %q", tool.Description)
	}
	if tool.Parameters["type"] != "object" {
		t.Errorf("Parameters.type expected object, got %v", tool.Parameters["type"])
	}
	if tool.Handle == nil {
		t.Fatal("Handle should not be nil")
	}
	result, err := tool.Handle(context.Background(), `{"a":3,"b":5}`)
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if !strings.Contains(result, "8.000000") {
		t.Errorf("Handle result expected 8.000000, got %q", result)
	}
}

func TestNewToolFromStruct_WithDeprecatedParameters(t *testing.T) {
	type AddParams struct {
		A float64 `json:"a"`
		B float64 `json:"b"`
	}
	customParams := map[string]interface{}{"type": "object"}
	tool := NewToolFromStruct("add", "Adds two numbers",
		func(ctx context.Context, p AddParams) (string, error) {
			return fmt.Sprintf("%f", p.A+p.B), nil
		},
		WithParameters(customParams),
	)
	// Parameters should be overridden by WithParameters
	if tool.Parameters["type"] != "object" {
		t.Errorf("Parameters.type expected object, got %v", tool.Parameters["type"])
	}
	// Handle should still work correctly
	result, err := tool.Handle(context.Background(), `{"a":3,"b":5}`)
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if !strings.Contains(result, "8.000000") {
		t.Errorf("Handle result expected 8.000000, got %q", result)
	}
}

func TestSchemaFromStruct_MapRawMessage(t *testing.T) {
	type S struct {
		Updates map[string]json.RawMessage `json:"updates"`
	}
	s := SchemaFromStruct[S]()
	props := s["properties"].(map[string]interface{})
	updProp := props["updates"].(map[string]interface{})
	if updProp["type"] != "object" {
		t.Errorf("updates.type expected object, got %v", updProp["type"])
	}
	_, has := updProp["additionalProperties"]
	if has {
		t.Errorf("expected no additionalProperties for map[string]json.RawMessage, got %v", updProp["additionalProperties"])
	}
}

func TestSchemaFromStruct_CycleRef(t *testing.T) {
	type Node struct {
		Name string `json:"name"`
		Next *Node  `json:"next,omitempty"`
	}
	s := SchemaFromStruct[Node]()
	props := s["properties"].(map[string]interface{})
	nameProp := props["name"].(map[string]interface{})
	if nameProp["type"] != "string" {
		t.Errorf("name.type expected string, got %v", nameProp["type"])
	}
	nextProp := props["next"].(map[string]interface{})
	if nextProp["type"] != "object" {
		t.Errorf("next.type expected object, got %v", nextProp["type"])
	}
	// Cycle detection: next should be an object, but next.next should NOT have a "next" field
	// (cycle detection stops recursion so the inner struct has no "next" property)
	nextProps, hasProps := nextProp["properties"].(map[string]interface{})
	if hasProps {
		if _, exists := nextProps["next"]; exists {
			t.Errorf("expected no next.next (cycle detection should stop at 1 level)")
		}
	}
}

func TestSchemaFromStruct_HiddenField(t *testing.T) {
	type S struct {
		Name      string `json:"name"`
		Internal  string `json:"-"`
		unexported string
	}
	s := SchemaFromStruct[S]()
	props := s["properties"].(map[string]interface{})
	if _, exists := props["internal"]; exists {
		t.Errorf("expected no internal property for json:\"-\" field")
	}
	if _, exists := props["unexported"]; exists {
		t.Errorf("expected no unexported property")
	}
}
