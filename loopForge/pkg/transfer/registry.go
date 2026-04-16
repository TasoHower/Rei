package transfer

import (
	"fmt"
	"sort"

	"loopforge/pkg/model"
	"loopforge/pkg/tool"
)

// AgentConfig holds the full configuration for one named agent in a registry.
type AgentConfig struct {
	Name               string
	Description        string // human-readable summary; surfaced in transfer tool descriptions
	ModelName          string
	SystemInstructions string
	ChatModel          model.ToolCallingChatModel
	ToolInfos          []*model.ToolInfo
	Executor           tool.ToolExecutor
	MaxSteps           int
	CallOptions        []model.CallOption
	TransferTargets    []string // allowed handoff targets; empty = all other agents in registry
}

// Registry is a named collection of AgentConfigs used by the Orchestrator
// to look up agents during transfer.
type Registry struct {
	agents map[string]*AgentConfig
	order  []string // insertion order for deterministic iteration
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{agents: make(map[string]*AgentConfig)}
}

// Register adds an agent configuration. Returns an error if Name is empty,
// ChatModel is nil, or a config with the same Name already exists.
func (r *Registry) Register(cfg AgentConfig) error {
	if cfg.Name == "" {
		return fmt.Errorf("transfer: AgentConfig.Name is required")
	}
	if cfg.ChatModel == nil {
		return fmt.Errorf("transfer: AgentConfig.ChatModel is nil for %q", cfg.Name)
	}
	if _, exists := r.agents[cfg.Name]; exists {
		return fmt.Errorf("transfer: duplicate agent name %q", cfg.Name)
	}
	c := cfg // shallow copy so caller cannot mutate after registration
	r.agents[cfg.Name] = &c
	r.order = append(r.order, cfg.Name)
	return nil
}

// Get returns the config for the given agent name, or false if not found.
func (r *Registry) Get(name string) (*AgentConfig, bool) {
	cfg, ok := r.agents[name]
	return cfg, ok
}

// Names returns all registered agent names in insertion order.
func (r *Registry) Names() []string {
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// TransferTargetsFor returns the agent names that current is allowed to
// transfer to. If cfg.TransferTargets is empty, all other agents are returned
// (in insertion order).
func (r *Registry) TransferTargetsFor(current string) []string {
	cfg, ok := r.agents[current]
	if !ok {
		return nil
	}
	if len(cfg.TransferTargets) > 0 {
		var out []string
		for _, t := range cfg.TransferTargets {
			if _, exists := r.agents[t]; exists && t != current {
				out = append(out, t)
			}
		}
		return out
	}
	var out []string
	for _, name := range r.order {
		if name != current {
			out = append(out, name)
		}
	}
	return out
}

// Validate checks that every agent's TransferTargets reference existing names.
// Call after all agents are registered.
func (r *Registry) Validate() error {
	for _, cfg := range r.agents {
		for _, t := range cfg.TransferTargets {
			if _, ok := r.agents[t]; !ok {
				return fmt.Errorf("transfer: agent %q: target %q not registered", cfg.Name, t)
			}
		}
	}
	return nil
}

// Len returns the number of registered agents.
func (r *Registry) Len() int {
	return len(r.agents)
}

// SortedNames returns agent names in alphabetical order (useful for deterministic tool ordering).
func (r *Registry) SortedNames() []string {
	out := r.Names()
	sort.Strings(out)
	return out
}
