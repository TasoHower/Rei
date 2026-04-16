// Package transfer provides multi-agent handoff (transfer) orchestration for
// loopForge. A Registry holds named agent configurations; the Orchestrator
// drives a transfer loop where the model's transfer_to_{name} tool calls cause
// control-flow handoffs between agents. The Orchestrator satisfies the
// agent.Agent interface, so callers see a single unified event stream.
package transfer
