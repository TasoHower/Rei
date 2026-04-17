package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type binaryArgs struct {
	A float64 `json:"a" jsonschema:"First operand."`
	B float64 `json:"b" jsonschema:"Second operand."`
}

func main() {
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":9089"
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "test-mcp", Version: "0.1.0"}, &mcp.ServerOptions{})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "add",
		Description: "Return a + b.",
	}, addHandler())

	mcp.AddTool(server, &mcp.Tool{
		Name:        "subtract",
		Description: "Return a - b.",
	}, subtractHandler())

	mcp.AddTool(server, &mcp.Tool{
		Name:        "multiply",
		Description: "Return a * b.",
	}, multiplyHandler())

	mcp.AddTool(server, &mcp.Tool{
		Name:        "divide",
		Description: "Return a / b. Error when b is zero.",
	}, divideHandler())

	// Stateless avoids "session not found" (HTTP 404) when a client sends
	// Mcp-Session-Id that is not in the handler map; loopForge may open a new
	// streamable connection per agent bootstrap while reusing the same URL.
	handler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		Stateless: true,
	})
	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	log.Printf("test-mcp streamable_http listening on %s (path /mcp)", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func addHandler() mcp.ToolHandlerFor[binaryArgs, any] {
	return func(_ context.Context, _ *mcp.CallToolRequest, args binaryArgs) (*mcp.CallToolResult, any, error) {
		return textResult(fmt.Sprintf("%.6g", args.A+args.B))
	}
}

func subtractHandler() mcp.ToolHandlerFor[binaryArgs, any] {
	return func(_ context.Context, _ *mcp.CallToolRequest, args binaryArgs) (*mcp.CallToolResult, any, error) {
		return textResult(fmt.Sprintf("%.6g", args.A-args.B))
	}
}

func multiplyHandler() mcp.ToolHandlerFor[binaryArgs, any] {
	return func(_ context.Context, _ *mcp.CallToolRequest, args binaryArgs) (*mcp.CallToolResult, any, error) {
		return textResult(fmt.Sprintf("%.6g", args.A*args.B))
	}
}

func divideHandler() mcp.ToolHandlerFor[binaryArgs, any] {
	return func(_ context.Context, _ *mcp.CallToolRequest, args binaryArgs) (*mcp.CallToolResult, any, error) {
		if args.B == 0 {
			return errResult("division by zero"), nil, nil
		}
		out := math.Round(args.A/args.B*1e6) / 1e6
		return textResult(fmt.Sprintf("%.6g", out))
	}
}

func textResult(s string) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: s}},
	}, nil, nil
}

func errResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
		IsError: true,
	}
}
