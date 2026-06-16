# Eidetix Frontend Config Panel — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an owner/admin-only "Eidetix" section to the project-detail sidebar (web + desktop) that configures the per-project Eidetix binding over the existing REST endpoints.

**Architecture:** Pure frontend. Core layer (`packages/core`) gains a zod schema, four `ApiClient` methods, a query + three mutations. A shared view (`packages/views/projects/components/eidetix-section.tsx`) renders the collapsible section, gated on `useCurrentMember`. Wired into the shared `ProjectDetail` (already used by both apps). The token is write-only — never fetched or displayed (redacted pattern).

**Tech Stack:** TypeScript, zod (lenient `.loose()` schemas + `parseWithFallback`), TanStack Query, React, shadcn/Base-UI components, `useT` i18n (en/zh-Hans/ja/ko parity).

**Spec:** `docs/superpowers/specs/2026-06-16-eidetix-frontend-design.md`.

---

## File structure

- Modify `packages/core/api/schemas.ts` — `EidetixConfigSchema` + `EMPTY_EIDETIX_CONFIG`.
- Modify `packages/core/api/client.ts` — 4 methods on `ApiClient`.
- Modify `packages/core/projects/queries.ts` — `projectKeys.eidetix` + `projectEidetixOptions`.
- Modify `packages/core/projects/mutations.ts` — `useSetProjectEidetix` / `useToggleProjectEidetix` / `useClearProjectEidetix`.
- Modify `packages/core/projects/index.ts` — export the new query/mutations (confirm barrel exports them).
- Create `packages/core/api/eidetix-schema.test.ts` — schema fallback test.
- Modify `packages/views/locales/{en,zh-Hans,ja,ko}/projects.json` — `eidetix.*` keys (all four, for parity).
- Create `packages/views/projects/components/eidetix-section.tsx` — the section.
- Create `packages/views/projects/components/eidetix-section.test.tsx` — section behavior.
- Modify `packages/views/projects/components/project-detail.tsx` — render the section.

---

### Task 1: Core schema + fallback

**Files:**
- Modify: `packages/core/api/schemas.ts`
- Test: `packages/core/api/eidetix-schema.test.ts`

- [ ] **Step 1: Write the failing test** — create `packages/core/api/eidetix-schema.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { parseWithFallback } from "./schema";
import { EidetixConfigSchema, EMPTY_EIDETIX_CONFIG } from "./schemas";

const opts = { endpoint: "GET /api/projects/{id}/eidetix" };

describe("EidetixConfigSchema", () => {
  it("parses a well-formed response", () => {
    const got = parseWithFallback(
      { configured: true, enabled: true, endpoint_url: "https://e/sse", graph_label: "Marketing" },
      EidetixConfigSchema, EMPTY_EIDETIX_CONFIG, opts,
    );
    expect(got).toEqual({ configured: true, enabled: true, endpoint_url: "https://e/sse", graph_label: "Marketing" });
  });

  it("defaults missing optional fields", () => {
    const got = parseWithFallback({ configured: true, enabled: false }, EidetixConfigSchema, EMPTY_EIDETIX_CONFIG, opts);
    expect(got.endpoint_url).toBe("");
    expect(got.graph_label).toBe("");
  });

  it("falls back when a required field is the wrong type", () => {
    const got = parseWithFallback({ configured: "yes", enabled: true }, EidetixConfigSchema, EMPTY_EIDETIX_CONFIG, opts);
    expect(got).toBe(EMPTY_EIDETIX_CONFIG);
  });

  it("falls back on null", () => {
    expect(parseWithFallback(null, EidetixConfigSchema, EMPTY_EIDETIX_CONFIG, opts)).toBe(EMPTY_EIDETIX_CONFIG);
  });
});
```

- [ ] **Step 2: Run it, expect failure**

Run: `pnpm --filter @multica/core exec vitest run api/eidetix-schema.test.ts`
Expected: FAIL — `EidetixConfigSchema`/`EMPTY_EIDETIX_CONFIG` not exported.

