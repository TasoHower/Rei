// Package lark implements Volcengine Ark (Lark LLM) chat via the official
// github.com/volcengine/volcengine-go-sdk (arkruntime).
package lark

import (
	"context"
	"strings"

	lpmodel "github.com/TasoHower/rei/loopForge/pkg/model"

	"github.com/volcengine/volcengine-go-sdk/service/arkruntime"
	arkmodel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
)

// DefaultBaseURL is the Beijing region Ark API v3 base URL (no trailing slash).
const DefaultBaseURL = "https://ark.cn-beijing.volces.com/api/v3"

// LarkChatModel implements ToolCallingChatModel using arkruntime HTTP clients.
type LarkChatModel struct {
	client    *arkruntime.Client
	modelName string
	tools     []*lpmodel.ToolInfo
}

// WithTools returns a new instance with tools bound; the receiver is not mutated.
func (a *LarkChatModel) WithTools(tools []*lpmodel.ToolInfo) (lpmodel.ToolCallingChatModel, error) {
	dup := make([]*lpmodel.ToolInfo, len(tools))
	copy(dup, tools)
	return &LarkChatModel{
		client:    a.client,
		modelName: a.modelName,
		tools:     dup,
	}, nil
}

// Generate performs a single non-streaming completion.
func (a *LarkChatModel) Generate(ctx context.Context, input []*lpmodel.Message, opts ...lpmodel.CallOption) (*lpmodel.Message, error) {
	cfg := lpmodel.ApplyCallOptions(opts...)
	name := a.modelName
	if cfg.ModelOverride != "" {
		name = cfg.ModelOverride
	}
	msgs, err := toArkMessages(input)
	if err != nil {
		return nil, err
	}
	effectiveTools := mergeToolLists(a.tools, cfg.Tools)
	req := arkmodel.CreateChatCompletionRequest{
		Model:    name,
		Messages: msgs,
		Tools:    toArkTools(effectiveTools),
	}
	if cfg.Temperature != nil {
		t := float32(*cfg.Temperature)
		req.Temperature = &t
	}
	if cfg.MaxTokens != nil {
		req.MaxTokens = cfg.MaxTokens
	}
	if cfg.TopP != nil {
		tp := float32(*cfg.TopP)
		req.TopP = &tp
	}
	streamFalse := false
	req.Stream = &streamFalse

	resp, err := a.client.CreateChatCompletion(ctx, req)
	if err != nil {
		return nil, err
	}
	return fromArkResponse(&resp), nil
}

// Stream streams chat completion deltas through MessageStreamReader.
func (a *LarkChatModel) Stream(ctx context.Context, input []*lpmodel.Message, opts ...lpmodel.CallOption) (lpmodel.MessageStreamReader, error) {
	cfg := lpmodel.ApplyCallOptions(opts...)
	name := a.modelName
	if cfg.ModelOverride != "" {
		name = cfg.ModelOverride
	}
	msgs, err := toArkMessages(input)
	if err != nil {
		return nil, err
	}
	effectiveTools := mergeToolLists(a.tools, cfg.Tools)
	streamTrue := true
	req := arkmodel.CreateChatCompletionRequest{
		Model:         name,
		Messages:      msgs,
		Tools:         toArkTools(effectiveTools),
		Stream:        &streamTrue,
		StreamOptions: &arkmodel.StreamOptions{IncludeUsage: true},
	}
	if cfg.Temperature != nil {
		t := float32(*cfg.Temperature)
		req.Temperature = &t
	}
	if cfg.MaxTokens != nil {
		req.MaxTokens = cfg.MaxTokens
	}
	if cfg.TopP != nil {
		tp := float32(*cfg.TopP)
		req.TopP = &tp
	}

	raw, err := a.client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		return nil, err
	}

	out := make(chan streamPart, 64)
	go runChatCompletionStream(raw, out)
	return newChanStreamReader(out), nil
}

func mergeToolLists(bound, perCall []*lpmodel.ToolInfo) []*lpmodel.ToolInfo {
	if len(perCall) > 0 {
		return perCall
	}
	return bound
}

// NewLarkChatModel returns a ToolCallingChatModel backed by Ark chat completions.
// baseURL may be empty to use DefaultBaseURL.
func NewLarkChatModel(apiKey, baseURL, modelName string) lpmodel.ToolCallingChatModel {
	apiKey = strings.TrimSpace(apiKey)
	opts := []arkruntime.ConfigOption{}
	if s := strings.TrimSpace(baseURL); s != "" {
		opts = append(opts, arkruntime.WithBaseUrl(strings.TrimRight(s, "/")))
	}
	client := arkruntime.NewClientWithApiKey(apiKey, opts...)
	return &LarkChatModel{
		client:    client,
		modelName: strings.TrimSpace(modelName),
	}
}

var _ lpmodel.ToolCallingChatModel = (*LarkChatModel)(nil)
