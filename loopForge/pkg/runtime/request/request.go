package request

// RuntimeRequest is the user-visible intent for one interaction.
type RuntimeRequest struct {
	SessionID   string
	UserMessage string
	RunMode     RunMode
	Options     RuntimeOptions
}

// RunMode selects orchestration mode for the run.
type RunMode string

const (
	RunModeSingleAgent RunMode = "single_agent"
	RunModeNetwork     RunMode = "network"
	RunModeInherit     RunMode = "inherit"
)

// RuntimeOptions holds optional overrides; zero value means server defaults.
type RuntimeOptions struct {
	Model           string
	SkillIDs        []string
	MaxSteps        *int
	SpawnMaxDepth   *int
	NetworkStrategy string // when RunModeNetwork: parallel | sequential | competitive
}