- [ ] **Step 3: Add the schema** — append to `packages/core/api/schemas.ts` (the file already `import { z } from "zod"`):

```ts
// Eidetix per-project config. Token is write-only server-side and never
// present in this response. Lenient per the API-compatibility rule.
export const EidetixConfigSchema = z.object({
  configured: z.boolean(),
  enabled: z.boolean(),
  endpoint_url: z.string().optional().default(""),
  graph_label: z.string().optional().default(""),
}).loose();
export type EidetixConfig = z.infer<typeof EidetixConfigSchema>;
export const EMPTY_EIDETIX_CONFIG: EidetixConfig = {
  configured: false,
  enabled: false,
  endpoint_url: "",
  graph_label: "",
};
```

- [ ] **Step 4: Run it, expect pass**

Run: `pnpm --filter @multica/core exec vitest run api/eidetix-schema.test.ts`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add packages/core/api/schemas.ts packages/core/api/eidetix-schema.test.ts
git commit -m "feat(eidetix-ui): EidetixConfig schema + fallback"
```

---

### Task 2: Core API client methods

**Files:**
- Modify: `packages/core/api/client.ts`

- [ ] **Step 1: Add the import** — in `packages/core/api/client.ts`, the file imports schemas from `./schemas` (e.g. `EMPTY_USER`, `UserSchema`). Add `EidetixConfigSchema, EMPTY_EIDETIX_CONFIG` (and the `EidetixConfig` type) to that existing import block from `./schemas`.

- [ ] **Step 2: Add the four methods** — in the `// Project resources` region of the `ApiClient` class (right after the project-resource methods, mirroring `getProject`/`updateProject`/`deleteProject`):

```ts
  // Project Eidetix config (owner/admin only; token is write-only).
  async getProjectEidetix(projectId: string): Promise<EidetixConfig> {
    const raw = await this.fetch<unknown>(`/api/projects/${projectId}/eidetix`);
    return parseWithFallback(raw, EidetixConfigSchema, EMPTY_EIDETIX_CONFIG, {
      endpoint: "GET /api/projects/{id}/eidetix",
    });
  }

  async setProjectEidetix(
    projectId: string,
    data: { token: string; graph_label?: string },
  ): Promise<EidetixConfig> {
    const raw = await this.fetch<unknown>(`/api/projects/${projectId}/eidetix`, {
      method: "PUT",
      body: JSON.stringify(data),
    });
    return parseWithFallback(raw, EidetixConfigSchema, EMPTY_EIDETIX_CONFIG, {
      endpoint: "PUT /api/projects/{id}/eidetix",
    });
  }

  async toggleProjectEidetix(projectId: string, enabled: boolean): Promise<EidetixConfig> {
    const raw = await this.fetch<unknown>(`/api/projects/${projectId}/eidetix`, {
      method: "PATCH",
      body: JSON.stringify({ enabled }),
    });
    return parseWithFallback(raw, EidetixConfigSchema, EMPTY_EIDETIX_CONFIG, {
      endpoint: "PATCH /api/projects/{id}/eidetix",
    });
  }

  async clearProjectEidetix(projectId: string): Promise<void> {
    await this.fetch(`/api/projects/${projectId}/eidetix`, { method: "DELETE" });
  }
```

`parseWithFallback` is already imported in `client.ts` (from `./schema`). The token only ever appears in the PUT request body — never a query string, never logged.

- [ ] **Step 3: Typecheck**

