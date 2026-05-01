## Context

Multica already has a rich issue model (`issue` table with title, description, priority, status, labels, start_date, end_date, dependencies) and a workspace-level `settings` JSONB column. The existing `pkg/agent/` package handles long-running code execution agents (Claude Code, Codex). This change introduces a separate, lightweight LLM utility layer (`pkg/llm/`) for fast structured completions — label classification and schedule generation — that are a fundamentally different interaction pattern from code agents.

DeepSeek exposes an OpenAI-compatible chat completions API, which means the same client can point at Ollama or any other OpenAI-compatible provider by changing `base_url`.

## Goals / Non-Goals

**Goals:**
- Workspace-level AI configuration (API key, model, base URL, rules) stored in existing `workspace.settings` JSONB — no new tables
- `pkg/llm/` package with a minimal OpenAI-compatible HTTP client and a clean `LLMClient` interface
- Two service functions: `SuggestLabels(ctx, workspaceID, issueIDs)` and `SuggestSchedule(ctx, workspaceID, issueIDs)`
- REST endpoints: `GET/POST /workspaces/:id/ai/settings`, `POST /workspaces/:id/ai/label`, `POST /workspaces/:id/ai/schedule`
- Frontend: AI settings tab + "Suggest Labels" bulk action + "Schedule with AI" bulk action with preview-then-apply modals
- Env var `DEEPSEEK_API_KEY` as server-level fallback key (workspace key takes priority)

**Non-Goals:**
- Streaming LLM responses (structured JSON completions are synchronous)
- Auto-triggering AI on issue create/update
- Per-agent or per-user AI keys
- Building or hosting an LLM — only API consumption
- Replacing the `pkg/agent/` code execution layer

## Decisions

### D1: Reuse `workspace.settings` JSONB rather than a new table
`workspace.settings` already exists as `[]byte` (JSONB). Adding an `"ai"` key requires no migration, no new query, and follows the same update path as existing settings. A new table would add overhead with no benefit given the data is workspace-scoped and low-volume.

*Alternative considered*: New `workspace_ai_config` table. Rejected — unnecessary indirection for simple key/value config.

### D2: New `pkg/llm/` package, separate from `pkg/agent/`
`pkg/agent/` spawns long-lived CLI subprocesses for code execution. `pkg/llm/` makes HTTP calls for structured completions. These are different reliability profiles, different callers, and different lifetimes. Mixing them would blur the abstraction.

```
pkg/agent/     → spawn CLI subprocesses, stream events, minutes-long tasks
pkg/llm/       → HTTP POST, structured JSON response, milliseconds
```

*Alternative considered*: Adding DeepSeek as a new `Backend` in `pkg/agent/`. Rejected — the `Backend` interface is designed for code execution sessions with message streaming, not one-shot completions.

### D3: OpenAI-compatible client with configurable `base_url`
DeepSeek, OpenAI, and Ollama all speak the same `/chat/completions` wire format. A single client with a configurable `base_url` supports all three without extra dependencies.

```go
type LLMClient interface {
    Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
}
```

### D4: `response_format: {"type": "json_object"}` for all AI calls
Both label suggestions and schedule suggestions return structured data. Using JSON mode eliminates fragile text parsing and makes the API contract explicit. Both DeepSeek and OpenAI support this.

### D5: API key fallback chain
`workspace.settings.ai.api_key` → env var `DEEPSEEK_API_KEY` → error (AI features unavailable).  
This allows self-hosted deployments to set a single server key while per-workspace override is possible for SaaS scenarios.

### D6: Preview-then-apply modal pattern (no auto-apply)
All AI suggestions are surfaced in a confirmation modal. The user can deselect individual items before applying. This matches the existing UX for destructive bulk actions and is appropriate since AI suggestions may need human review.

### D7: Label rules stored as a free-text string array in settings
Rules like `"frontend related issues → frontend label"` are expressed as natural language strings that the LLM receives as part of the system prompt. No schema enforcement needed — the LLM interprets them. The UI presents them as an editable list.

## Risks / Trade-offs

- **External API latency**: DeepSeek calls add 1–3s per request. For bulk scheduling of many issues, this may be noticeable. Mitigation: show a loading state; future optimization could batch or stream results.
- **API key exposure**: The workspace API key is returned in settings responses. Mitigation: redact the key value in GET responses (return only masked last 4 chars, e.g. `sk-...xxxx`). The server uses the stored value directly.
- **LLM hallucination on label names**: The AI may suggest labels that are semantically correct but differ in spelling from existing ones. Mitigation: existing workspace labels are included in the prompt context; the response schema asks the model to prefer matching existing labels.
- **No migration needed, but settings schema is implicit**: The `"ai"` key in `workspace.settings` is parsed by Go structs, not validated by the DB. Mitigation: use strict Go struct unmarshalling with explicit zero values.

## Open Questions

- Should the schedule suggestion scope be limited to a project, or workspace-wide? (Proposal says workspace-scope; project-scope can be a follow-up.)
- Should we cap the number of issues in a single `/ai/label` or `/ai/schedule` request to avoid huge prompts? Suggested default cap: 20 issues per request.
