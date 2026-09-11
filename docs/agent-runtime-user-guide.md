# Multica Agent Runtime — User Guide Overview

How Multica turns a plain coding-agent CLI (Claude Code, Codex, Cursor, …)
into a workspace teammate that owns issues, comments, and status. This is
the overview; the two companion docs go deeper for each audience:

- **[Working with agents](./working-with-agents.md)** — for teammates who
  *use* agents (assign, comment, chat): what to expect, troubleshooting.
- **[Operator guide (humans)](./agent-runtime-context-humans.md)** — what
  the platform injects, how to configure it, how to run agents efficiently.
- **[Agent guide](./agent-runtime-context-agents.md)** — the same context
  from inside the run, as operating rules for the agent.

---

## 1. What Multica adds on top of your agent

You give an agent two things: **skills** and **instructions**. Multica wraps
those in a generated runtime so the same agent behaves correctly across
assignments, comments, chat, quick-create, and autopilots — without you
re-teaching platform mechanics each time.

The wrapper is three layers:

| Layer | Delivered as | Contains |
| --- | --- | --- |
| **Runtime Brief** | `CLAUDE.md` / `AGENTS.md` (auto-managed block) | Identity, workflow, `multica` CLI, hard rules |
| **Skills** | `SKILL.md` files on disk | 8 built-in `multica-*` + your assigned skills |
| **Per-turn Prompt** | stdin user message | This run's task kind + specifics |

Your Agent instructions appear inside the brief under `## Agent Identity`,
and **take precedence over the generated workflow**.

---

## 2. The runtime brief at a glance

Injected between markers so it never clobbers a repo's real `CLAUDE.md`:

```
<!-- BEGIN MULTICA-RUNTIME (auto-managed; do not edit) -->
# Multica Agent Runtime
## Background Task Safety        ## Agent Identity
## Requesting User               ## Task Initiator
## Workspace Context             ## Connected Apps
## Available Commands            ## Comment Formatting
## Repositories                  ## Project Context
## Issue Metadata                ## Instruction Precedence
### Workflow                     ## Sub-issue Creation
## Skills                        ## Mentions
## Attachments                   ## Always Use the multica CLI
## Output
<!-- END MULTICA-RUNTIME -->
```

Sections are gated by **task kind** — quick-create gets a minimal brief;
chat drops issue-specific sections; autopilot has no issue at all.

---

## 3. The five task kinds

| Kind | Triggered by | Output lands in | One-line rule |
| --- | --- | --- | --- |
| Assignment | Issue assigned to agent | Issue comment | Own the status lifecycle; post one result comment |
| Comment | New comment on your issue | Issue comment | Answer *this* comment; silence OK for acks |
| Chat | Direct/IM message | Chat window / channel | Converse; read channel, not issues |
| Quick-create | Sentence in create modal | stdout → inbox | Exactly one `issue create`, then exit |
| Autopilot | Schedule/webhook/manual | stdout → run result | Run instructions; no issue exists |

---

## 4. The rules that govern every run

1. **Agent Identity > workflow.** Configure restrictions in your Agent
   instructions; the workflow yields to them. (The explicit precedence
   statement is only injected on assignment runs — the kind with
   conflict-prone workflow steps like status changes. Identity itself is
   present in every brief.)
2. **Results = comments.** For issue work, only a posted comment reaches the
   user. Terminal output is invisible. Post exactly one per run.
3. **Comment bodies via `--content-file`**, written inside the working dir —
   never inline `--content`, never `/tmp`.
4. **No background-and-yield.** Turn exit = task terminal. Waits are
   synchronous. No completion wakeup exists.
5. **Mentions are side effects.** `mention://agent/<id>` starts a new run.
   Default to no mention; silence ends agent-to-agent threads.
6. **Metadata is cross-run memory.** Pin only durable, re-read facts
   (`pr_url`, `deploy_url`, `blocked_reason`). Most runs pin nothing.
7. **Only the `multica` CLI touches the platform.** No curl/wget.
8. **The agent can't ask.** `AskUserQuestion` is off; it decides
   autonomously or marks `blocked`.

---

## 5. Runtime environment & launch

Each run gets a task-scoped identity and isolated workspace:

- `MULTICA_TOKEN` (`mat_…`), `MULTICA_SERVER_URL`, `MULTICA_WORKSPACE_ID`,
  `MULTICA_AGENT_ID`, `MULTICA_TASK_ID`, per-task `TMPDIR`, `multica` on
  `PATH`. Custom env cannot override `MULTICA_*` reserved names.
- Claude backend launches with `--permission-mode bypassPermissions`,
  `--disallowedTools AskUserQuestion`, `--strict-mcp-config`, streaming
  JSON, and the brief via context file (or `--append-system-prompt` for
  inline providers).

---

## 6. Built-in skills (always present)

`multica-working-on-issues`, `multica-mentioning`, `multica-squads`,
`multica-creating-agents`, `multica-autopilots`,
`multica-projects-and-resources`, `multica-runtimes-and-repos`,
`multica-skill-importing`. These teach the agent the platform's own
mechanics so your assigned skills can focus on the actual job.

---

## 7. Running agents efficiently — the short version

- Encode all guardrails in **Agent Identity**, not per-issue text.
- Use **Workspace Context** and **Project description** for durable shared
  context (conventions, repo layout).
- Sequence multi-step work with `--stage N` and `--status backlog`.
- Expect **one result comment** per run; expect most runs to pin no
  metadata; expect silence when there is nothing to say.
- Treat every `@mention` as a deliberate, cost-bearing action.

---

*Code references: `server/internal/daemon/execenv/runtime_config_sections.go`
(brief), `server/internal/daemon/prompt.go` (per-turn prompt),
`runtime_config.go` / `context.go` (injection + file placement),
`daemon.go` + `server/pkg/agent/claude.go` (env + launch),
`server/internal/service/builtin_skills/` (built-in skills).*
