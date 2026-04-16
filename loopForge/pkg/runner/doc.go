// Package runner provides the top-level Runner orchestrator that takes an
// [agent.RunnerAgent] and drives its execution, including multi-agent transfer
// loops with per-run cloning for concurrency isolation.
//
// Agent definitions and their capabilities (tool-calling loop, transfer tool
// generation, etc.) live in loopforge/pkg/agent.
package runner
