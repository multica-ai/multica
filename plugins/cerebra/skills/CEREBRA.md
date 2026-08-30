---
name: cerebra-routing
description: Cerebra is the intelligent model router built into this Multica instance. It automatically selects the optimal model for each task based on prompt complexity. Simple questions use fast lightweight models; coding/debug tasks use standard models; architecture/design tasks use frontier heavy models. Works with any connected runtime — OpenCode, Claude, Kimi, Grok, Ollama, or any custom provider.
---

# Cerebra — Intelligent Model Routing

Cerebra is running as a background sidecar (port 7842) on this instance.

## Tier Classification

| Tier | Model type | Example prompts |
|------|-----------|-----------------|
| **Simple** | Lightweight, fast, cheap | "What files are in this repo?", "Summarize this comment" |
| **Standard** | Balanced, accurate | "Debug this race condition", "Fix the auth handler" |
| **Heavy** | Frontier, thorough | "Architect a distributed consensus engine", "Design a new sharding system" |

## How It Works

1. `task.started` fires when an agent task is queued
2. Cerebra receives the prompt, classifies it by complexity
3. Calls `set_session_model` on the active agent session with the optimal model
4. Falls back automatically if the preferred model is rate-limited (circuit breaker)

## Provider Compatibility

Works with **any** connected runtime — zero configuration required:
- OpenCode, OpenClaw, Claude, Kimi, Grok, Hermes, Qoder
- Local Ollama (offline, air-gap friendly)
- Any unknown/custom runtime prefix
