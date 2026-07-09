# Antigravity Live Transcript Empty State Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the misleading live-event spinner for empty Antigravity transcripts with a static explanation while preserving existing empty states for all other runtimes and terminal tasks.

**Architecture:** Reuse the runtime metadata that `AgentTranscriptDialog` already resolves and derive a provider-aware empty-state condition in the component. Keep the change entirely presentational: add one localized transcript message, select it only for a known Antigravity live task with zero displayed items, and leave API, persistence, WebSocket, timeout, and transcript parsing behavior untouched.

**Tech Stack:** React, TypeScript, Vitest, Testing Library, i18next locale JSON, pnpm

---

### Task 1: Add provider-aware empty-state regression coverage

**Files:**
- Modify: `packages/views/common/task-transcript/agent-transcript-dialog.test.tsx`
- Test: `packages/views/common/task-transcript/agent-transcript-dialog.test.tsx`

- [ ] **Step 1: Expose the mocked runtime API and add a typed runtime fixture**

Import `api` and `AgentRuntime`, then add a small fixture that supplies the complete runtime shape used by `api.listRuntimes()`:

```tsx
import { api } from "@multica/core/api";
import type { AgentRuntime, AgentTask } from "@multica/core/types/agent";

function runtimeFor(provider: string): AgentRuntime {
  return {
    id: "runtime-1",
    workspace_id: "workspace-1",
    daemon_id: "daemon-1",
    name: `${provider} runtime`,
    runtime_mode: "local",
    provider,
    launch_header: "",
    status: "online",
    device_info: "",
    metadata: {},
    owner_id: "owner-1",
    visibility: "private",
    last_seen_at: null,
    created_at: "2026-06-08T08:00:00Z",
    updated_at: "2026-06-08T08:00:00Z",
  };
}
```

- [ ] **Step 2: Let the render helper express live tasks without changing existing callers**

Extend the helper with an optional second argument and reset the mutable API mock in `beforeEach`:

```tsx
function renderDialog(
  dialogItems: TimelineItem[] = items,
  options: { task?: AgentTask; isLive?: boolean } = {},
) {
  return renderWithI18n(
    <AgentTranscriptDialog
      open
      onOpenChange={vi.fn()}
      task={options.task ?? baseTask}
      items={dialogItems}
      agentName="Codex"
      isLive={options.isLive}
    />,
  );
}

beforeEach(() => {
  cleanup();
  vi.mocked(api.listRuntimes).mockResolvedValue([]);
  useTranscriptViewStore.setState({
    sortDirection: "chronological",
    preserveFilters: false,
    selectedFilterKeys: [],
    defaultExpanded: false,
  });
});
```

- [ ] **Step 3: Write the Antigravity and non-Antigravity live-empty tests**

Use a live task with a runtime id so the component follows its existing metadata path. Wait for provider metadata before asserting the final empty state:

```tsx
const liveTask: AgentTask = {
  ...baseTask,
  runtime_id: "runtime-1",
  status: "running",
  completed_at: null,
};

it("explains unavailable live events for an empty Antigravity transcript", async () => {
  vi.mocked(api.listRuntimes).mockResolvedValue([runtimeFor("antigravity")]);

  renderDialog([], { task: liveTask, isLive: true });

  expect(
    await screen.findByText(
      "Antigravity does not currently provide live execution events. The transcript will be available after the task completes.",
    ),
  ).toBeInTheDocument();
  expect(screen.queryByText("Waiting for events...")).not.toBeInTheDocument();
});

it("keeps waiting for live events from other runtimes", async () => {
  vi.mocked(api.listRuntimes).mockResolvedValue([runtimeFor("hermes")]);

  renderDialog([], { task: liveTask, isLive: true });

  await screen.findByText("hermes runtime");
  expect(screen.getByText("Waiting for events...")).toBeInTheDocument();
});
```

- [ ] **Step 4: Run the focused test and verify RED**

Run:

```bash
pnpm --filter @multica/views exec vitest run common/task-transcript/agent-transcript-dialog.test.tsx
```

Expected: FAIL only in `explains unavailable live events for an empty Antigravity transcript` because the provider-specific sentence is not rendered; the non-Antigravity assertion and four pre-existing tests pass.

### Task 2: Implement the minimal provider-aware empty state

**Files:**
- Modify: `packages/views/common/task-transcript/agent-transcript-dialog.tsx`
- Modify: `packages/views/locales/en/agents.json`
- Modify: `packages/views/locales/zh-Hans/agents.json`
- Modify: `packages/views/locales/ja/agents.json`
- Modify: `packages/views/locales/ko/agents.json`
- Test: `packages/views/common/task-transcript/agent-transcript-dialog.test.tsx`

- [ ] **Step 1: Add the localized transcript message**

Add the same `antigravity_live_unavailable` key beside `waiting_events` in every supported locale. Locale parity tests require Japanese and Korean coverage whenever the English key set grows:

```json
// packages/views/locales/en/agents.json
"waiting_events": "Waiting for events...",
"antigravity_live_unavailable": "Antigravity does not currently provide live execution events. The transcript will be available after the task completes.",
"no_data": "No execution data recorded."
```

