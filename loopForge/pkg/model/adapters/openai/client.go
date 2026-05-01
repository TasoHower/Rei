package openai

import (
	"context"
	"strings"

	lpmodel "github.com/TasoHower/rei/loopForge/pkg/model"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/param"
)

const DefaultBaseURL = "https://api.openai.com/v1"
const DefaultModelName = "gpt-4o-mini"

type OpenAIChatModel struct {
	client    *openai.Client
	modelName string
	tools     []*lpmodel.ToolInfo
}

func NewOpenAIChatModel(apiKey, baseURL, modelName string) lpmodel.ToolCallingChatModel {
	apiKey = strings.TrimSpace(apiKey)
	var opts []option.RequestOption
	if apiKey != "" {
		opts = append(opts, option.WithAPIKey(apiKey))
	}
	if s := strings.TrimSpace(baseURL); s != "" {
		opts = append(opts, option.WithBaseURL(strings.TrimRight(s, "/")+"/"))
	}
	client := openai.NewClient(opts...)
	name := strings.TrimSpace(modelName)
	if name == "" {
		name = DefaultModelName
	}
	return &OpenAIChatModel{
		client:    &client,
		modelName: name,
	}
}

func (c *OpenAIChatModel) WithTools(tools []*lpmodel.ToolInfo) (lpmodel.ToolCallingChatModel, error) {
	dup := make([]*lpmodel.ToolInfo, len(tools))
	copy(dup, tools)
	return &OpenAIChatModel{
		client:    c.client,
		modelName: c.modelName,
		tools:     dup,
	}, nil
}

func (c *OpenAIChatModel) Generate(ctx context.Context, input []*lpmodel.Message, opts ...lpmodel.CallOption) (*lpmodel.Message, error) {
	cfg := lpmodel.ApplyCallOptions(opts...)
	name := c.modelName
	if cfg.ModelOverride != "" {
		name = cfg.ModelOverride
	}
	msgs, err := toOpenAIMessages(input)
	if err != nil {
		return nil, err
	}
	effectiveTools := mergeToolLists(c.tools, cfg.Tools)
	params := openai.ChatCompletionNewParams{
		Messages: msgs,
		Model:    name,
		Tools:    toOpenAITools(effectiveTools),
	}
	if cfg.Temperature != nil {
		params.Temperature = param.NewOpt(*cfg.Temperature)
	}
	if cfg.MaxTokens != nil {
		params.MaxTokens = param.NewOpt(int64(*cfg.MaxTokens))
	}
	if cfg.TopP != nil {
		params.TopP = param.NewOpt(*cfg.TopP)
	}
	resp, err := c.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, err
	}
	if len(resp.Choices) == 0 {
		return nil, nil
	}
	out := fromOpenAIResponse(&resp.Choices[0])
	if resp.Usage.PromptTokens > 0 || resp.Usage.CompletionTokens > 0 {
		out.InputTokens = resp.Usage.PromptTokens
		out.OutputTokens = resp.Usage.CompletionTokens
	}
	return out, nil
}

func (c *OpenAIChatModel) Stream(ctx context.Context, input []*lpmodel.Message, opts ...lpmodel.CallOption) (lpmodel.MessageStreamReader, error) {
	cfg := lpmodel.ApplyCallOptions(opts...)
	name := c.modelName
	if cfg.ModelOverride != "" {
		name = cfg.ModelOverride
	}
	msgs, err := toOpenAIMessages(input)
	if err != nil {
		return nil, err
	}
	effectiveTools := mergeToolLists(c.tools, cfg.Tools)
	params := openai.ChatCompletionNewParams{
		Messages: msgs,
		Model:    name,
		Tools:    toOpenAITools(effectiveTools),
		StreamOptions: openai.ChatCompletionStreamOptionsParam{
			IncludeUsage: param.NewOpt(true),
		},
	}
	if cfg.Temperature != nil {
		params.Temperature = param.NewOpt(*cfg.Temperature)
	}
	if cfg.MaxTokens != nil {
		params.MaxTokens = param.NewOpt(int64(*cfg.MaxTokens))
	}
	if cfg.TopP != nil {
		params.TopP = param.NewOpt(*cfg.TopP)
	}
	stream := c.client.Chat.Completions.NewStreaming(ctx, params)
	out := make(chan streamPart, 64)
	go runChatCompletionStream(stream, out)
	return newChanStreamReader(out), nil
}

var _ lpmodel.ToolCallingChatModel = (*OpenAIChatModel)(nil)
