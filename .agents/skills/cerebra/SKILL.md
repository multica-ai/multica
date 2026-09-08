---
name: cerebra-routing
description: Architecture guide and reference for Multica's Cerebra intelligent LLM routing, tier scoring, session pinning, model discovery, and failover engine. Use when working on model dispatch, task routing, MCP tool compatibility, runtime tier maps, or error recovery in Multica.
metadata:
  author: multica
  version: "1.0.0"
---

# Cerebra Routing & Resilience Architecture

Cerebra is Multica's intelligent, real-time request-level LLM router and self-healing engine inside the Multica Daemon (`server/internal/cerebra` and `server/internal/daemon`).

## Core Invariants

1. **Filter-Before-Select**: Unavailability, policy rules, and budget limits are filtered *before* choosing candidate models.
2. **Deterministic Zero-Latency Routing**: Sub-millisecond keyword parsing and token scoring with zero external network overhead on the routing path.
3. **MCP Tool Floor**: Any task with active MCP tools, plugin hooks, or connected apps is strictly floored at `TierStandard` (never dispatched to `TierSimple`).
4. **Sticky Session Escalation**: Higher-tier pins (e.g. `TierHeavy` for architecture/refactor) persist across multi-turn chat sessions within the 2-hour TTL.
5. **Quota vs Context Separation**: Quota exhaustion/rate limits trigger model unavailability and live failovers; context length errors do not mark models unavailable.

---

## 3 Complexity Tiers

- **`TierSimple` (`"simple"`)**: Lightweight models (e.g., Haiku, Mini, Flash, Nano, $\le 4\text{B}$ params). Used for summaries, status checks, and simple questions.
- **`TierStandard` (`"standard"`)**: Balanced coding models (e.g., Sonnet, Coder, $7\text{B} - 16\text{B}$ params). Used for bug fixes, feature implementation, and tool/MCP execution.
- **`TierHeavy` (`"heavy"`)**: Frontier reasoning models (e.g., Opus, R1, o1, o3, $\ge 30\text{B}$ params). Used for system design, migrations, and complex refactoring.

---

## Classification Rules (`scorer.go`)

### Keyword Precedence (Highest-Tier-Wins)
- **`heavy`**: `refactor`, `architect`, `architecture`, `design`, `migrate`, `migration`
- **`standard`**: `debug`, `debugging`, `test`, `fix`, `add`, `update`, `implement`
- *Note*: Exact word boundary matching (`!unicode.IsLetter && !unicode.IsDigit`) prevents false matches (e.g., `prefix` or `fixture` will not trigger `fix`).

### Token Count Thresholds
- $\ge 2000 \text{ words} \implies \text{TierHeavy}$
- $\ge 500 \text{ words} \implies \ge \text{TierStandard}$

---

## Multica Plugin & MCP Detection (`cerebra_daemon_integration.go`)

`detectMCPUsage` checks:
- Agent MCP config overlay (`task.Agent.McpConfig`)
- Connected application integrations
- Plugin hook tools (`len(task.PluginHookTools) > 0`)
- Remote MCP connections (`len(task.RemoteMCPConnections) > 0`)

If any are present, `TaskMeta.WillUseMCPTools` is set to `true`, forcing the minimum tier to `TierStandard`.

---

## Live Failover Loop (`daemon.go`)

- **Max Failovers**: Up to 3 attempts per task (`maxModelFailovers = 3`).
- **Trigger**: Error matching `429`, `insufficient_quota`, `resource_exhausted`, `billing_hard_limit_reached`, or `rate-limited`.
- **Action**:
  1. Failed model marked unavailable in `UnavailabilityStore` (1-hour TTL).
  2. Session pin invalidated in `SessionStore`.
  3. Dynamic TierMap refreshed to pick next viable candidate.
  4. Task resumed on replacement model without failing the parent workflow.

---

## Database & API Surface

- **Tables**: `cerebra_model_unavailability`, `cerebra_routing_log`, `agent_runtime.tier_model_map`.
- **API Endpoints**:
  - `GET /api/tasks/{taskId}/routing-log`: Retrieve routing evidence and decision rule.
  - `PUT /api/runtimes/{runtimeId}/tier-model-map`: Configure runtime simple/standard/heavy model assignments.
