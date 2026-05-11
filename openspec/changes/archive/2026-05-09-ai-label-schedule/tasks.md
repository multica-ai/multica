## 1. Backend — LLM Package

- [x] 1.1 Create `server/pkg/llm/client.go` with `LLMClient` interface and `CompletionRequest`/`CompletionResponse` types
- [x] 1.2 Create `server/pkg/llm/openai_compat.go` implementing `LLMClient` via HTTP POST to `/chat/completions` (configurable base URL, JSON mode)
- [x] 1.3 Create `server/pkg/llm/prompts/label.go` with system and user prompt builders for label suggestions
- [x] 1.4 Create `server/pkg/llm/prompts/schedule.go` with system and user prompt builders for schedule suggestions

## 2. Backend — AI Settings

- [x] 2.1 Add `AISettings` Go struct (provider, api_key, base_url, model, label_rules) and workspace settings parsing helpers in `server/internal/handler/workspace.go` or a new `ai_settings.go`
- [x] 2.2 Create `server/internal/handler/ai.go` with `GetAISettings` and `UpdateAISettings` handlers; parse/update `workspace.settings["ai"]`
- [x] 2.3 Register `GET /workspaces/{id}/ai/settings` and `POST /workspaces/{id}/ai/settings` in `cmd/server/router.go`

## 3. Backend — AI Label Endpoint

- [x] 3.1 Create `server/internal/service/ai_label.go`: `SuggestLabels(ctx, workspaceID, issueIDs)` — fetches issue content, fetches workspace labels, calls LLM, returns structured suggestions
- [x] 3.2 Add `SuggestLabels` handler to `server/internal/handler/ai.go` (validate max 20, resolve AI key, call service)
- [x] 3.3 Register `POST /workspaces/{id}/ai/label` in router

## 4. Backend — AI Schedule Endpoint

- [x] 4.1 Create `server/internal/service/ai_schedule.go`: `SuggestSchedule(ctx, workspaceID, issueIDs)` — fetches issues + dependencies, calls LLM, returns start/end suggestions with reasons
- [x] 4.2 Add `SuggestSchedule` handler to `server/internal/handler/ai.go`
- [x] 4.3 Register `POST /workspaces/{id}/ai/schedule` in router

## 5. Backend — Tests

- [x] 5.1 Unit test `pkg/llm/openai_compat.go` with a mock HTTP server
- [x] 5.2 Unit test `service/ai_label.go` with mock LLM client and mock DB queries
- [x] 5.3 Unit test `service/ai_schedule.go` with mock LLM client

## 6. Frontend — AI Settings Tab

- [x] 6.1 Add AI settings types to `apps/workspace/src/features/settings/` (provider, api_key_masked, model, base_url, label_rules)
- [x] 6.2 Create `apps/workspace/src/features/settings/components/ai-tab.tsx` with form fields for provider, API key, model, base URL, and an editable label rules list
- [x] 6.3 Add GET/POST mutations for `/ai/settings` in `apps/workspace/src/features/settings/mutations.ts`
- [x] 6.4 Register "AI" tab in the workspace settings page

## 7. Frontend — Suggest Labels Modal

- [x] 7.1 Add `POST /workspaces/:id/ai/label` API call in `apps/workspace/src/features/issues/mutations.ts`
- [x] 7.2 Create `apps/workspace/src/features/issues/components/ai-label-modal.tsx` — preview modal showing suggestions grouped by issue with checkboxes; "Apply" calls existing add-label endpoints
- [x] 7.3 Wire "Suggest Labels" action into the issue list bulk-select toolbar

## 8. Frontend — Schedule with AI Modal

- [x] 8.1 Add `POST /workspaces/:id/ai/schedule` API call in `apps/workspace/src/features/issues/mutations.ts`
- [x] 8.2 Create `apps/workspace/src/features/issues/components/ai-schedule-modal.tsx` — preview table showing issue / start / end / reason with row checkboxes; "Apply" calls existing issue update endpoint for each checked row
- [x] 8.3 Wire "Schedule with AI" action into the issue list bulk-select toolbar
