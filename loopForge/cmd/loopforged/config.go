package main

import "os"

// Environment variables for Volcengine Ark (Lark LLM) via volcengine-go-sdk.
// Primary names use LARK_*; DOUBAO_* / ARK_* remain as fallbacks.
// Create an inference endpoint in the Ark console; use its ID as the model (e.g. ep-xxxx).
const (
	EnvLarkAPIKey  = "LARK_API_KEY"
	EnvLarkModel   = "LARK_MODEL"
	EnvLarkBaseURL = "LARK_BASE_URL"
	// Legacy names (still read by LoadLarkConfig)
	EnvDoubaoAPIKey  = "DOUBAO_API_KEY"
	EnvArkAPIKey     = "ARK_API_KEY"
	EnvDoubaoModel   = "DOUBAO_MODEL"
	EnvArkModel      = "ARK_MODEL"
	EnvDoubaoBaseURL = "DOUBAO_BASE_URL"
	EnvUseMock       = "LOOPFORGE_USE_MOCK"
)

// DefaultLarkBaseURL is the Beijing region Ark API v3 base URL (no trailing slash).
const DefaultLarkBaseURL = "https://ark.cn-beijing.volces.com/api/v3"

// LarkConfig holds credentials for the first real LLM call.
type LarkConfig struct {
	APIKey  string
	Model   string
	BaseURL string
}

// LoadLarkConfig returns config when both API key and model (endpoint id) are set.
func LoadLarkConfig() (LarkConfig, bool) {
	key := firstString(os.Getenv(EnvLarkAPIKey), os.Getenv(EnvDoubaoAPIKey), os.Getenv(EnvArkAPIKey))
	model := firstString(os.Getenv(EnvLarkModel), os.Getenv(EnvArkModel), os.Getenv(EnvDoubaoModel))
	if key == "" || model == "" {
		return LarkConfig{}, false
	}
	base := firstString(os.Getenv(EnvLarkBaseURL), os.Getenv(EnvDoubaoBaseURL))
	if base == "" {
		base = DefaultLarkBaseURL
	}
	return LarkConfig{
		APIKey:  key,
		Model:   model,
		BaseURL: base,
	}, true
}

func firstString(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// UseMock reports offline scripted demo (no API key).
func UseMock() bool {
	v := os.Getenv(EnvUseMock)
	return v == "1" || v == "true" || v == "yes"
}
