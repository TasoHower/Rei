package autoreg

import (
	"encoding/json"
	"reflect"
	"strings"
)

// SchemaFromStruct generates a JSON Schema map from a Go struct type using reflection.
//   - T must be a struct.
//   - Field tags: json:"name,omitempty" for property name / optional, description:"..." for description.
//   - Only exported fields with json tags are included.
//   - Pointers are treated as optional (omitted from "required").
//   - json.RawMessage is treated as {"type": "object"} (not decomposed as []byte).
//   - map[string]interface{} and map[string]json.RawMessage produce {"type": "object"} without additionalProperties.
//   - When "required" would be empty, the key is omitted entirely.
func SchemaFromStruct[T any]() map[string]interface{} {
	var zero T
	t := reflect.TypeOf(zero)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	props, req := collectProperties(t, map[reflect.Type]bool{})
	schema := map[string]interface{}{
		"type": "object",
	}
	if len(props) > 0 {
		schema["properties"] = props
	}
	if len(req) > 0 {
		schema["required"] = req
	}
	return schema
}

func collectProperties(t reflect.Type, visited map[reflect.Type]bool) (map[string]interface{}, []string) {
	if t.Kind() != reflect.Struct {
		return nil, nil
	}
	if visited[t] {
		return nil, nil
	}
	visited[t] = true

	props := make(map[string]interface{})
	var req []string

	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if name == "" {
			name = f.Name
		}
		hasOmitEmpty := opts == "omitempty"

		propSchema := typeToSchema(f.Type, visited)
		if desc := f.Tag.Get("description"); desc != "" {
			propSchema["description"] = desc
		}
		props[name] = propSchema

		if !hasOmitEmpty && !isPointer(f.Type) && !isRawMessage(f.Type) {
			req = append(req, name)
		}
	}

	return props, req
}

func typeToSchema(t reflect.Type, visited map[reflect.Type]bool) map[string]interface{} {
	if isRawMessage(t) {
		return map[string]interface{}{"type": "object"}
	}
	if isPointer(t) {
		return typeToSchema(t.Elem(), visited)
	}

	switch t.Kind() {
	case reflect.String:
		return map[string]interface{}{"type": "string"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return map[string]interface{}{"type": "integer"}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]interface{}{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]interface{}{"type": "number"}
	case reflect.Bool:
		return map[string]interface{}{"type": "boolean"}
	case reflect.Slice:
		elem := t.Elem()
		if isRawMessage(elem) {
			return map[string]interface{}{"type": "array"}
		}
		return map[string]interface{}{
			"type":  "array",
			"items": typeToSchema(elem, visited),
		}
	case reflect.Map:
		if isUnconstrainedMap(t) {
			return map[string]interface{}{"type": "object"}
		}
		return map[string]interface{}{
			"type":                 "object",
			"additionalProperties": typeToSchema(t.Elem(), visited),
		}
	case reflect.Struct:
		childProps, _ := collectProperties(t, visited)
		s := map[string]interface{}{"type": "object"}
		if len(childProps) > 0 {
			s["properties"] = childProps
		}
		return s
	default:
		return map[string]interface{}{"type": "string"}
	}
}

func isPointer(t reflect.Type) bool {
	return t.Kind() == reflect.Ptr
}

var rawMessageType = reflect.TypeOf(json.RawMessage(nil))

func isRawMessage(t reflect.Type) bool {
	return t == rawMessageType
}

func isUnconstrainedMap(t reflect.Type) bool {
	if t.Kind() != reflect.Map {
		return false
	}
	elem := t.Elem()
	if elem.Kind() == reflect.Interface {
		return true
	}
	return isRawMessage(elem)
}