Run: `pnpm --filter @multica/core typecheck` (or `pnpm typecheck`)
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add packages/core/api/client.ts
git commit -m "feat(eidetix-ui): api client methods for project eidetix config"
```

---

### Task 3: Core query + mutations

**Files:**
- Modify: `packages/core/projects/queries.ts`
- Modify: `packages/core/projects/mutations.ts`
- Modify: `packages/core/projects/index.ts` (only if it re-exports named symbols explicitly — confirm)

- [ ] **Step 1: Add the query** — in `packages/core/projects/queries.ts`, add to `projectKeys` and a new options fn:

```ts
export const projectKeys = {
  all: (wsId: string) => ["projects", wsId] as const,
  list: (wsId: string) => [...projectKeys.all(wsId), "list"] as const,
  detail: (wsId: string, id: string) =>
    [...projectKeys.all(wsId), "detail", id] as const,
  eidetix: (wsId: string, id: string) =>
    [...projectKeys.all(wsId), "eidetix", id] as const,
};

export function projectEidetixOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: projectKeys.eidetix(wsId, id),
    queryFn: () => api.getProjectEidetix(id),
  });
}
```

- [ ] **Step 2: Add the mutations** — append to `packages/core/projects/mutations.ts` (file already imports `useMutation, useQueryClient`, `api`, `projectKeys`, `useWorkspaceId`):

```ts
import type { EidetixConfig } from "../api/schemas";

export function useSetProjectEidetix(projectId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: { token: string; graph_label?: string }) =>
      api.setProjectEidetix(projectId, data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: projectKeys.eidetix(wsId, projectId) });
    },
  });
}

export function useToggleProjectEidetix(projectId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (enabled: boolean) => api.toggleProjectEidetix(projectId, enabled),
    onMutate: async (enabled) => {
      await qc.cancelQueries({ queryKey: projectKeys.eidetix(wsId, projectId) });
      const prev = qc.getQueryData<EidetixConfig>(projectKeys.eidetix(wsId, projectId));
      qc.setQueryData<EidetixConfig>(projectKeys.eidetix(wsId, projectId), (old) =>
        old ? { ...old, enabled } : old,
      );
      return { prev };
    },
    onError: (_err, _enabled, ctx) => {
      if (ctx?.prev) qc.setQueryData(projectKeys.eidetix(wsId, projectId), ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: projectKeys.eidetix(wsId, projectId) });
    },
  });
}

export function useClearProjectEidetix(projectId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: () => api.clearProjectEidetix(projectId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: projectKeys.eidetix(wsId, projectId) });
    },
  });
}
```

(Place the `import type { EidetixConfig }` line with the other imports at the top of the file, not mid-file.)

- [ ] **Step 3: Confirm the barrel re-exports** — `packages/views` imports these via `@multica/core/projects`. Check `packages/core/projects/index.ts`:

Run: `grep -n "queries\|mutations" packages/core/projects/index.ts`
- If it does `export * from "./queries"` / `export * from "./mutations"`, nothing to do.
- If it names exports explicitly, add `projectEidetixOptions`, `useSetProjectEidetix`, `useToggleProjectEidetix`, `useClearProjectEidetix` to the appropriate export lists.

- [ ] **Step 4: Typecheck**

Run: `pnpm --filter @multica/core typecheck`
Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add packages/core/projects/queries.ts packages/core/projects/mutations.ts packages/core/projects/index.ts
git commit -m "feat(eidetix-ui): project eidetix query + mutations"
```

---

### Task 4: i18n keys (all four locales)

**Files:**
- Modify: `packages/views/locales/en/projects.json`
- Modify: `packages/views/locales/zh-Hans/projects.json`
- Modify: `packages/views/locales/ja/projects.json`
- Modify: `packages/views/locales/ko/projects.json`

A `parity.test.ts` enforces identical key sets across all locales, so every key must exist in all four files.

