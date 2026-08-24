# Capability Map and Milestones

[English](roadmap.md) | [简体中文](roadmap.zh-CN.md)

The project is organized by system dependency rather than a prescribed course.
Every topic connects a research question, representative papers, and an object
that can be implemented and evaluated.

| Layer | Topic | Representative work | Engineering object |
| --- | --- | --- | --- |
| 1 | Transformer and context | Attention Is All You Need, GPT-3 | provider, context assembly, model routing |
| 2 | reasoning, retrieval, alignment | CoT, RAG, InstructGPT, Constitutional AI | structured output, retrieval, citations, policy |
| 3 | Agent Loop | autonomous-agent surveys, ReAct-style systems | controller, state, action protocol, stop conditions |
| 4 | Memory | Memory in the LLM Era, A-MEM, HiAgent | session view, lifecycle memory, provenance, compaction |
| 5 | Tool Use | Toolformer, AutoGen | registry, schema, permission, timeout, observation |
| 6 | Planning | Understanding LLM Agent Planning | persisted DAG, dependency scheduling, verification |
| 7 | Evaluation / Safety | SPA-Bench, CyBench, AgentPoison | real tasks, evidence, approval, audit, adversarial cases |
| 8 | Optimization / Productization | DPO, Search-R1, AgentGym-RL, AFlow | traces, preferences, skills, workflow policy |

## Implemented Baseline

Paper Agent currently provides:

- deterministic and OpenAI-compatible model providers
- streaming, fallback, prompt-cache and usage accounting
- a ReAct loop with persistent Goal continuation
- file, shell, search, web, clarification, subagent, plugin, and MCP tools
- risk levels, approval, timeout, output limits, and host boundaries
- structured Sessions, Goals, Plans, Memory, Metrics, and trajectories
- L1/L2/L3 session compaction and context-length recovery
- plan validation, persistence, dependencies, role concurrency, verification,
  and human acceptance
- CLI, readline, TUI, Web UI, HTTP/SSE, and Feishu entry points
- cross-platform build, E2E, browser regression, open-source checks, archives,
  checksums, and release signing workflow

## Next Milestones

1. Add controlled PDF extraction, section location, and citation-entailment
   tools with fixed evaluation cases.
2. Add explicit memory candidates, conflict merging, expiry, and user-facing
   deletion/audit flows.
3. Build a public, reproducible task set that scores outcome, evidence, cost,
   latency, intervention, and safety together.
4. Harden process, filesystem, network, credential, and tenant isolation for
   deployments beyond a local reference environment.
5. Add Feishu long connections and encrypted callback payload support.
6. Extract versioned Skills only from repeatedly accepted trajectories, then
   compare them through ablation before considering policy training or RL.

Any new paper or implementation should state which layer it changes, which
prerequisite it assumes, what cost or attack surface it adds, and how its value
can be verified through a reproducible task.
