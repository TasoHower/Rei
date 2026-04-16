// Package agent defines the minimal runnable contracts from doc/design/abstractions.md section 2.4
// (Agent, Planner) and the default concrete Agent: RunnerAgent — a pkg/model chat loop with
// function calling. Tool execution helpers live in loopforge/pkg/tool.
//
// Transfer (handoff): RunnerAgent supports multi-agent transfer natively. Call
// AddHandoff to register allowed handoff targets; the Orchestrator in
// loopforge/pkg/transfer reads these via Handoffs() and injects transfer tools
// at runtime. Clone() creates per-run copies for concurrency isolation.
package agent
