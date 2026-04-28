# Multica Roadmap — Firtal Agent Platform

Multica er Firtals **AI Infrastructure layer** — det sted hvor alle virksomhedens agenter bor,
uanset hvor de bruges fra (UI, Slack, API, andre apps).

## Full Ranked Plan

**[RANKED-FEATURES.md](RANKED-FEATURES.md)** — 16 features ranked by enterprise impact, mappet mod Firtals platform-arkitektur (Layer 3 + Layer 4). Inkluderer execution order i 6 waves over ~20 uger.

## Infrastructure Features (allerede planlagt)

| # | Feature | Effort | Document |
|---|---------|--------|----------|
| 1 | [Public API + MCP Server](01-public-api.md) | 2-3 uger | Erstattet af Agent Chat API (#0a) i ranked plan |
| 2 | [Cloud Agent Runtimes](02-cloud-runtimes.md) | 2-3 uger | Claude Remote + Pi Agents connectors |
| 3 | [Cloud Deploy](03-cloud-deploy.md) | 3-5 dage | Fly.io / Railway one-click deploy |

## Execution Order

```
Wave 1 (uge 1-3):    Cloud Deploy → Agent Chat API → Auth Hardening
Wave 2 (uge 3-7):    Skill Marketplace → Integration Framework
Wave 3 (uge 7-10):   Persistent Memory → Agent Governance → Cloud Runtimes
Wave 4 (uge 10-13):  Scheduled Automations → Slack Bot → Platform Integration
Wave 5 (uge 13-17):  Evals → Knowledge Engine → Onboarding
Wave 6 (uge 17-20):  Dashboard → RBAC → Search → Self-healing
```

## Context

Multica er Firtals AI-kolonne i platform-arkitekturen (Layer 3 + Layer 4). Det mapper til:
- **Layer 4 (AI):** Agent Office, Agent Office (Workspace), Skills, MCP Tools, API Management, Knowledge Bases
- **Layer 3 (AI Infrastructure):** LLM Gateway, Agent Runtime, Agent Profiler, Skills Loader, MCP Gateway, Anti-hallucination, Evals, Agent Memory, Knowledge Engine, Agent Governance
