# Eidetix Frontend Config Panel — Design

**Status:** Approved design, pre-implementation
**Date:** 2026-06-16
**Scope:** Web + desktop UI for the per-project Eidetix config (the CLI-only v0 surface, now exposed in the project sidebar). Backend REST endpoints already exist.

## Problem

The Eidetix per-project binding is configured today only via the CLI
(`multica project eidetix set/show/clear/enable/disable`). Owners/admins
managing marketing projects in the web/desktop app have no way to see or change
it without dropping to a terminal. This adds a UI panel over the **existing**
owner/admin-gated REST endpoints — no backend change.

## Backend contract (already shipped)

- `GET /api/projects/{id}/eidetix` → `{configured, enabled, endpoint_url, graph_label}` (never the token). `configured:false` when no row.
- `PUT /api/projects/{id}/eidetix` `{token, endpoint_url?, graph_label?}` → upsert (sticky: omitting endpoint/label preserves them). Returns **503** if the server has no `MULTICA_EIDETIX_SECRET_KEY`.
- `PATCH /api/projects/{id}/eidetix` `{enabled}` → toggle (404 if not configured).
- `DELETE /api/projects/{id}/eidetix` → clear (204).
- All gated to workspace **owner/admin** (members get 403).

## Decisions (from brainstorming)

1. **Placement: a collapsible "Eidetix" section in the project-detail sidebar**, below Resources — mirrors `ProjectResourcesSection`. No new route/modal.
2. **Owner/admin only.** The section renders `null` for other roles (backend also enforces).
3. **Scope: core controls + graph label** — set/replace token, graph label, enable/disable, clear. The endpoint URL is **not** exposed (defaults to the partner SSE URL server-side); out of scope for v1.
4. **Write-only secret UX** — the token is never fetched or displayed. Status shows *configured / not configured*; a "Set / Replace token" affordance reveals a password input. Mirrors the redacted pattern in `agents/components/tabs/mcp-config-tab.tsx`.
5. **Web + desktop share one component** in `packages/views/`; both apps already render the shared `ProjectDetail`, so no per-app routing work.
6. **Mobile out of scope** (independent app).

## Components

### C1. Core — schema (`packages/core/api/schemas.ts`)

```ts
export const EidetixConfigSchema = z.object({
  configured: z.boolean(),
  enabled: z.boolean(),
  endpoint_url: z.string().optional().default(""),
  graph_label: z.string().optional().default(""),
}).loose();
export type EidetixConfig = z.infer<typeof EidetixConfigSchema>;
export const EMPTY_EIDETIX_CONFIG: EidetixConfig = {
  configured: false, enabled: false, endpoint_url: "", graph_label: "",
};
```

### C2. Core — api client (`packages/core/api/client.ts`)

Flat methods on `ApiClient` (matching `getProject`/`updateProject`):
- `getProjectEidetix(projectId)` → `parseWithFallback(data, EidetixConfigSchema, EMPTY_EIDETIX_CONFIG, { endpoint: "GET /api/projects/{id}/eidetix" })`.
- `setProjectEidetix(projectId, { token, graph_label? })` → PUT, parsed.
- `toggleProjectEidetix(projectId, enabled)` → PATCH `{enabled}`, parsed.
- `clearProjectEidetix(projectId)` → DELETE.

The token is a request-body field only; never logged, never stored client-side, never placed in a query string.

### C3. Core — queries + mutations (`packages/core/projects/{queries,mutations}.ts`)

- `projectKeys.eidetix(wsId, projectId)` cache key; `projectEidetixOptions(wsId, projectId)`.
- `useSetProjectEidetix(projectId)` (PUT), `useToggleProjectEidetix(projectId)` (PATCH, optimistic on `enabled` with rollback), `useClearProjectEidetix(projectId)` (DELETE). All invalidate `projectKeys.eidetix` on settle. Workspace-scoped via `useWorkspaceId()` per the keying rule.

### C4. View — section (`packages/views/projects/components/eidetix-section.tsx`)

`<ProjectEidetixSection projectId />`, a collapsible section mirroring `ProjectResourcesSection`:
- Gated: `const { role } = useCurrentMember(wsId); if (role !== "owner" && role !== "admin") return null;`
- Reads `projectEidetixOptions`. States:
  - **Not configured** → "Set token" affordance (password input + optional graph-label field) → `useSetProjectEidetix`.
  - **Configured** → shows graph label + status, an enable/disable toggle (`useToggleProjectEidetix`), "Replace token" (re-reveals the input), and "Clear" (confirm → `useClearProjectEidetix`).
- The token input is a password field, cleared on submit; the existing token is never shown.
- Wired into `packages/views/projects/components/project-detail.tsx` sidebar, after `<ProjectResourcesSection />`.

### C5. i18n

New keys under a `project.eidetix` namespace in **en and zh** locale files
(`packages/views/locales/`), following the conventions glossary
(`apps/docs/content/docs/developers/conventions.mdx`). Strings: section title,
status (configured / not configured), set/replace token, token field label +
placeholder, graph-label field, enable/disable, clear + confirm, the
server-not-enabled (503) error.

## Error handling

- **GET failure / contract drift** → `parseWithFallback` → `EMPTY_EIDETIX_CONFIG`; the section shows "not configured", never white-screens.
- **PUT 503** (server has no encryption key) → error toast: "Eidetix isn't enabled on this server." No optimistic state to roll back (set is not optimistic).
- **PATCH error** → optimistic toggle rolls back; toast.
- **Member somehow reaches it** → backend 403; section is hidden client-side anyway.

## Testing

- **`packages/core`**: schema test — a malformed/partial response (missing field, wrong type, null) through `EidetixConfigSchema` returns the fallback (per the API Response Compatibility rule). One mutation test that the set call posts `{token, graph_label}` and the token never lands anywhere but the request body.
- **`packages/views`**: section test (jsdom, mock `@multica/core`) — renders for owner/admin, returns null for member; "Set token" → calls `useSetProjectEidetix`; toggle → `useToggleProjectEidetix`; clear → `useClearProjectEidetix`; and the existing token value is never rendered to the DOM.
- No app-level (`apps/web` / `apps/desktop`) tests needed — the component is shared and framework-agnostic.

## Out of scope (v1)

- Endpoint-URL override (hidden; server default).
- Mobile.
- Surfacing Eidetix usage/telemetry in the UI.
- A workspace-level (cross-project) Eidetix admin view.

## Documentation upkeep

No built-in agent skill documents the *web UI*, so no `SKILL.md` change is
required (unlike the CLI command, which updated the projects source map). If a
user-facing docs page lists project settings, add the Eidetix section there.
