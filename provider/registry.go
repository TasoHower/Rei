package provider

import "github.com/tasohower/rei/core"

// Protocol 是 wire 协议。
type Protocol string

const (
	ProtocolChatCompletions Protocol = "chat_completions"
	ProtocolAnthropic       Protocol = "anthropic"
	ProtocolResponses       Protocol = "responses"
)

// ModelMeta 是模型元数据：窗口、输出上限与价表（每百万 token 的 µUSD）。
type ModelMeta struct {
	ID            string       `json:"id"`
	ContextWindow int          `json:"context_window"`
	MaxOutput     int          `json:"max_output"`
	Pricing       core.Pricing `json:"pricing"`
}

// Preset 是一个厂商预设。
type Preset struct {
	ID       string      `json:"id"`
	Name     string      `json:"name"`
	Protocol Protocol    `json:"protocol"`
	BaseURL  string      `json:"base_url"`
	Models   []ModelMeta `json:"models"`
}

// presets 是内置预设。价格只作默认值，宿主可以覆盖。
var presets = []Preset{
	{
		ID: "deepseek", Name: "DeepSeek", Protocol: ProtocolChatCompletions,
		BaseURL: "https://api.deepseek.com/v1",
		Models: []ModelMeta{
			{ID: "deepseek-chat", ContextWindow: 64_000, MaxOutput: 8_192,
				Pricing: core.Pricing{Input: 270_000, Output: 1_100_000, CachedIn: 27_000}},
			{ID: "deepseek-reasoner", ContextWindow: 64_000, MaxOutput: 8_192,
				Pricing: core.Pricing{Input: 550_000, Output: 2_190_000, CachedIn: 140_000, Reasoning: 2_190_000}},
		},
	},
	{
		ID: "openai", Name: "OpenAI", Protocol: ProtocolResponses,
		BaseURL: "https://api.openai.com/v1",
		Models: []ModelMeta{
			{ID: "gpt-5", ContextWindow: 272_000, MaxOutput: 128_000,
				Pricing: core.Pricing{Input: 1_250_000, Output: 10_000_000, CachedIn: 125_000}},
		},
	},
	{
		ID: "anthropic", Name: "Anthropic", Protocol: ProtocolAnthropic,
		BaseURL: "https://api.anthropic.com",
		Models: []ModelMeta{
			{ID: "claude-sonnet-4-5", ContextWindow: 200_000, MaxOutput: 64_000,
				Pricing: core.Pricing{Input: 3_000_000, Output: 15_000_000, CachedIn: 300_000}},
		},
	},
	{
		ID: "kimi", Name: "Kimi", Protocol: ProtocolChatCompletions,
		BaseURL: "https://api.moonshot.cn/v1",
		Models: []ModelMeta{
			{ID: "kimi-k2", ContextWindow: 128_000, MaxOutput: 8_192,
				Pricing: core.Pricing{Input: 600_000, Output: 2_500_000, CachedIn: 150_000}},
		},
	},
	{
		ID: "qwen", Name: "Qwen", Protocol: ProtocolChatCompletions,
		BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
		Models: []ModelMeta{
			{ID: "qwen3-max", ContextWindow: 128_000, MaxOutput: 8_192,
				Pricing: core.Pricing{Input: 1_200_000, Output: 6_000_000}},
		},
	},
	{
		ID: "openrouter", Name: "OpenRouter", Protocol: ProtocolChatCompletions,
		BaseURL: "https://openrouter.ai/api/v1",
		Models:  []ModelMeta{},
	},
}

// Presets 返回内置预设的副本。
func Presets() []Preset {
	out := make([]Preset, len(presets))
	copy(out, presets)
	return out
}

// PresetFor 按厂商 id 查找预设。
func PresetFor(id string) (Preset, bool) {
	for _, p := range presets {
		if p.ID == id {
			return p, true
		}
	}
	return Preset{}, false
}

// Model 按模型 id 在全部预设里查找元数据。
func Model(id string) (ModelMeta, bool) {
	for _, p := range presets {
		for _, m := range p.Models {
			if m.ID == id {
				return m, true
			}
		}
	}
	return ModelMeta{}, false
}

// PricingFor 返回模型默认价表。
func PricingFor(id string) (core.Pricing, bool) {
	m, ok := Model(id)
	return m.Pricing, ok
}
