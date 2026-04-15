// Package doubao provides a Volcengine Ark (Doubao) chat client as a dedicated ToolCallingChatModel implementation,
// using the OpenAI-compatible /chat/completions path via agent-sdk-go.
package doubao

import (
	"strings"

	"loopforge/pkg/model"
	"loopforge/pkg/model/adapters/agentsdk"

	"github.com/agentizen/agent-sdk-go/pkg/model/providers/openai"
)

// DefaultBaseURL is the Beijing region Ark OpenAI-compatible API root (no trailing slash).
const DefaultBaseURL = "https://ark.cn-beijing.volces.com/api/v3"

// ArkChatModel is the Doubao (Ark) chat model: OpenAI-compatible HTTP completions through the shared agentsdk stack.
type ArkChatModel struct {
	*agentsdk.SDKChatModel
}

// WithTools returns a new Ark-bound model with tools attached; see ToolCallingChatModel.
func (a *ArkChatModel) WithTools(tools []*model.ToolInfo) (model.ToolCallingChatModel, error) {
	next, err := a.SDKChatModel.WithTools(tools)
	if err != nil {
		return nil, err
	}
	inner := next.(*agentsdk.SDKChatModel)
	return &ArkChatModel{SDKChatModel: inner}, nil
}

// NewArkChatModel returns a ToolCallingChatModel backed by Ark HTTP chat completions.
// baseURL may be empty to use DefaultBaseURL.
func NewArkChatModel(apiKey, baseURL, modelName string) model.ToolCallingChatModel {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultBaseURL
	}
	p := openai.NewProvider(apiKey).SetBaseURL(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	return &ArkChatModel{SDKChatModel: agentsdk.NewSDKChatModel(p, modelName)}
}

var _ model.ToolCallingChatModel = (*ArkChatModel)(nil)