```json
// packages/views/locales/zh-Hans/agents.json
"waiting_events": "等待事件中...",
"antigravity_live_unavailable": "Antigravity 暂不提供实时执行事件。task 完成后即可查看执行记录。",
"no_data": "未记录执行数据。"
```

```json
// packages/views/locales/ja/agents.json
"waiting_events": "イベントを待機中...",
"antigravity_live_unavailable": "Antigravity は現在、リアルタイム実行イベントを提供していません。タスク完了後に実行記録を確認できます。",
"no_data": "記録された実行データがありません。"
```

```json
// packages/views/locales/ko/agents.json
"waiting_events": "이벤트를 기다리는 중...",
"antigravity_live_unavailable": "Antigravity는 현재 실시간 실행 이벤트를 제공하지 않습니다. 작업이 완료되면 실행 기록을 확인할 수 있습니다.",
"no_data": "기록된 실행 데이터가 없습니다."
```

- [ ] **Step 2: Derive the exact provider-aware condition**

Immediately after `displayItems`, derive the state without adding props, effects, API calls, or mutable state:

```tsx
const isAntigravityLiveEmpty =
  isLive && displayItems.length === 0 && runtimeInfo?.provider === "antigravity";
```

- [ ] **Step 3: Render static explanatory feedback for that condition**

Inside the existing `displayItems.length === 0` container, replace the current `{isLive ? (...) : (...)}` expression with the following complete expression. Reuse the already imported `Clock`; keep `Loader2` and the generic waiting copy for all other live runtimes:

```tsx
{isAntigravityLiveEmpty ? (
  <div className="flex max-w-md items-center gap-2 px-4 text-center">
    <Clock className="h-4 w-4 shrink-0" />
    {t(($) => $.transcript.antigravity_live_unavailable)}
  </div>
) : isLive ? (
  <div className="flex items-center gap-2">
    <Loader2 className="h-4 w-4 animate-spin" />
    {t(($) => $.transcript.waiting_events)}
  </div>
) : (
  t(($) => $.transcript.no_data)
)}
```

- [ ] **Step 4: Run the focused test and verify GREEN**

Run:

```bash
pnpm --filter @multica/views exec vitest run common/task-transcript/agent-transcript-dialog.test.tsx
```

Expected: PASS with 1 test file and 6 tests passing, with no React warnings or unhandled errors.

- [ ] **Step 5: Commit the focused implementation**

```bash
git add packages/views/common/task-transcript/agent-transcript-dialog.test.tsx \
  packages/views/common/task-transcript/agent-transcript-dialog.tsx \
  packages/views/locales/en/agents.json \
  packages/views/locales/zh-Hans/agents.json \
  packages/views/locales/ja/agents.json \
  packages/views/locales/ko/agents.json
git commit -m "fix(transcript): explain unavailable Antigravity live events"
```

### Task 3: Verify the branch and prepare it for review

**Files:**
- Review: `packages/views/common/task-transcript/agent-transcript-dialog.test.tsx`
- Review: `packages/views/common/task-transcript/agent-transcript-dialog.tsx`
- Review: `packages/views/locales/en/agents.json`
- Review: `packages/views/locales/zh-Hans/agents.json`
- Review: `packages/views/locales/ja/agents.json`
- Review: `packages/views/locales/ko/agents.json`

- [ ] **Step 1: Run the complete views test suite**

Run:

```bash
pnpm --filter @multica/views test
```

Expected: PASS for every `@multica/views` test file with no failed tests or unhandled errors.

- [ ] **Step 2: Run views typechecking**

Run:

```bash
pnpm --filter @multica/views typecheck
```

Expected: exit code 0 with no TypeScript errors, including typed access to `antigravity_live_unavailable`.

- [ ] **Step 3: Run views linting**

Run:

```bash
pnpm --filter @multica/views lint
```

Expected: exit code 0 with no lint errors introduced by the change.

- [ ] **Step 4: Inspect the complete branch diff**

Run:

```bash
git diff --check origin/main...HEAD
git status --short
git diff --stat origin/main...HEAD
git diff origin/main...HEAD -- \
  packages/views/common/task-transcript/agent-transcript-dialog.test.tsx \
  packages/views/common/task-transcript/agent-transcript-dialog.tsx \
  packages/views/locales/en/agents.json \
  packages/views/locales/zh-Hans/agents.json \
  packages/views/locales/ja/agents.json \
  packages/views/locales/ko/agents.json
```

Expected: no whitespace errors; only the approved design, implementation plan, component test, component, and four locale files differ from `origin/main`; every implementation line traces to issue #5181.

- [ ] **Step 5: Push the branch and open the official PR**

Push `fix/antigravity-live-transcript` to the authenticated fork, then create a PR against `multica-ai/multica:main`. The PR body must include `Closes #5181`, the verified test commands, the provider-aware scope, an explicit note that native `transcript.jsonl` parsing is excluded, the contributor thinking path requested by the repository template, and AI-assistance disclosure.
