# Agent detail tabs

Authoritative map of **how a tab gets onto the agent detail page**. Read this
before adding, moving, or removing a tab there.

## Two pages, one target

There are two agent detail pages, selected by the `cerebro_agent_page_redesign`
feature flag in `apps/web/app/[workspaceSlug]/(dashboard)/agents/[id]/page.tsx`:

| Flag | Page | Zone |
|---|---|---|
| on (default) | `CerebroAgentDetailPage` (`packages/cerebro-agent-page-redesign/`) | cerebro |
| off | `AgentDetailPage` (`packages/views/agents/`) | upstream |

**`CerebroAgentDetailPage` is the target.** It lives in the cerebro zone, so we
own it outright; every change to the upstream page costs a `CEREBRO-PATCH`
marker and is at risk on the next upstream sync. The legacy page stays only as
the rollback path behind the flag.

## Adding a tab

Cerebro-owned tabs are contributed by **one registry**:
`packages/cerebro-agent-tabs/index.ts`. Both pages read it, so a tab added there
appears on both — this is the only place a tab is registered.

1. Build the tab UI in its own `packages/cerebro-agent-*/` package and export a
   `createAgent<Name>Tabs(): AgentDetailTabExtension[]` factory.
2. Add the factory to `agentDetailTabExtensions()` in
   `packages/cerebro-agent-tabs/index.ts`. Gate it on a feature flag there if it
   needs one — the registry owns flag filtering, not the page and not the tab.
3. Add the English label to `AGENT_DETAIL_TAB_LABELS` (same file) and the i18n
   key under `agents.tabs` in `packages/views/locales/*/agents.json`.

Do **not** add the tab to `BASE_TABS` in
`packages/cerebro-agent-page-redesign/views/agent-page-tabs.ts`. That array is
only the tabs the redesigned page renders itself (Tasks, Instructions, Skills,
Advanced, Integrations).

## What a tab receives

```ts
render(context: { agent: Agent; runtimes: AgentRuntime[]; canEdit: boolean }): ReactNode
```

A tab reports unsaved edits by rendering its own guard; the page-level
unsaved-changes dialog covers the tabs the page owns, not extension tabs.

## Deep links

`?tab=<id>` focuses a tab on arrival. `<id>` is the extension's `id`, so
`?tab=production_prompt` works as soon as the tab is registered and its flag is
on. Both pages honour it.

## Changing an agent's runtime

When `cerebro_agent_setup_capabilities` is on, the runtime is **not** written
from the identity rail. FIR-4000 moved it into the governed change request:
Instructions tab → "How this agent runs" → **Engine** → enter a Change title →
**Save & approve**. The rail shows the current runtime read-only with a link to
that tab. Nothing else writes `runtime_id` from the UI in that mode.

With the flag off, the rail's `RuntimePicker` writes it directly.
