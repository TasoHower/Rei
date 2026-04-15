package mcpserver

// ptrBool returns a non-nil *bool for MCP ToolAnnotations fields that require pointers.
func ptrBool(b bool) *bool { return &b }
