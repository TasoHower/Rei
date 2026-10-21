package events

// PlanStep 是计划里的一个步骤。Index 从 1 开始，与工具参数中的 step 对齐。
type PlanStep struct {
	Index  int    `json:"index"`
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
}

// PlanGenerated 表示模型提交了一份新计划。宿主通常在审批界面上展示它，
// 用户确认后再让运行继续。
type PlanGenerated struct {
	Base
	Plan  string     `json:"plan"`
	Steps []PlanStep `json:"steps"`
}

func (e *PlanGenerated) Type() EventType { return PlanGeneratedType }

// PlanUpdated 表示计划被执行中的新事实修正（追加/删除/重排步骤）。
type PlanUpdated struct {
	Base
	Plan  string     `json:"plan"`
	Steps []PlanStep `json:"steps"`
	Note  string     `json:"note,omitempty"`
}

func (e *PlanUpdated) Type() EventType { return PlanUpdatedType }

// PlanStepStarted 表示某个步骤开始执行。
type PlanStepStarted struct {
	Base
	Index int    `json:"index"`
	Title string `json:"title"`
}

func (e *PlanStepStarted) Type() EventType { return PlanStepStartedType }

// PlanStepCompleted 表示某个步骤结束。Status 取 plan.Status* 常量
// （completed / failed / skipped）。
type PlanStepCompleted struct {
	Base
	Index  int    `json:"index"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Note   string `json:"note,omitempty"`
}

func (e *PlanStepCompleted) Type() EventType { return PlanStepCompletedType }
