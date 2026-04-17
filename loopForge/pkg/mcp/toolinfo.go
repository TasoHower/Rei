package mcp

import (
	"loopforge/pkg/model/types"
)

// ToToolInfo builds model.ToolInfo from mapped parts. ExposedName comes from Mapped.ExposedName;
// Parameters are already sanitized for Ark via NormalizeInputSchema.
func (p *MappedToolParts) ToToolInfo(handle types.ToolCallHandler) *types.ToolInfo {
	if p == nil || p.Mapped == nil {
		return nil
	}
	return &types.ToolInfo{
		Name:        p.Mapped.ExposedName,
		Description: p.Description,
		Parameters:  p.Parameters,
		Handle:      handle,
	}
}
