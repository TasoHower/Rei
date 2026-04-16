// Package agent defines the minimal runnable contracts from doc/design/abstractions.md section 2.4
// (Agent, Planner) and the default concrete Agent: RunnerAgent — a pkg/model chat loop with
// function calling. Tool execution helpers live in loopforge/pkg/tool.
//
// Runner is the top-level execution entry point. Create a Runner via NewRunner
// with an entry RunnerAgent. If the entry agent has handoff targets (registered
// via AddHandoff), the Runner automatically manages the transfer loop with
// per-run Clone for concurrency isolation.
package agent
