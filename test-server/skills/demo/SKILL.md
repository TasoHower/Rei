---
name: demo
description: Bundled demo skill for test-server Skill Runtime debugger.
version: "0.1.0"
allowed-tools: Read, execute_shell_script
---

## Purpose

This skill is loaded by default when you run test-server with **Skill debug** mode. It proves that `SkillRegistry` injection reaches the model context (look for `## Skill: demo` in the effective system prompt when you enable **Show system prompt** on LLM rounds).

## Optional script

The engine still invokes a **shell** entry path. Use `scripts/hello.sh`, which calls `scripts/hello.py` so you can inspect how arguments are passed (`args` in the tool call become `argv[1:]` in Python). Example tool arguments: `{"skill":"demo","script":"scripts/hello.sh","args":["from_agent","step2"]}`.
