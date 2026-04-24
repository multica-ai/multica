---
title: AI coding tools matrix
description: Multica supports 10 AI coding tools; they implement the same interface, but the capability details diverge significantly.
---

import { Callout } from "fumadocs-ui/components/callout";

Multica ships with built-in support for **10 AI coding tools**. They all implement the same interface — queue, dispatch, execute, return results — so you can drive any of them from the same Multica board. **But the capability details diverge significantly**: whether session resumption actually works, whether MCP is supported, where skill files live, how models are selected. This page is the full matrix.

For guidance on picking a tool when creating an agent, see [Creating and configuring agents](/agents-create).

## Capability matrix

| Tool | Vendor | Session resumption | MCP | Skill injection path | Model selection |
|---|---|---|---|---|---|
| **Claude Code** | Anthropic | ✅ | **✅ (the only one that actually uses it)** | `.claude/skills/` | Static + flag |
| **Codex** | OpenAI | ⚠️ Code exists but unreachable | ❌ | `$CODEX_HOME/skills/` | Static |
| **Copilot** | GitHub | ✅ | ❌ | `.github/skills/` | Static (determined by account entitlement) |
| **Cursor** | Anysphere | ⚠️ Code exists but unusable | ❌ | `.cursor/skills/` | Dynamic discovery |
| **Gemini** | Google | ❌ | ❌ | `.agent_context/skills/` | Static |
| **Hermes** | Nous Research | ✅ | ❌ | `.agent_context/skills/` (fallback) | Dynamic discovery |
| **Kimi** | Moonshot | ✅ | ❌ | `.kimi/skills/` | Dynamic discovery |
| **OpenCode** | SST | ✅ | ❌ | `.config/opencode/skills/` | Dynamic discovery |
| **OpenClaw** | Open source | ✅ | ❌ | `.agent_context/skills/` (fallback) | Bound to the agent, can't be switched per task |
| **Pi** | Inflection AI | ✅ (session is a file path) | ❌ | `.pi/agent/skills/` | Dynamic discovery |

## What each tool is for

### Claude Code

From Anthropic. **First choice for new users** — the most complete feature set: session resumption actually works, it's the **only one of the 10 that truly reads MCP configuration**, and it supports fine-tuning flags like `--max-turns` and `--append-system-prompt`. Requires an Anthropic API key.

### Codex

From OpenAI. Uses JSON-RPC 2.0, has stronger statefulness, and a finer-grained approve mechanism (manual approval for `exec_command` and `patch_apply`). **Session resumption code exists but is currently unreachable** — if you need resume, pick Claude Code or one of the ACP family.

### Copilot

From GitHub. Model routing goes through your GitHub account entitlement — the tool doesn't select a model itself; GitHub decides which model you get. Placing skills in `.github/skills/` is GitHub CLI's native discovery mechanism.

### Cursor

From Anysphere, the CLI counterpart to the Cursor editor. **Session resumption code exists but doesn't actually work** — the Cursor CLI event stream doesn't return a session ID, so any resume value you pass is always invalid. If you need resume, pick something else.

### Gemini

From Google, supports the Gemini 2.5 and 3 series. **No session resumption and no MCP** — suitable for one-shot tasks that don't need long context memory.

### Hermes

From Nous Research. Uses the ACP protocol (shares a transport with Kimi). Session resumption works. But the **skill injection path is the generic fallback** (`.agent_context/skills/`), not a dedicated one — if the Hermes CLI itself doesn't read this path, skills may not take effect. Verify by testing.

### Kimi

From Moonshot, aimed at the Chinese market. Shares the ACP protocol with Hermes, but the skill path `.kimi/skills/` is Kimi CLI's native discovery mechanism — different from Hermes's fallback.

### OpenCode

From SST, open source. Dynamically discovers available models (scans the CLI's configuration file). Session resumption works. **Suitable for tinkerers who want to customize their model catalog.**

### OpenClaw

Open-source project, a CLI agent orchestrator. **Model is bound at the agent layer** (`openclaw agents add --model`) — it can't be overridden per task. Configuration is strictly controlled: users can't pass `--model` or `--system-prompt`; the agent-registration config decides.

### Pi

From Inflection AI, minimalist. **Session resumption is unusual** — the session ID is a file path on disk (`~/.pi/...`) rather than a string ID. In other tools, the resume id is a string returned by the CLI; in Pi, the resume id is the session file itself.

## Session resumption: who really supports it

The session resumption mechanism is covered in [Tasks](/tasks#can-a-task-continue-from-the-previous-context). Here's the **exact current state** per tool:

| Status | Tools | Meaning |
|---|---|---|
| ✅ Really works | Claude Code, Copilot, Hermes, Kimi, OpenCode, OpenClaw, Pi | Pass the resume id and it continues from the previous context |
| ⚠️ Code exists but unreachable | Codex, Cursor | Resume paths exist in the code but aren't actually reached (Codex silently falls back; Cursor doesn't return session id) — **treat as unsupported** |
| ❌ None | Gemini | The CLI has no resume mechanism |

**For your decision**: if your workflow needs agents to preserve context across tasks (failure retries, manual reruns, conversational iteration), pick only from the ✅ row.

## MCP configuration: only Claude Code actually reads it

**Of the 10 tools, only Claude Code actually consumes `mcp_config`**. The other 9 accept the field but **completely ignore it** — no error, no warning, the config just has no effect.

<Callout type="warning">
If you set `mcp_config` in an agent configuration but pick a tool other than Claude Code, your MCP servers have **no effect** on that agent. MCP integration currently covers Claude Code only.
</Callout>

## Where skill files go

Each tool uses **its own** skill discovery path. Before a task runs, the Multica daemon copies the workspace's skill files into the corresponding path:

| Tool | Path | Native discovery? |
|---|---|---|
| Claude Code | `.claude/skills/` | ✅ Native |
| Codex | `$CODEX_HOME/skills/` | ✅ Native |
| Copilot | `.github/skills/` | ✅ Native |
| Cursor | `.cursor/skills/` | ✅ Native |
| Kimi | `.kimi/skills/` | ✅ Native |
| OpenCode | `.config/opencode/skills/` | ✅ Native |
| Pi | `.pi/agent/skills/` | ✅ Native |
| Gemini | `.agent_context/skills/` | ⚠️ Generic fallback |
| Hermes | `.agent_context/skills/` | ⚠️ Generic fallback |
| OpenClaw | `.agent_context/skills/` | ⚠️ Generic fallback |

Whether a fallback-path tool actually reads this directory depends on the tool's own documentation — no guarantees. If your skills aren't taking effect for Gemini / Hermes / OpenClaw, check this first.

For creating and using skills, see [Skills](/skills).

## Next

- [Creating and configuring agents](/agents-create) — pick a tool for your agent
- [Tasks](/tasks) — task lifecycle and session-resumption mechanics
- [Daemon and runtimes](/daemon-runtimes) — where the tools run and how they connect to Multica
