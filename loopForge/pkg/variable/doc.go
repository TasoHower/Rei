// Package variable provides a thread-safe VarStore for cross-agent and
// tool-visible state, optional prompt injection ([Variables] block), and
// persistence via Snapshot/Import. Use Materialize to merge a static manifest,
// a persisted snapshot, and per-run runtime bindings for multi-turn flows.
package variable
