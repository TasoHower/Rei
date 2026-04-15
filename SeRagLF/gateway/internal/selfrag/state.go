package selfrag

type RetrievedDoc struct {
	Content        string  `json:"content"`
	Source         string  `json:"source"`
	RelevanceScore float64 `json:"relevance_score"`
}

type State struct {
	Question      string         `json:"question"`
	Collection    string         `json:"collection"`
	Generation    string         `json:"generation"`
	Documents     []RetrievedDoc `json:"documents"`
	Retries       int            `json:"retries"`
	MaxRetries    int            `json:"max_retries"`
	TopK          int            `json:"top_k"`
	Grounded      bool           `json:"grounded"`
	AnswerUseful  bool           `json:"answer_useful"`
	TraceSteps    []TraceStep    `json:"trace_steps"`
}

type TraceStep struct {
	Node   string `json:"node"`
	Detail string `json:"detail"`
}

func (s *State) AddTrace(node, detail string) {
	s.TraceSteps = append(s.TraceSteps, TraceStep{Node: node, Detail: detail})
}
