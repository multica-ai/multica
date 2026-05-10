## Why

Issues currently require manual labeling and date scheduling, which is tedious at scale. By integrating an AI backend (DeepSeek), workspace members can generate label suggestions and schedule issue timelines in one click — without giving up control.

## What Changes

- Add a workspace-level AI configuration (provider, API key, model, base URL, label rules) stored in `workspace.settings`
- Add a `POST /workspaces/:id/ai/settings` and `GET /workspaces/:id/ai/settings` API to manage AI config
- Add a `POST /workspaces/:id/ai/label` API: accepts one or more issue IDs, returns AI-suggested labels (matching existing or proposing new ones)
- Add a `POST /workspaces/:id/ai/schedule` API: accepts one or more issue IDs, returns AI-suggested `start_date`/`end_date` per issue, respecting dependencies and priority
- Add an "AI" tab to the workspace settings UI for configuring the AI integration
- Add a "Suggest Labels" action to the issues list (single + multi-select) that shows a preview modal before applying
- Add a "Schedule with AI" action to the issues list/project view that shows a preview modal before applying date changes
- Add `pkg/llm/` package in the backend as an OpenAI-compatible LLM client (supports DeepSeek now; Base URL is configurable for future Ollama/other providers)

## Capabilities

### New Capabilities

- `ai-settings`: Workspace-level AI provider configuration (API key, model, base URL, label rules) stored in workspace settings JSONB
- `ai-label-suggestions`: AI-powered label suggestions for one or more issues — uses workspace label rules + existing labels as context, returns ranked suggestions the user previews and applies
- `ai-schedule-suggestions`: AI-powered scheduling suggestions for one or more issues — returns recommended `start_date`/`end_date` per issue with reasoning, respecting existing issue dependencies and priority

### Modified Capabilities

<!-- No existing spec-level behavior changes -->

## Impact

- **Backend**: New `pkg/llm/` package (OpenAI-compatible HTTP client), new `internal/service/ai_label.go` and `internal/service/ai_schedule.go`, new `internal/handler/ai.go`, new routes in `cmd/server/router.go`
- **Database**: No schema changes; `workspace.settings` JSONB is extended with an `"ai"` key
- **Frontend**: New AI settings tab in workspace settings; new "Suggest Labels" and "Schedule with AI" actions in issues list with preview modals in `features/issues/`
- **Config**: `DEEPSEEK_API_KEY` env var as fallback when workspace-level key is not set
- **No breaking changes**
