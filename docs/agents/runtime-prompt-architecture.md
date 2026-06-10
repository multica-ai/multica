# Runtime Prompt Architecture

This note documents how local Multica runtimes receive task context, provider
configuration, and session resume state.

## Short Answer

Multica uses one shared daemon prompt builder for local runtimes, but the final
context differs by task kind and provider:

- Task kind chooses the user prompt body: issue assignment, comment trigger,
  chat, autopilot, or quick-create.
- Provider chooses the runtime config file and CLI invocation details.
- Claude, Cursor, Gemini, OpenCode, Codex, and several ACP-style runtimes all
  receive the same `ResumeSessionID` field, but they translate it into different
  runtime-specific session mechanisms.
- The `40_000` cap is only a character cap for the server-rendered inline issue
  snapshot. It is not a cap on the whole provider conversation, prior Claude
  session cache, runtime config files, skills, tools, or transcript history.

This matters for incidents like TECH-3077: a comment-triggered Claude run can
receive a fresh, capped `IssueSnapshot` in the new user message and still resume
the old Claude Code session via `--resume`. The resumed provider session carries
Claude's previous conversation/cache outside Multica's snapshot cap.

## Execution Flow

1. The server claim endpoint builds an `AgentTaskResponse` for the daemon.
2. The daemon prepares or reuses a workdir.
3. The daemon writes runtime config files and skill files into the workdir.
4. The daemon builds the per-turn user prompt with `BuildPrompt`.
5. The daemon invokes the provider backend with `agent.ExecOptions`, including
   `ResumeSessionID` when the server supplied a prior session.
6. The provider backend maps those options to CLI flags, app-server calls, or an
   HTTP chat completion request.

## Matrix

| Dimension | Claude | Codex | Cursor | Gemini | OpenCode |
| --- | --- | --- | --- | --- | --- |
| Main task prompt | `BuildPrompt` output via stdin stream-json | `BuildPrompt` output as `turn/start` text | `BuildPrompt` output as `-p` arg | `BuildPrompt` output as `-p` arg | `BuildPrompt` output as final arg |
| Runtime brief | `CLAUDE.md` in workdir | `AGENTS.md` plus Codex home/skills | `AGENTS.md` plus `.cursor/skills` | `GEMINI.md` | `AGENTS.md`; may also inline `SystemPrompt` |
| Skills | `.claude/skills` | handled via per-task `CODEX_HOME` | `.cursor/skills` | default/fallback unless provider-specific path | `.opencode/skills` |
| Resume | `claude ... --resume <id>` | app-server `thread/resume`, fallback to `thread/start` | `--resume <id>` | `-r <id>` | `--session <id>` |
| Interactive terminal | Same prompt/session path; output mirrored when `presentation_mode=interactive` | Same | Same | Same | Same |
| Print/headless mode | Normal daemon mode | Normal daemon mode | Normal daemon mode | Normal daemon mode | Normal daemon mode |

`presentation_mode=interactive` is not a different prompt mode. It only mirrors
the stream to the Cerebro terminal broker before the same backend invocation.

## Provider Evidence