- [ ] **Step 1: Add the `eidetix` block to `en/projects.json`** (place it as a top-level sibling of the existing keys, matching the file's existing JSON shape/indentation):

```json
"eidetix": {
  "title": "Eidetix",
  "status_configured": "Configured",
  "status_not_configured": "Not configured",
  "graph_label": "Graph label",
  "graph_label_placeholder": "e.g. Marketing",
  "token_label": "Bearer token",
  "token_placeholder": "Paste the Eidetix token",
  "set_token": "Set token",
  "replace_token": "Replace token",
  "save": "Save",
  "cancel": "Cancel",
  "enabled": "Enabled",
  "enable": "Enable",
  "disable": "Disable",
  "clear": "Clear",
  "clear_confirm": "Remove the Eidetix binding for this project?",
  "saved": "Eidetix updated",
  "cleared": "Eidetix binding removed",
  "error_server_disabled": "Eidetix isn't enabled on this server",
  "error_generic": "Couldn't update Eidetix"
}
```

- [ ] **Step 2: Mirror into the other three locales.** Add the same block with translated values to `zh-Hans/projects.json`, `ja/projects.json`, `ko/projects.json`. Use the conventions glossary (`apps/docs/content/docs/developers/conventions.zh.mdx`) for the Chinese voice. For ja/ko, translate consistently; keep "Eidetix" as the product name verbatim. (If unsure of a term, a faithful translation that preserves the key set is required for parity — the parity test checks keys, not translation quality.)

Example `zh-Hans/projects.json` block:
```json
"eidetix": {
  "title": "Eidetix",
  "status_configured": "已配置",
  "status_not_configured": "未配置",
  "graph_label": "图谱标签",
  "graph_label_placeholder": "例如 Marketing",
  "token_label": "Bearer 令牌",
  "token_placeholder": "粘贴 Eidetix 令牌",
  "set_token": "设置令牌",
  "replace_token": "替换令牌",
  "save": "保存",
  "cancel": "取消",
  "enabled": "已启用",
  "enable": "启用",
  "disable": "停用",
  "clear": "清除",
  "clear_confirm": "移除该项目的 Eidetix 绑定？",
  "saved": "Eidetix 已更新",
  "cleared": "已移除 Eidetix 绑定",
  "error_server_disabled": "此服务器未启用 Eidetix",
  "error_generic": "无法更新 Eidetix"
}
```

- [ ] **Step 3: Run the parity test**

Run: `pnpm --filter @multica/views exec vitest run locales/parity.test.ts`
Expected: PASS (all locales have identical key sets).

- [ ] **Step 4: Commit**

```bash
git add packages/views/locales/*/projects.json
git commit -m "feat(eidetix-ui): i18n keys for the eidetix project section"
```

---

### Task 5: The section component

**Files:**
- Create: `packages/views/projects/components/eidetix-section.tsx`
- Test: `packages/views/projects/components/eidetix-section.test.tsx`

- [ ] **Step 1: Write the failing test** — create `eidetix-section.test.tsx`. Mirror the mocking style of an existing `packages/views/projects/components/*.test.tsx` (inspect one first for the exact `vi.mock` shape used in this package). It must mock `@multica/core/permissions`, `@multica/core/hooks`, `@multica/core/projects`, and `@tanstack/react-query`'s `useQuery`:

```tsx
import { render, screen, fireEvent } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";

const setMutate = vi.fn();
const toggleMutate = vi.fn();
const clearMutate = vi.fn();
let mockRole = "owner";
let mockData = { configured: true, enabled: true, endpoint_url: "", graph_label: "Marketing" };

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws1" }));
vi.mock("@multica/core/permissions", () => ({ useCurrentMember: () => ({ role: mockRole }) }));
vi.mock("@multica/core/projects", () => ({
  projectEidetixOptions: () => ({ queryKey: ["e"], queryFn: vi.fn() }),
  useSetProjectEidetix: () => ({ mutate: setMutate, isPending: false }),
  useToggleProjectEidetix: () => ({ mutate: toggleMutate, isPending: false }),
  useClearProjectEidetix: () => ({ mutate: clearMutate, isPending: false }),
}));
vi.mock("@tanstack/react-query", () => ({ useQuery: () => ({ data: mockData }) }));

import { ProjectEidetixSection } from "./eidetix-section";

beforeEach(() => {
  setMutate.mockReset(); toggleMutate.mockReset(); clearMutate.mockReset();
  mockRole = "owner";
  mockData = { configured: true, enabled: true, endpoint_url: "", graph_label: "Marketing" };
});

describe("ProjectEidetixSection", () => {
  it("renders nothing for a non-admin member", () => {
    mockRole = "member";
    const { container } = render(<ProjectEidetixSection projectId="p1" />);
    expect(container).toBeEmptyDOMElement();
  });

  it("shows configured status + graph label for an admin", () => {
    render(<ProjectEidetixSection projectId="p1" />);
    expect(screen.getByText("Marketing")).toBeInTheDocument();
  });

  it("toggles enabled", () => {
    render(<ProjectEidetixSection projectId="p1" />);
    fireEvent.click(screen.getByRole("button", { name: /disable/i }));
    expect(toggleMutate).toHaveBeenCalledWith(false);
  });

  it("submits a new token without rendering any existing token", () => {
    mockData = { configured: false, enabled: false, endpoint_url: "", graph_label: "" };
    render(<ProjectEidetixSection projectId="p1" />);
    fireEvent.click(screen.getByRole("button", { name: /set token/i }));
    fireEvent.change(screen.getByLabelText(/bearer token/i), { target: { value: "tok-123" } });
    fireEvent.click(screen.getByRole("button", { name: /save/i }));
    expect(setMutate).toHaveBeenCalledWith(
      expect.objectContaining({ token: "tok-123" }),
      expect.anything(),
    );
  });
});
```

> Adjust `vi.mock` targets to match how this package's existing tests mock `@multica/core` (some mock the `i18n` `useT` too — if `useT` needs mocking, add `vi.mock("../../i18n", () => ({ useT: () => ({ t: (f) => { const o = new Proxy({}, { get: (_t, k) => k }); const r = f(o); return typeof r === "string" ? r : String(r); } }) }))`, or whatever the existing tests do). The labels in assertions (`set token`, `disable`, `bearer token`, `save`) must match the rendered i18n strings — if the test harness returns keys rather than English, assert on keys instead.

- [ ] **Step 2: Run it, expect failure**

Run: `pnpm --filter @multica/views exec vitest run projects/components/eidetix-section.test.tsx`
Expected: FAIL — module not found.

- [ ] **Step 3: Implement the section** — create `packages/views/projects/components/eidetix-section.tsx`. Mirror `project-resources-section.tsx`'s collapsible-section shell and `mcp-config-tab.tsx`'s redacted-secret treatment:

```tsx
"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, Lock } from "lucide-react";
import { toast } from "sonner";
import {
  projectEidetixOptions,
  useSetProjectEidetix,
  useToggleProjectEidetix,
  useClearProjectEidetix,
} from "@multica/core/projects";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentMember } from "@multica/core/permissions";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { useT } from "../../i18n";

export function ProjectEidetixSection({ projectId }: { projectId: string }) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const { role } = useCurrentMember(wsId);
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState(false);
  const [token, setToken] = useState("");
  const [label, setLabel] = useState("");

  const isAdmin = role === "owner" || role === "admin";
  const { data } = useQuery({ ...projectEidetixOptions(wsId, projectId), enabled: isAdmin });
  const setM = useSetProjectEidetix(projectId);
  const toggleM = useToggleProjectEidetix(projectId);
  const clearM = useClearProjectEidetix(projectId);

  // Owner/admin only — members never see this (backend also enforces 403).
  if (!isAdmin) return null;

  const cfg = data ?? { configured: false, enabled: false, endpoint_url: "", graph_label: "" };

  function submitToken() {
    if (!token.trim()) return;
    setM.mutate(
      { token: token.trim(), graph_label: label.trim() || undefined },
      {
        onSuccess: () => {
          setToken(""); setLabel(""); setEditing(false);
          toast.success(t(($) => $.eidetix.saved));
        },
        onError: (err: unknown) => {
          const status = (err as { status?: number })?.status;
          toast.error(status === 503 ? t(($) => $.eidetix.error_server_disabled) : t(($) => $.eidetix.error_generic));
        },
      },
    );
  }

  return (
    <div>
      <button
        type="button"
        className="flex w-full items-center gap-1 text-xs font-medium text-muted-foreground hover:text-foreground"
        onClick={() => setOpen(!open)}
      >
        {t(($) => $.eidetix.title)}
        <ChevronRight className={`h-3 w-3 transition-transform ${open ? "rotate-90" : ""}`} />
      </button>

      {open && (
        <div className="space-y-2 pl-2 pt-1">
          <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <Lock className="h-3 w-3" />
            {cfg.configured ? t(($) => $.eidetix.status_configured) : t(($) => $.eidetix.status_not_configured)}
            {cfg.configured && cfg.graph_label ? ` · ${cfg.graph_label}` : ""}
          </p>

          {cfg.configured && !editing && (
            <div className="flex flex-wrap gap-1.5">
              <Button size="sm" variant="outline" onClick={() => toggleM.mutate(!cfg.enabled)} disabled={toggleM.isPending}>
                {cfg.enabled ? t(($) => $.eidetix.disable) : t(($) => $.eidetix.enable)}
              </Button>
              <Button size="sm" variant="outline" onClick={() => setEditing(true)}>
                {t(($) => $.eidetix.replace_token)}
              </Button>
              <Button
                size="sm"
                variant="outline"
                onClick={() => {
                  if (typeof window !== "undefined" && !window.confirm(t(($) => $.eidetix.clear_confirm))) return;
                  clearM.mutate(undefined, { onSuccess: () => toast.success(t(($) => $.eidetix.cleared)) });
                }}
                disabled={clearM.isPending}
              >
                {t(($) => $.eidetix.clear)}
              </Button>
            </div>
          )}

          {(!cfg.configured || editing) && (
            <div className="space-y-1.5">
              <div className="space-y-1">
                <Label htmlFor="eidetix-token" className="text-xs">{t(($) => $.eidetix.token_label)}</Label>
                <Input
                  id="eidetix-token"
                  type="password"
                  value={token}
                  placeholder={t(($) => $.eidetix.token_placeholder)}
                  onChange={(e) => setToken(e.target.value)}
                />
              </div>
              <div className="space-y-1">
                <Label htmlFor="eidetix-label" className="text-xs">{t(($) => $.eidetix.graph_label)}</Label>
                <Input
                  id="eidetix-label"
                  value={label}
                  placeholder={cfg.graph_label || t(($) => $.eidetix.graph_label_placeholder)}
                  onChange={(e) => setLabel(e.target.value)}
                />
              </div>
              <div className="flex gap-1.5">
                <Button size="sm" onClick={submitToken} disabled={!token.trim() || setM.isPending}>
                  {cfg.configured ? t(($) => $.eidetix.save) : t(($) => $.eidetix.set_token)}
                </Button>
                {editing && (
                  <Button size="sm" variant="ghost" onClick={() => { setEditing(false); setToken(""); setLabel(""); }}>
                    {t(($) => $.eidetix.cancel)}
                  </Button>
                )}
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
```

> Confirm the shadcn `Input` and `Label` import paths against an existing views component (`grep -rn "components/ui/input\|components/ui/label" packages/views | head`). If `Label` isn't used elsewhere, replace it with a plain `<label className="text-xs">`. Confirm `useCurrentMember` is exported from `@multica/core/permissions` (`grep -n "useCurrentMember" packages/core/permissions/index.ts`); if it lives elsewhere, import from the correct path.

- [ ] **Step 4: Run the test, expect pass**

Run: `pnpm --filter @multica/views exec vitest run projects/components/eidetix-section.test.tsx`
Expected: PASS (4 tests). If label-string assertions fail because the harness returns keys, align the assertions to whatever the harness renders.

- [ ] **Step 5: Commit**

```bash
git add packages/views/projects/components/eidetix-section.tsx packages/views/projects/components/eidetix-section.test.tsx
git commit -m "feat(eidetix-ui): project eidetix sidebar section component"
```

---

### Task 6: Wire the section into the project sidebar

**Files:**
- Modify: `packages/views/projects/components/project-detail.tsx`

- [ ] **Step 1: Add the import** — near the existing `import { ProjectResourcesSection } from "./project-resources-section";` (line ~44):

```tsx
import { ProjectEidetixSection } from "./eidetix-section";
```

- [ ] **Step 2: Render it after Resources** — find `<ProjectResourcesSection projectId={projectId} />` (line ~738) and add directly below it:

```tsx
      <ProjectResourcesSection projectId={projectId} />
      <ProjectEidetixSection projectId={projectId} />
```

- [ ] **Step 3: Typecheck + run the views project tests**

Run: `pnpm --filter @multica/views typecheck && pnpm --filter @multica/views exec vitest run projects/`
Expected: no type errors; tests pass.

- [ ] **Step 4: Commit**

```bash
git add packages/views/projects/components/project-detail.tsx
git commit -m "feat(eidetix-ui): render eidetix section in the project sidebar"
```

---

### Task 7: Full verification

- [ ] **Step 1: Typecheck everything**

Run: `pnpm typecheck`
Expected: no errors across packages + apps.

- [ ] **Step 2: Lint**

Run: `pnpm lint`
Expected: clean (fixes any phantom-dep / import errors — e.g. if `@multica/ui` input/label needs declaring in `packages/views/package.json`, add it per the dependency rule).

- [ ] **Step 3: Run the touched package test suites**

Run: `pnpm --filter @multica/core exec vitest run && pnpm --filter @multica/views exec vitest run`
Expected: all green (schema fallback, parity, section behavior).

- [ ] **Step 4: Confirm the token never appears in a read path**

Run: `grep -rn "token" packages/core/api/client.ts | grep -i eidetix` and review — the token must appear only in `setProjectEidetix`'s request body, never in `getProjectEidetix`, a URL, or a log.
Expected: only the PUT body references `token`.

---

## Self-Review

**1. Spec coverage:**
- C1 schema → Task 1. C2 client → Task 2. C3 queries/mutations → Task 3. C4 view section (gating, states, write-only token, toggle, clear) → Task 5; wired → Task 6. C5 i18n (all locales) → Task 4. Error handling (parseWithFallback fallback, 503 toast, optimistic toggle rollback) → Tasks 1/2/3/5. Testing (core schema fallback, views section incl. token-never-rendered + member-hidden) → Tasks 1 & 5. Owner/admin gating → Task 5. Web+desktop sharing → Task 6 (shared component; no per-app work). All spec sections covered.

**2. Placeholder scan:** No TBDs. Every code step has complete code. The two "confirm X against existing code" notes (barrel re-export shape, Input/Label import path, useT mock shape) are verification instructions with concrete fallbacks, not unfilled blanks.

**3. Type consistency:** `EidetixConfig` (Task 1) is consumed by client methods (Task 2) and the toggle mutation's optimistic cache type (Task 3). Method names are identical across tasks: `getProjectEidetix` / `setProjectEidetix` / `toggleProjectEidetix(projectId, enabled)` / `clearProjectEidetix`; hooks `useSetProjectEidetix` / `useToggleProjectEidetix` / `useClearProjectEidetix`; `projectEidetixOptions(wsId, id)` + `projectKeys.eidetix(wsId, id)`. The component (Task 5) imports exactly those names. `toggleM.mutate(boolean)` matches the mutation's `mutationFn: (enabled: boolean)`. `setM.mutate({token, graph_label?})` matches `mutationFn: (data: {token, graph_label?})`.

**Correction vs spec:** the spec said i18n keys go in "en + zh"; the codebase's `parity.test.ts` requires all four locales (en, zh-Hans, ja, ko), so Task 4 covers all four.
