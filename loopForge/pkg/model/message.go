package model

import "github.com/TasoHower/rei/loopForge/pkg/model/types"

type Role = types.Role

const (
	RoleUser      = types.RoleUser
	RoleAssistant = types.RoleAssistant
	RoleSystem    = types.RoleSystem
	RoleTool      = types.RoleTool
)

type ToolCallPart = types.ToolCallPart
type Message = types.Message

// ToolCallHandler binds local execution for a function tool (see types.ToolCallHandler).
type ToolCallHandler = types.ToolCallHandler
