# What Multica Injects Into Every Agent — Operator Guide (Humans)

Audience: workspace owners, agent authors, and operators who want to run
Multica agents efficiently. This explains the *invisible* context Multica
wraps around every agent run — everything on top of the skills you assign
and the Agent's own instructions.

Source of truth in code:
- Runtime brief assembler — `server/internal/daemon/execenv/runtime_config_sections.go`
- Per-turn prompt — `server/internal/daemon/prompt.go`
- Injection + file placement — `server/internal/daemon/execenv/runtime_config.go`, `context.go`
- Launch + env — `server/internal/daemon/daemon.go`, `server/pkg/agent/claude.go`
- Built-in skills — `server/internal/service/builtin_skills/`

---

## The mental model: three layers, not one

When you assign an issue to an agent, the model does **not** just see the
issue text and your Agent instructions. Multica assembles three layers and
hands them to the coding-agent CLI (Claude Code, Codex, Cursor, etc.):

```
┌─────────────────────────────────────────────────────────────┐
│ LAYER 1 — Runtime Brief   (CLAUDE.md / AGENTS.md, auto-managed)│
│   "# Multica Agent Runtime" … identity, workflow, CLI, rules  │
├─────────────────────────────────────────────────────────────┤
│ LAYER 2 — Skills          (files on disk, discovered by CLI)  │
│   8 built-in multica-* skills  +  your assigned skills        │
├─────────────────────────────────────────────────────────────┤
│ LAYER 3 — Per-turn Prompt (the stdin user message)            │
│   "You are running as a local coding agent…" + task specifics │
└─────────────────────────────────────────────────────────────┘
```

Your Agent instructions land inside Layer 1 under **`## Agent Identity`**.
That is the *only* part of Layer 1 you write directly. Everything else is
generated per run.

---

## Layer 1 — The Runtime Brief

Written by `InjectRuntimeConfig` into the working directory's context file
(`CLAUDE.md` for Claude/CodeBuddy, `AGENTS.md` for everyone else), inside
auto-managed markers so it survives re-writes without clobbering a repo's
real CLAUDE.md content:

```
<!-- BEGIN MULTICA-RUNTIME (auto-managed; do not edit) -->
# Multica Agent Runtime
…
<!-- END MULTICA-RUNTIME -->
```

The brief is assembled section-by-section. Which sections appear depends on
the **task kind** (see matrix below). Sections, in order:

| Section | What it does | Who controls it |
| --- | --- | --- |
| Header | "You are a coding agent in the Multica platform. Use the `multica` CLI." | fixed |
| Background Task Safety | Hard rule: never end a turn with background work running; no wakeups exist | fixed |
| **Agent Identity** | `**You are: <name>** (ID: …)` + **your Agent instructions verbatim** | **you** |
| Requesting User | Runtime owner's self-description, as background context | runtime owner profile |
| Task Initiator | Who triggered this run (member/agent); attribution + privacy note | derived |
| Workspace Context | Workspace-level system prompt | workspace owner |
| Connected Apps | MCP servers for connected toolkits (Jira, Slack, …) | workspace integrations |
| Available Commands | The `multica` CLI reference (core loop + issue CRUD) | fixed |
| Comment Formatting | File-first rule for posting comments (never inline `--content`) | fixed |
| Repositories | Workspace repos + `multica repo checkout` | workspace repos |
| Project Context | Project title/description/resources when issue belongs to a project | project owner |
| Issue Metadata | Discipline for the per-issue KV scratchpad | fixed |
| Instruction Precedence | Agent Identity wins over the workflow (assignment only) | fixed |
| Workflow | Step-by-step loop for this task kind | fixed per kind |
| Sub-issue Creation | `--status todo/backlog` + `--stage N` semantics | fixed |
| Skills | Lists installed skill names + descriptions | you + built-ins |
| Mentions | `mention://` links are side-effecting; when (not) to use | fixed |
| Attachments | How to fetch attachments via CLI | fixed |
| Always Use CLI | Only touch Multica through the `multica` CLI, never curl | fixed |
| Output | Where results must go (comment / stdout / chat) | fixed per kind |

### Instruction precedence — the one rule that matters most

