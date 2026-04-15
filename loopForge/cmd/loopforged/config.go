package main

import "os"

// Environment variable names for Volcengine Ark (Doubao) OpenAI-compatible API.
// Create an inference endpoint in the Ark console; use its ID as DOUBAO_MODEL (e.g. ep-xxxx).
const (
	EnvDoubaoAPIKey  = "DOUBAO_API_KEY"
	EnvArkAPIKey     = "ARK_API_KEY" // alternate name used by Volcengine docs
	EnvDoubaoModel   = "DOUBAO_MODEL"
	EnvDoubaoBaseURL = "DOUBAO_BASE_URL"
	EnvUseMock       = "LOOPFORGE_USE_MOCK"
)

// DefaultDoubaoBaseURL is the Beijing region Ark API v3 base URL (no trailing slash).
const DefaultDoubaoBaseURL = "https://ark.cn-beijing.volces.com/api/v3"

// DoubaoConfig holds credentials for the first real LLM call.
type DoubaoConfig struct {
	APIKey  string
	Model   string
	BaseURL string
}

// LoadDoubaoConfig returns config when both API key and model (endpoint id) are set.
func LoadDoubaoConfig() (DoubaoConfig, bool) {
	key := os.Getenv(EnvDoubaoAPIKey)
	if key == "" {
		key = os.Getenv(EnvArkAPIKey)
	}
	model := os.Getenv(EnvDoubaoModel)
	if key == "" || model == "" {
		return DoubaoConfig{}, false
	}
	base := os.Getenv(EnvDoubaoBaseURL)
	if base == "" {
		base = DefaultDoubaoBaseURL
	}
	return DoubaoConfig{
		APIKey:  key,
		Model:   model,
		BaseURL: base,
	}, true
}

// UseMock reports offline scripted demo (no API key).
func UseMock() bool {
	v := os.Getenv(EnvUseMock)
	return v == "1" || v == "true" || v == "yes"
}
