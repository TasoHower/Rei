package deepseek

import (
	"context"
	"strings"

	lpmodel "loopforge/pkg/model"

	dspk "github.com/cohesion-org/deepseek-go"
)

const DefaultBaseURL = "https://api.deepseek.com/"

type DeepSeekChatModel struct {
	client    *dspk.Client
	modelName string
	tools     []*lpmodel.ToolInfo
}

func (m *DeepSeekChatModel) WithTools(tools []*lpmodel.ToolInfo) (lpmodel.ToolCallingChatModel, error) {
	dup := make([]*lpmodel.ToolInfo, len(tools))
	copy(dup, tools)
	return &DeepSeekChatModel{
		client:    m.client,
		modelName: m.modelName,
		tools:     dup,
	}, nil
}

func (m *DeepSeekChatModel) Generate(ctx context.Context, input []*lpmodel.Message, opts ...lpmodel.CallOption) (*lpmodel.Message, error) {
	cfg := lpmodel.ApplyCallOptions(opts...)
	name := m.modelName
	if cfg.ModelOverride != "" {
		name = cfg.ModelOverride
	}
	msgs, err := toDeepSeekMessages(input)
	if err != nil {
		return nil, err
	}
	effectiveTools := mergeToolLists(m.tools, cfg.Tools)
	req := &dspk.ChatCompletionRequest{
		Model:    name,
		Messages: msgs,
		Tools:    toDeepSeekTools(effectiveTools),
	}
	if cfg.Temperature != nil {
		req.Temperature = float32(*cfg.Temperature)
	}
	if cfg.MaxTokens != nil {
		req.MaxTokens = *cfg.MaxTokens
	}
	if cfg.TopP != nil {
		req.TopP = float32(*cfg.TopP)
	}
	resp, err := m.client.CreateChatCompletion(ctx, req)
	if err != nil {
		return nil, err
	}
	return fromDeepSeekResponse(resp), nil
}

func (m *DeepSeekChatModel) Stream(ctx context.Context, input []*lpmodel.Message, opts ...lpmodel.CallOption) (lpmodel.MessageStreamReader, error) {
	cfg := lpmodel.ApplyCallOptions(opts...)
	name := m.modelName
	if cfg.ModelOverride != "" {
		name = cfg.ModelOverride
	}
	msgs, err := toDeepSeekMessages(input)
	if err != nil {
		return nil, err
	}
	effectiveTools := mergeToolLists(m.tools, cfg.Tools)
	req := &dspk.StreamChatCompletionRequest{
		Model:    name,
		Messages: msgs,
		Tools:    toDeepSeekTools(effectiveTools),
		StreamOptions: dspk.StreamOptions{
			IncludeUsage: true,
		},
	}
	if cfg.Temperature != nil {
		req.Temperature = float32(*cfg.Temperature)
	}
	if cfg.MaxTokens != nil {
		req.MaxTokens = *cfg.MaxTokens
	}
	if cfg.TopP != nil {
		req.TopP = float32(*cfg.TopP)
	}
	raw, err := m.client.CreateChatCompletionStream(ctx, req)
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

func NewDeepSeekChatModel(apiKey, baseURL, modelName string) lpmodel.ToolCallingChatModel {
	apiKey = strings.TrimSpace(apiKey)
	var opts []dspk.Option
	if s := strings.TrimSpace(baseURL); s != "" {
		opts = append(opts, dspk.WithBaseURL(strings.TrimRight(s, "/")+"/"))
	}
	client, err := dspk.NewClientWithOptions(apiKey, opts...)
	if err != nil {
		return nil
	}
	return &DeepSeekChatModel{
		client:    client,
		modelName: strings.TrimSpace(modelName),
	}
}

var _ lpmodel.ToolCallingChatModel = (*DeepSeekChatModel)(nil)
