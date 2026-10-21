package core

// Cost 是以「微美元」（µUSD）为单位的整数成本。用整数记账让长会话的累计值
// 保持精确：1 µUSD = 1e-6 美元，所以一次 0.0042 美元调用等于 4200。
type Cost struct {
	Input  int64 `json:"input"`
	Output int64 `json:"output"`
	// Reasoning 是 Output 的展示视图，用于单独计价的推理模型；不会重复计入 Total。
	Reasoning int64 `json:"reasoning,omitempty"`
}

// Add 把另一份成本累加进来。
func (c *Cost) Add(o Cost) {
	c.Input += o.Input
	c.Output += o.Output
	c.Reasoning += o.Reasoning
}

// Total 返回可计费成本（µUSD）。
func (c Cost) Total() int64 { return c.Input + c.Output }

// Usage 是一次模型调用（或一次运行）的 token 与成本账目。
type Usage struct {
	Input       int  `json:"input"`
	CachedInput int  `json:"cached_input,omitempty"`
	Output      int  `json:"output"`
	Reasoning   int  `json:"reasoning,omitempty"`
	Cost        Cost `json:"cost"`
}

// Add 把另一份用量累加进来。
func (u *Usage) Add(o Usage) {
	u.Input += o.Input
	u.CachedInput += o.CachedInput
	u.Output += o.Output
	u.Reasoning += o.Reasoning
	u.Cost.Add(o.Cost)
}

// agent_end 上的结束原因。
const (
	FinishStop          = "stop"
	FinishMaxIterations = "max_iterations"
	FinishMaxCost       = "max_cost"
	FinishAbort         = "abort"
	FinishError         = "error"
)

// Pricing 是「每百万 token」的价格表，单位为 µUSD。
type Pricing struct {
	Input     int64 `json:"input"`
	Output    int64 `json:"output"`
	CachedIn  int64 `json:"cached_input,omitempty"`
	Reasoning int64 `json:"reasoning,omitempty"`
}

// PricedUsage 按价格表把 token 折算成 Cost。若配置了缓存价，
// 命中的输入 token 按 CachedIn 计价而不是 Input。
func PricedUsage(u Usage, p Pricing) Usage {
	cached := u.CachedInput
	if cached > u.Input {
		cached = u.Input
	}
	billableIn := u.Input - cached
	u.Cost = Cost{
		Input:     scale(billableIn, p.Input) + scale(cached, p.CachedIn),
		Output:    scale(u.Output, p.Output),
		Reasoning: scale(u.Reasoning, p.Reasoning),
	}
	return u
}

func scale(tokens int, microPerMillion int64) int64 {
	if tokens <= 0 || microPerMillion <= 0 {
		return 0
	}
	return int64(tokens) * microPerMillion / 1_000_000
}
