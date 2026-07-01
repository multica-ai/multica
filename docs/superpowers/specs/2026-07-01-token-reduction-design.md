# Token Reduction: Slim Brief + Comment Session Resumption

## Problem

LLM API token consumption (Claude, OpenAI, OpenCode, etc.) is excessive: a handful of Multica tasks with comment back-and-forth exhausts provider rate limits or budgets far faster than using the same agent directly.

Two stacked causes:

1. **Verbose legacy brief** — `buildMetaSkillContent` in `runtime_config.go` generates a large CLAUDE.md/AGENTS.md (≈7k extra chars per task vs. the slim path). A slimmer version exists and has been running in staging but the `runtime_brief_slim` feature flag defaults to `false` in production.

2. **Unbounded session history on comment tasks** — `daemon.go:3914` passes `--resume <PriorSessionID>` unconditionally. For comment-triggered tasks, each new comment trigger replays the full prior conversation (all tool outputs, thinking turns) into the context window. After 4–5 back-and-forth replies, accumulated history dominates input token cost. A failed resume also triggers a fresh-session retry (lines 3966–3978), doubling spend on stale sessions.

## Changes

### 1. Enable slim brief in production

**File**: `server/internal/daemon/execenv/runtime_config_flag.go`

Change `useSlimBrief()` to default to `true`:

```go
func useSlimBrief() bool {
    return briefFlags().IsEnabled(context.Background(), runtimeBriefSlimFlag, true) // was: false
}
```

Effect: production routes to `buildMetaSkillContentSlim` (runtime_config_sections.go). The slim path gates sections by task kind — quick-create, chat, and autopilot skip Mentions, Comment Formatting, Issue Metadata, Sub-issue, and Repositories where they don't apply. Per-section prose is compressed. Estimated saving: ~7k chars per comment-triggered task.

Rollback: `FF_RUNTIME_BRIEF_SLIM=false` env var, no redeploy needed.

Follow-up (separate PR): delete the legacy path in `runtime_config.go` (847 lines) and the flag machinery once production has burned in.

### 2. Stop resuming sessions on comment-triggered tasks

**File**: `server/internal/daemon/daemon.go` (around line 3914)

Gate `PriorSessionID` to non-comment tasks before building `execOpts`:

```go
priorSession := task.PriorSessionID
if task.TriggerCommentID != "" {
    // Comment tasks re-read issue context and history fresh via CLI on
    // every run; replaying prior session history adds nothing and
    // accumulates unbounded input tokens over back-and-forth comment cycles.
    priorSession = ""
}
execOpts := agent.ExecOptions{
    ...
    ResumeSessionID: priorSession,
    ...
}
```

**What is unaffected**:
- Chat tasks (`ChatSessionID != ""`) — checked before `TriggerCommentID` in `BuildPrompt`; session continuity is the whole point of chat.
- Assignment tasks — keep current behavior (resume gated by `gateResumeToReusedWorkdir`).

**Also eliminates**: the double-spend from failed-resume retries (lines 3966–3978) on comment tasks.

## Scope

These two changes only. Out of scope:
- Skill-size limits (reasonable follow-up once we measure per-agent skill total size)
- Assignment-task session age/turn caps (follow-up)
- Token observability in the usage dashboard (separate concern)

## Files Changed

| File | Change |
|------|--------|
| `server/internal/daemon/execenv/runtime_config_flag.go` | Flip slim brief default to `true` |
| `server/internal/daemon/daemon.go` | Gate `PriorSessionID` to non-comment tasks |
