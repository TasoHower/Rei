// Package agent defines the minimal runnable contracts from doc/design/abstractions.md section 2.4
// (Agent, Planner) and the default concrete Agent: RunnerAgent — a pkg/model chat loop with
// function calling. Tool execution helpers live in loopforge/pkg/tool.
//
// Multi-agent transfer (handoff) orchestration is in the separate loopforge/pkg/transfer package;
// RunnerAgent exposes ToolInterceptor / ExtraTools hooks for that purpose.
package agent