> Agent Identity instructions have priority over the assignment workflow.
> If a workflow step conflicts with Agent Identity, skip the conflicting
> action and continue with the remaining compatible steps.

Practical consequence: if your Agent instructions say "never change issue
status" or "you are delegation-only," those win. The workflow's `status
in_progress` / `in_review` steps are skipped automatically. **Write
restrictions into Agent Identity; do not rely on the workflow.**

---

## The five task kinds

`classifyTask` picks one kind per run. The kind decides which brief sections
appear, which workflow is emitted, and where output goes.

| Kind | Trigger | Output goes to | Notable |
| --- | --- | --- | --- |
| **Assignment** | Issue assigned/promoted to the agent | Issue comment (mandatory) | Agent owns status transitions; may carry a handoff note |
| **Comment** | New comment on an assigned issue | Issue comment (if reply warranted) | Embeds the triggering comment text; coalesces comments that arrived mid-run; silence is allowed for agent-to-agent acks |
| **Chat** | Direct chat / IM channel message | The chat window / channel | Slack-aware: reads channel history, not Multica issues |
| **Quick-create** | Natural-language sentence in the create-issue modal | stdout → inbox notification | Exactly ONE `multica issue create`, then exit. No get/comment |
| **Autopilot** | Scheduled / webhook / manual autopilot, run-only | stdout → autopilot run result | No issue exists; complete instructions directly |

Section × Kind matrix (from the code comment):

```
Section               | comment | assign | autopilot | quick_create | chat
----------------------+---------+--------+-----------+--------------+------
Available Commands    |  full   |  full  |   full    |   minimal    | full
Comment Formatting    |   ✓     |   ✓    |    —      |     —        |  —
Repositories          |   △     |   △    |    △      |     —        |  △
Project Context       |   △     |   △    |    —      |     —        |  —
Issue Metadata        |   ✓     |   ✓    |    —      |     —        |  —
Instruction Precedence|   —     |   ✓    |    —      |     —        |  —
Sub-issue Creation    |   ✓     |   ✓    |    —      |     —        |  —
Skills                |   ✓     |   ✓    |    ✓      |     —        |  ✓
Mentions              |   ✓     |   ✓    |    —      |     —        |  —
Attachments           |   ✓     |   ✓    |    —      |     —        |  —
```
(✓ always, — never, △ only if data present.)

---

## Layer 2 — Skills

Two sets of skills reach every eligible run:

1. **Your assigned skills** — whatever you attach to the agent.
2. **Eight built-in `multica-*` skills** — always available, teaching the
   agent how the platform itself works:

| Built-in skill | Teaches |
| --- | --- |
| `multica-working-on-issues` | PR-link vs close intent, metadata keys, status side effects, sub-issue enqueue |
| `multica-mentioning` | `[@Name](mention://<type>/<uuid>)` mechanics and side effects |
| `multica-squads` | Squad leader delegation, roles, activity tracking |
| `multica-creating-agents` | Agent creation contracts, settings, runtime selection |
| `multica-autopilots` | Creating/triggering autopilots, schedule/webhook/manual |
| `multica-projects-and-resources` | Project context, resources, GitHub repos |
| `multica-runtimes-and-repos` | Runtime/repo configuration |
| `multica-skill-importing` | Importing skills from URLs / archives |

Skills are written to disk at a provider-specific path so the CLI discovers
them natively:

| Provider | Context file | Skills path |
| --- | --- | --- |
| claude, codebuddy | `CLAUDE.md` | `.claude/skills/<name>/SKILL.md` |
| copilot | `AGENTS.md` | `.github/skills/<name>/SKILL.md` |
| opencode | `AGENTS.md` | `.opencode/skills/<name>/SKILL.md` |
| cursor | `AGENTS.md` | `.cursor/skills/<name>/SKILL.md` |
| codex | `AGENTS.md` | via `CODEX_HOME` |
| (others) | `AGENTS.md` | `.<provider>/skills/<name>/SKILL.md` |

The brief only *lists* skill names + descriptions ("discovered
automatically"); the full `SKILL.md` bodies live on disk for the CLI to open
on demand (progressive disclosure).

---

## Layer 3 — The per-turn prompt

The stdin user message. Deliberately minimal — detail lives in Layer 1. Per
kind:

- **Assignment:** "You are running as a local coding agent… Your assigned
  issue ID is: `<id>`… Start by running `multica issue get <id> --output
  json`." Includes the **handoff note** if the assigner left one.
- **Comment:** Embeds the triggering comment inline (`[NEW COMMENT] …`) so
  the agent cannot miss it, plus any coalesced earlier comments, plus a
  re-emitted reply target (`--parent <trigger-comment-id>`). Re-sent every
  turn so a resumed session never replies to the wrong comment.
- **Chat:** The user's message verbatim, channel-awareness block for
  Slack/IM, explicitly selected `/skill` refs, attachment ids.
- **Quick-create:** The user's raw sentence + detailed field rules (title,
  description structure, assignee resolution, project/parent pinning).
- **Autopilot:** Run id, trigger source/payload, autopilot instructions.

---

## Runtime environment injected into the agent process

Set in `daemon.go` before launching the CLI:

| Var | Value |
| --- | --- |
| `MULTICA_TOKEN` | Task-scoped `mat_` token (agent's identity for this run) |
| `MULTICA_SERVER_URL` | Daemon/API base (localhost for local runtimes) |
| `MULTICA_WORKSPACE_ID` | Workspace |
| `MULTICA_AGENT_ID` / `MULTICA_AGENT_NAME` | The agent |
| `MULTICA_TASK_ID` / `MULTICA_TASK_SLOT` | This run |
| `TMPDIR`/`TMP`/`TEMP` | Per-task temp dir |
| `PATH` | Prepended with the `multica` binary dir |
| (optional) | `MULTICA_AUTOPILOT_*`, `MULTICA_QUICK_CREATE_*`, `CODEX_HOME`, `CURSOR_DATA_DIR`, `OPENCLAW_*` |

**Custom env caveat (important for operators):** an agent's `CustomEnv` is
merged in, but any key that collides with a `MULTICA_*` reserved name is
**blocked** (`isBlockedEnvKey`). You cannot override the token, server URL,
workspace, or agent identity via custom env. If you need custom config, use
non-`MULTICA_` variable names.

---

## How the CLI is launched (Claude backend)

```
claude -p --output-format stream-json --input-format stream-json --verbose \
  --strict-mcp-config --permission-mode bypassPermissions \
  --disallowedTools AskUserQuestion \
  [--model …] [--effort low|medium|high|xhigh|max] [--max-turns N] \
  [--append-system-prompt "<runtime brief>"] \
  [--resume <session>] [--mcp-config <file>] \
  [ExtraArgs] [per-agent CustomArgs]
```

- Permissions are bypassed and `AskUserQuestion` is disabled — the agent
  **cannot** ask the user mid-run. It must decide autonomously.
- For inline providers (`kiro`, `kimi`, `traecli`), the runtime brief is
  passed via `--append-system-prompt` instead of a context file.
- `--strict-mcp-config` means only Multica's MCP config is used.

---

## Operating implications (how to run agents efficiently)

1. **Put all guardrails in Agent Identity.** It has precedence over the
   workflow and is the only text you fully control. "Never change status,"
   "delegation only," "read repo X first" belong here.
2. **Results only exist if posted as a comment.** Terminal output and run
   logs are invisible to users (except chat/quick-create/autopilot, which
   capture stdout). If your agent "did the work but nothing showed up," it
   skipped the mandatory comment.
3. **Metadata is the cross-run memory.** Facts a *future* run will re-read
   (PR URL, deploy URL, blocker) go in issue metadata; everything else goes
   in the result comment. Most runs pin nothing — that's expected.
4. **Mentions cost money.** `mention://agent/<id>` enqueues a whole new run.
   Accidental sign-off mentions start agent-to-agent loops. Silence is the
   designed way to end an exchange.
5. **Use stages/backlog for ordering.** `--stage N` groups sub-issues that
   run together; `--status backlog` holds work until promoted. This is how
   you sequence multi-step plans without hand-promoting.
6. **Workspace Context + Project description are broadcast to every run.**
   Use them for durable shared context (conventions, repo layout) instead of
   repeating it in each issue.
7. **Background work must be synchronous.** There is no completion wakeup;
   an agent that backgrounds a build and yields loses the result.
```
