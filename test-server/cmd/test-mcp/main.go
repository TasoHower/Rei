package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"sync/atomic"
)

var reqID atomic.Int64

func nextID() int64 {
	return reqID.Add(1)
}

type jsonrpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonrpcResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      any         `json:"id,omitempty"`
	Result  any         `json:"result,omitempty"`
	Error   *rpcError   `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type toolDefinition struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema any         `json:"inputSchema"`
}

var tools = []toolDefinition{
	{
		Name:        "add",
		Description: "Add two numbers",
		InputSchema: schemaForArgs("a", "b"),
	},
	{
		Name:        "subtract",
		Description: "Subtract b from a",
		InputSchema: schemaForArgs("a", "b"),
	},
	{
		Name:        "multiply",
		Description: "Multiply two numbers",
		InputSchema: schemaForArgs("a", "b"),
	},
	{
		Name:        "divide",
		Description: "Divide a by b",
		InputSchema: schemaForArgs("a", "b"),
	},
}

func schemaForArgs(argNames ...string) map[string]any {
	props := make(map[string]any)
	required := make([]string, 0, len(argNames))
	for _, n := range argNames {
		props[n] = map[string]any{
			"type":        "number",
			"description": fmt.Sprintf("The %s operand", n),
		}
		required = append(required, n)
	}
	return map[string]any{
		"type":       "object",
		"properties": props,
		"required":   required,
	}
}

func handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var req jsonrpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, nil, -32700, "Parse error")
		return
	}

	switch req.Method {
	case "initialize":
		handleInitialize(w, &req)
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
	case "tools/list":
		handleToolsList(w, &req)
	case "tools/call":
		handleToolsCall(w, &req, body)
	default:
		writeError(w, req.ID, -32601, fmt.Sprintf("Method not found: %s", req.Method))
	}
}

func handleInitialize(w http.ResponseWriter, req *jsonrpcRequest) {
	writeResult(w, req.ID, map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    "test-mcp",
			"version": "1.0.0",
		},
	})
}

func handleToolsList(w http.ResponseWriter, req *jsonrpcRequest) {
	writeResult(w, req.ID, map[string]any{
		"tools": tools,
	})
}

func handleToolsCall(w http.ResponseWriter, req *jsonrpcRequest, rawBody []byte) {
	var callReq struct {
		Params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		} `json:"params"`
	}
	if err := json.Unmarshal(rawBody, &callReq); err != nil {
		writeError(w, req.ID, -32602, "Invalid params")
		return
	}

	name := callReq.Params.Name
	args := callReq.Params.Arguments

	a, aOK := toFloat64(args["a"])
	b, bOK := toFloat64(args["b"])
	if !aOK || !bOK {
		writeToolError(w, req.ID, fmt.Sprintf("Invalid arguments for %s: a and b must be numbers", name))
		return
	}

	var result float64
	switch name {
	case "add":
		result = a + b
	case "subtract":
		result = a - b
	case "multiply":
		result = a * b
	case "divide":
		if b == 0 {
			writeToolError(w, req.ID, "Division by zero")
			return
		}
		result = a / b
	default:
		writeError(w, req.ID, -32601, fmt.Sprintf("Tool not found: %s", name))
		return
	}

	text := formatNumber(result)
	writeResult(w, req.ID, map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": text},
		},
	})
}

func toFloat64(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case json.Number:
		f, err := val.Float64()
		return f, err == nil
	}
	return 0, false
}

func formatNumber(f float64) string {
	if f == math.Trunc(f) && !math.IsInf(f, 0) && math.Abs(f) < 1e15 {
		return fmt.Sprintf("%.0f", f)
	}
	return fmt.Sprintf("%g", f)
}

func writeResult(w http.ResponseWriter, id any, result any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(jsonrpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	})
}

func writeError(w http.ResponseWriter, id any, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(jsonrpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: msg},
	})
}

func writeToolError(w http.ResponseWriter, id any, msg string) {
	writeResult(w, id, map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": msg},
		},
		"isError": true,
	})
}

func main() {
	addr := os.Getenv("TEST_MCP_ADDR")
	if addr == "" {
		addr = ":9089"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", handleMCP)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	log.Printf("test-mcp starting on %s/mcp", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("test-mcp: %v", err)
	}
}
