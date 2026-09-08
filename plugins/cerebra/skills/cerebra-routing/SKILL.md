---
name: cerebra-routing
description: Cerebra is the intelligent model router and resilience engine built into Multica. It automatically selects the optimal model tier (simple, standard, heavy) for each task based on prompt complexity, tool/MCP requirements, and available provider catalogs. Simple tasks route to fast lightweight models; coding and tool-calling tasks route to standard models; complex system design and refactoring route to heavy frontier models.
---

# Cerebra — Intelligent Model Routing & Resilience

Cerebra dynamically evaluates task requirements and routes tasks to the optimal model tier across all configured runtimes.

## Complexity Tiers

| Tier | Workloads | Criteria | Representative Models |
| :--- | :--- | :--- | :--- |
| **`TierSimple`** | Summaries, syntax questions, status checks, format conversion | Short prompts (<500 words), no tool calls | `claude-3-5-haiku`, `gpt-4o-mini`, `gemini-2.5-flash`, `llama3.2:3b` |
| **`TierStandard`** | Feature implementation, debugging, test writing, tool calling | Keywords (`debug`, `fix`, `test`, `implement`), $\ge 500$ words, or **MCP tool usage** | `claude-3-5-sonnet`, `gpt-4o`, `gemini-2.5-pro`, `qwen2.5-coder:7b` |
| **`TierHeavy`** | Architecture design, system migrations, deep refactoring | Keywords (`architect`, `refactor`, `design`, `migrate`), or $\ge 2000$ words | `claude-3-opus`, `o1`, `o3`, `deepseek-r1:14b`, `nemotron-3-ultra` |

## Core Invariants & Rules

1. **Filter-Before-Select**: Cerebra checks unavailability and policy constraints before model selection.
2. **MCP Tool Floor**: Tasks that utilize MCP tools, connected apps, or plugin hooks are strictly floored at `TierStandard` (never dispatched to `TierSimple`).
3. **Sticky Session Pinning**: 2-hour sliding window; if a session is escalated to `TierHeavy`, subsequent turns stay on `TierHeavy` to preserve conversation context.
4. **Self-Healing Failover**: Intercepts `429`, quota exhaustion, and rate limits, marks the failed model temporarily unavailable (1h TTL), and immediately retries with the next best candidate model up to 3 times.

## Supported Runtimes & Local Discovery

- **Cloud Providers**: OpenCode, Claude, OpenAI/Codex, Kimi, Hermes.
- **Local Ollama**: Probes `127.0.0.1:11434` for locally installed models and auto-syncs tool-capable models into OpenCode.
- **Aggregators**: Direct provider connections are prioritized over public proxy aggregators (`openrouter/*`).