- Shared prompt builder: [server/internal/daemon/prompt.go](../../server/internal/daemon/prompt.go#L19)
  dispatches by task fields: chat, comment, autopilot, quick-create, then issue
  assignment.
- Daemon passes provider and session state into the backend:
  [server/internal/daemon/daemon.go](../../server/internal/daemon/daemon.go#L3131)
  sets `ExecOptions{Cwd, Model, ResumeSessionID, McpConfig, ...}`.
- Claude always runs in print mode:
  [server/pkg/agent/claude.go](../../server/pkg/agent/claude.go#L564) builds
  `claude -p --output-format stream-json --input-format stream-json ...`.
- Claude resumes with prior provider session:
  [server/pkg/agent/claude.go](../../server/pkg/agent/claude.go#L596) appends
  `--resume <id>` when `ResumeSessionID` is non-empty.
- Codex uses app-server, not `-p`:
  [server/pkg/agent/codex.go](../../server/pkg/agent/codex.go#L677) starts or
  resumes a thread, then [server/pkg/agent/codex.go](../../server/pkg/agent/codex.go#L696)
  sends the prompt in `turn/start`.
- Codex resume fallback is explicit:
  [server/pkg/agent/codex.go](../../server/pkg/agent/codex.go#L892) tries
  `thread/resume`, and [server/pkg/agent/codex.go](../../server/pkg/agent/codex.go#L914)
  falls back to `thread/start` on error.
- Cursor maps the same option to `--resume`:
  [server/pkg/agent/cursor.go](../../server/pkg/agent/cursor.go#L432).
- Gemini maps it to `-r`:
  [server/pkg/agent/gemini.go](../../server/pkg/agent/gemini.go#L278).
- OpenCode maps it to `--session`:
  [server/pkg/agent/opencode.go](../../server/pkg/agent/opencode.go#L76).

## Task Kind Differences

`BuildPrompt` is the single entry point:
[server/internal/daemon/prompt.go](../../server/internal/daemon/prompt.go#L19).

- Chat tasks use `buildChatPrompt` and include direct user messages plus chat
  attachment metadata: [server/internal/daemon/prompt.go](../../server/internal/daemon/prompt.go#L201).
- Comment-triggered issue tasks include the triggering comment directly, then
  optional snapshot/bundled context, then reply instructions:
  [server/internal/daemon/prompt.go](../../server/internal/daemon/prompt.go#L143).
- Assignment-triggered issue tasks either inline snapshot context, point at
  `multica issue context`, or instruct the agent to fetch issue and comments:
  [server/internal/daemon/prompt.go](../../server/internal/daemon/prompt.go#L32).
- Autopilot run-only tasks include autopilot title, source, trigger payload, and
  description, and explicitly say not to run `multica issue get`:
  [server/internal/daemon/prompt.go](../../server/internal/daemon/prompt.go#L267).
- Quick-create tasks have no issue and instruct the agent to create exactly one
  issue: [server/internal/daemon/prompt.go](../../server/internal/daemon/prompt.go#L55).

The runtime brief repeats workflow guardrails in provider-native config files.
`InjectRuntimeConfig` maps providers to config files:
[server/internal/daemon/execenv/runtime_config.go](../../server/internal/daemon/execenv/runtime_config.go#L129).
It writes `CLAUDE.md` for Claude, `AGENTS.md` for Codex/Cursor/OpenCode and
others, and `GEMINI.md` for Gemini:
[server/internal/daemon/execenv/runtime_config.go](../../server/internal/daemon/execenv/runtime_config.go#L159).

## Issue Runs vs Chat Runs

Issue runs use `issue_id`. The claim handler can add issue title, project,
project resources, repos, snapshot, bundled context hint, trigger comment, and
prior issue session:
[server/internal/handler/daemon.go](../../server/internal/handler/daemon.go#L1580).

Chat runs use `chat_session_id`. The claim handler loads chat-session state,
chat history, unresponded user messages, and a chat-message ID for attachments:
[server/internal/handler/daemon.go](../../server/internal/handler/daemon.go#L1763).

Chat history is capped independently at 30 messages in claim handling:
[server/internal/handler/daemon.go](../../server/internal/handler/daemon.go#L1800)
and [server/pkg/db/queries/chat.sql](../../server/pkg/db/queries/chat.sql#L124).

## The 40k Cap

The cap lives in
[server/internal/handler/daemon_cost_snapshot_cerebro.go](../../server/internal/handler/daemon_cost_snapshot_cerebro.go#L44):

```go
snapshotMaxChars = 40_000
```

It applies only after the server renders `IssueSnapshot` from:

- issue number/title/status/priority/description
- recent comments
- excluding the trigger comment because that is embedded separately
- excluding system rows
- keeping only the most recent 30 comments

The 30-comment limit is at
[server/internal/handler/daemon_cost_snapshot_cerebro.go](../../server/internal/handler/daemon_cost_snapshot_cerebro.go#L39).
Rendering is at
[server/internal/handler/daemon_cost_snapshot_cerebro.go](../../server/internal/handler/daemon_cost_snapshot_cerebro.go#L185).
The overflow behavior is at
[server/internal/handler/daemon_cost_snapshot_cerebro.go](../../server/internal/handler/daemon_cost_snapshot_cerebro.go#L80):
if snapshot compression is on, the handler tries compression; otherwise it
skips the snapshot and the agent falls back to fetching context.

The cap does not include:

- resumed Claude/Codex/Cursor/Gemini/OpenCode provider session history
- provider cache read/write tokens
- runtime config file contents
- skill files
- MCP tool configuration
- project resources
- chat history cap
- live task transcript stored in `task_message`

## Why TECH-3077 Could Resume Old Claude Context

The relevant code path is intentional:

1. Server looks up the last session for the same agent and issue unless the task
   has `force_fresh_session=true`:
   [server/internal/handler/daemon.go](../../server/internal/handler/daemon.go#L1735).
2. If the prior task used the same runtime, the claim response gets
   `PriorSessionID`:
   [server/internal/handler/daemon.go](../../server/internal/handler/daemon.go#L1753).
3. The daemon logs and forwards this as `ResumeSessionID`:
   [server/internal/daemon/daemon.go](../../server/internal/daemon/daemon.go#L3082)
   and [server/internal/daemon/daemon.go](../../server/internal/daemon/daemon.go#L3136).
4. The Claude backend turns that into `--resume`:
   [server/pkg/agent/claude.go](../../server/pkg/agent/claude.go#L596).

So the expectation "only user message plus capped snapshot" is not what the code
does for a normal follow-up task. It sends a new user prompt, but inside an
existing Claude provider session when a prior session exists and the task is not
marked fresh.

The existing resume-avoidance filters exclude known poisoned sessions:
[server/pkg/db/queries/agent.sql](../../server/pkg/db/queries/agent.sql#L368).
They exclude `api_invalid_request` and some known failure reasons, but they do
not exclude a completed/failed Claude session merely because it previously
accumulated large cache/context or hit a 1M-context ceiling unless that failure
was classified into one of the excluded reasons.

## Fields Added Above The Prompt

These are the main context sources:

- Claim response fields: `AgentTaskResponse` includes workspace context, agent,
  repos, project resources, prior session/workdir, trigger comment, snapshot,
  chat fields, autopilot fields, runtime tools config, and presentation mode:
  [server/internal/handler/agent.go](../../server/internal/handler/agent.go#L172).
- Agent instructions and skills are loaded on claim:
  [server/internal/handler/daemon.go](../../server/internal/handler/daemon.go#L1518).
- Runtime tools/MCP config is loaded from runtime and workspace connections:
  [server/internal/handler/daemon.go](../../server/internal/handler/daemon.go#L1486).
- Workspace context is loaded for every task kind:
  [server/internal/handler/daemon.go](../../server/internal/handler/daemon.go#L2075).
- Project resources are written to `.multica/project/resources.json`:
  [server/internal/daemon/execenv/context.go](../../server/internal/daemon/execenv/context.go#L110).
- Skills are written to provider-specific directories:
  [server/internal/daemon/execenv/context.go](../../server/internal/daemon/execenv/context.go#L152).
- Chat attachments are listed as IDs and filenames in the prompt:
  [server/internal/daemon/prompt.go](../../server/internal/daemon/prompt.go#L219).

Issue attachments are not directly inlined by `BuildPrompt`; agents are expected
to discover/download attachments through the CLI when needed.

## Suggested Fix

There is a documentation gap and probably a product-policy gap:

- Documentation should say clearly that `snapshot_prompt` caps only the inline
  issue snapshot, not provider session resume state.
- If the intended behavior for cost-saving snapshot mode is "fresh provider
  session", add a policy switch on claim: when `IssueSnapshot` is set and the
  task is comment-triggered or assignment-triggered, either skip prior session
  lookup or mark the task `force_fresh_session=true`.
- Safer alternative: add a runtime setting, for example
  `fresh_session_when_snapshot_prompt=true`, so teams can choose between
  continuity and context-cost isolation.

Until that exists, operators should use manual rerun/fresh-session paths when a
Claude session has hit a context/cache ceiling.
