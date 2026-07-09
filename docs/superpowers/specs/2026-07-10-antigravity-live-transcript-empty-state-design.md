# Antigravity Live Transcript Empty State Design

## Context

Multica's Antigravity backend runs `agy -p`, the daemon-compatible non-interactive mode. The CLI can execute tools, but it exposes plain assistant stdout instead of the structured live event stream available from runtimes such as Hermes. During a typical Antigravity task, Multica may therefore receive no `TaskMessagePayload` rows until the final response appears near the end of the run.

The transcript dialog currently treats every live task with zero displayable events the same: it renders an animated loader and the generic “waiting for events” label. For Antigravity this can look like a hung transcript request even though the request succeeded and the runtime simply has no live events to provide.

GitHub issue [#5181](https://github.com/multica-ai/multica/issues/5181) tracks this behavior. It is distinct from #4779 / #4814, which fixed completed runs whose recovered final text was missing from the persisted transcript.

## Goal

When a live Antigravity task has no transcript items, replace the indefinite-looking loader with a clear, non-animated explanation that live transcript events are unavailable and the transcript will be available after completion.

## Non-goals

- Do not tail or parse Antigravity's native `transcript.jsonl`.
- Do not infer tool calls or thinking blocks from plain stdout.
- Do not change the Antigravity backend, task-message API, database, or WebSocket flow.
- Do not add request timeouts or redesign terminal transcript error handling.
- Do not change the existing live empty state for other runtimes.

## Approaches Considered

### 1. Provider-aware empty state in the existing dialog — selected

Use the runtime metadata that `AgentTranscriptDialog` already fetches. When `isLive` is true, `displayItems` is empty, and `runtimeInfo.provider` is `antigravity`, render a static informational message instead of the generic spinner.

This is the smallest change that accurately describes the current integration contract. It requires no API changes and preserves all existing behavior for runtimes that can stream structured events.

### 2. Generic request timeout and error state

Add an `AbortController` timeout to `listTaskMessages` and surface a retry UI. This would improve genuine network failures, but it would not address the observed live Antigravity case because the request succeeds and returns an empty array. It should be handled independently.

### 3. Tail Antigravity's native transcript

Monitor `transcript.jsonl` and translate records such as `RUN_COMMAND`, `VIEW_FILE`, and `PLANNER_RESPONSE` into Multica task messages. This could provide real live telemetry, but it depends on an internal record format and must solve conversation-ID discovery, accumulated resumed-session offsets, partial writes, and deduplication. It is intentionally excluded from this focused bug fix.

## UI Behavior

The existing dialog continues to open immediately for a live task and performs its normal backfill.

The empty event area follows this decision order:

1. If transcript items exist, render them normally.
2. If the task is live and the resolved runtime provider is `antigravity`, show a static clock icon and provider-specific explanatory text.
3. If the task is live for any other or unresolved provider, retain the existing animated “waiting for events” state.
4. If the task is terminal and has no events, retain the existing “no data” state.

The brief generic spinner before runtime metadata resolves is acceptable. If the runtime metadata request fails, the dialog retains the existing generic live behavior rather than guessing the provider.

English copy:

> Antigravity does not currently provide live execution events. The transcript will be available after the task completes.

Chinese copy:

> Antigravity 暂不提供实时执行事件。task 完成后即可查看执行记录。

## Components and Data Flow

- `AgentTranscriptDialog` already calls `api.listRuntimes()` when opened and stores the matching runtime in `runtimeInfo`.
- No new state, API request, or prop is introduced.
- A derived boolean identifies the provider-specific live empty state from `isLive`, `displayItems.length`, and `runtimeInfo?.provider`.
- The event-list empty-state branch selects between the Antigravity explanation, the existing generic live loader, and the existing terminal no-data label.
- New locale keys live under the existing transcript namespace in the English and Chinese agent locale files.

## Error Handling

Runtime metadata failures remain soft failures, matching current behavior. The provider-specific message is shown only when the provider is known with certainty. Transcript backfill errors and terminal fetch behavior are unchanged.

## Testing

Add focused component coverage in `agent-transcript-dialog.test.tsx`:

1. A live task with no items and an Antigravity runtime shows the provider-specific explanation and does not show the generic waiting label.
2. A live task with no items and a non-Antigravity runtime retains the generic waiting label.

Run the focused transcript dialog test, the `@multica/views` test suite, and TypeScript typechecking before creating the PR. Include a screenshot in the PR if a local UI fixture can reproduce the state without expanding the implementation scope.

## Risks

- Runtime metadata resolves asynchronously, so users may briefly see the existing loader before the explanatory state appears.
- If the provider identifier changes, the condition would stop matching; the existing typed runtime provider value and tests reduce this risk.
- Provider-specific UI can accumulate over time. This change remains inline and narrowly scoped because only Antigravity currently has this confirmed live-event contract.

## Success Criteria

- The Antigravity-specific message appears for a live empty transcript after runtime metadata resolves.
- No animated loader remains in that provider-specific state.
- Other runtime and terminal empty states are unchanged.
- Focused tests demonstrate the distinction and pass with the implementation.
