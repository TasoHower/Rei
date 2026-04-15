package types

// CallConfig is the resolved per-call configuration after applying CallOption values.
type CallConfig struct {
	Temperature   *float64
	MaxTokens     *int
	TopP          *float64
	ModelOverride string
	Tools         []*ToolInfo
}

// CallOption mutates CallConfig. Implementations are additive; last writer wins for scalars.
type CallOption func(*CallConfig)

// WithTemperature sets sampling temperature when supported by the backend.
func WithTemperature(v float64) CallOption {
	return func(c *CallConfig) {
		c.Temperature = &v
	}
}

// WithMaxTokens sets max tokens to generate (mapped to provider-specific field).
func WithMaxTokens(n int) CallOption {
	return func(c *CallConfig) {
		c.MaxTokens = &n
	}
}

// WithTopP sets top-p sampling.
func WithTopP(v float64) CallOption {
	return func(c *CallConfig) {
		c.TopP = &v
	}
}

// WithModel sets the logical model name for this call only (must exist on the provider).
func WithModel(name string) CallOption {
	return func(c *CallConfig) {
		c.ModelOverride = name
	}
}

// WithTools attaches tools for this call (replaces any previous WithTools in the same option list).
func WithTools(tools []*ToolInfo) CallOption {
	return func(c *CallConfig) {
		c.Tools = tools
	}
}

// ApplyCallOptions merges options into a CallConfig.
func ApplyCallOptions(opts ...CallOption) CallConfig {
	var c CallConfig
	for _, o := range opts {
		if o != nil {
			o(&c)
		}
	}
	return c
}
