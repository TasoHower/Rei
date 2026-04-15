package agentsdk

import (
	"context"
	"fmt"

	lpmodel "loopforge/pkg/model"

	sdkmodel "github.com/agentizen/agent-sdk-go/pkg/model"
)

// SDKChatModel implements lpmodel.ToolCallingChatModel using agent-sdk-go model.Provider + model.Model.
type SDKChatModel struct {
	provider  sdkmodel.Provider
	modelName string
	tools     []*lpmodel.ToolInfo
}

// NewSDKChatModel builds a chat model bound to provider and logical model name (e.g. gpt-4o-mini, or Ark endpoint id).
func NewSDKChatModel(provider sdkmodel.Provider, modelName string) *SDKChatModel {
	return &SDKChatModel{
		provider:  provider,
		modelName: modelName,
	}
}

// WithTools returns a new instance with tools bound (immutable pattern, compare eino ToolCallingChatModel).
func (c *SDKChatModel) WithTools(tools []*lpmodel.ToolInfo) (lpmodel.ToolCallingChatModel, error) {
	dup := make([]*lpmodel.ToolInfo, len(tools))
	copy(dup, tools)
	return &SDKChatModel{
		provider:  c.provider,
		modelName: c.modelName,
		tools:     dup,
	}, nil
}

// Generate performs a single non-streaming completion.
func (c *SDKChatModel) Generate(ctx context.Context, input []*lpmodel.Message, opts ...lpmodel.CallOption) (*lpmodel.Message, error) {
	cfg := lpmodel.ApplyCallOptions(opts...)
	name := c.modelName
	if cfg.ModelOverride != "" {
		name = cfg.ModelOverride
	}
	m, err := c.provider.GetModel(name)
	if err != nil {
		return nil, err
	}
	effectiveTools := mergeToolLists(c.tools, cfg.Tools)
	req, err := MessagesToSDKRequest(input, toolsToIface(effectiveTools), CallConfigToSDKSettings(cfg))
	if err != nil {
		return nil, err
	}
	resp, err := m.GetResponse(ctx, req)
	if err != nil {
		return nil, err
	}
	return FromSDKResponse(resp), nil
}

// Stream forwards SDK stream events: each text delta is one Recv (assistant message with that fragment).
// After deltas, if the model returned no streamed text, the final merged assistant message is sent once.
func (c *SDKChatModel) Stream(ctx context.Context, input []*lpmodel.Message, opts ...lpmodel.CallOption) (lpmodel.MessageStreamReader, error) {
	cfg := lpmodel.ApplyCallOptions(opts...)
	name := c.modelName
	if cfg.ModelOverride != "" {
		name = cfg.ModelOverride
	}
	m, err := c.provider.GetModel(name)
	if err != nil {
		return nil, err
	}
	effectiveTools := mergeToolLists(c.tools, cfg.Tools)
	req, err := MessagesToSDKRequest(input, toolsToIface(effectiveTools), CallConfigToSDKSettings(cfg))
	if err != nil {
		return nil, err
	}
	eventCh, err := m.StreamResponse(ctx, req)
	if err != nil {
		return nil, err
	}

	out := make(chan streamPart, 64)
	go func() {
		defer close(out)
		var hadDelta bool
		var final *sdkmodel.Response
		for ev := range eventCh {
			if ev.Error != nil {
				out <- streamPart{err: ev.Error}
				return
			}
			switch ev.Type {
			case sdkmodel.StreamEventTypeContent:
				if ev.Content != "" {
					hadDelta = true
					out <- streamPart{
						msg: &lpmodel.Message{
							Role:    lpmodel.RoleAssistant,
							Content: ev.Content,
						},
					}
				}
			case sdkmodel.StreamEventTypeDone:
				if ev.Response != nil {
					final = ev.Response
				}
			}
		}
		if final != nil {
			finalMsg := FromSDKResponse(final)
			if hadDelta {
				// Text was already streamed as deltas; clear Content to avoid
				// duplication while preserving ToolCalls and Usage.
				finalMsg.Content = ""
			}
			out <- streamPart{msg: finalMsg}
		} else if !hadDelta {
			out <- streamPart{err: fmt.Errorf("stream ended without content or final response")}
		}
	}()

	return newChanStreamReader(out), nil
}

var (
	_ lpmodel.BaseChatModel        = (*SDKChatModel)(nil)
	_ lpmodel.ToolCallingChatModel = (*SDKChatModel)(nil)
)
