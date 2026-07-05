# ObitaX Sidebar And Fullscreen Chat Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a new `obitaX` workspace menu entry that opens a full-screen agent chat page, move `Issues` below `小队`, and remove the sidebar bottom-left Discord/help promo area.

**Architecture:** Reuse the existing shared chat stack in `packages/views/chat` and expose a route-backed full-screen shell instead of building a second chat system. Keep the floating chat overlay available for existing pages, but hide it on the dedicated `obitaX` route and drive sidebar navigation entirely from centralized workspace paths.

**Tech Stack:** Next.js App Router, shared React/TypeScript views in `packages/views`, Zustand chat store, TanStack Query, Vitest.

---

## File Structure

- Modify: `packages/core/paths/paths.ts`
  Add the workspace-scoped `obitax()` path builder used by shared navigation and route wiring.
- Modify: `packages/core/paths/paths.test.ts`
  Add a failing path assertion for the new route.
- Modify: `packages/views/layout/app-sidebar.tsx`
  Insert `obitaX` into the workspace menu, reorder `issues`, and remove the bottom-left Discord/help footer area.
- Modify: `packages/views/layout/app-sidebar.test.tsx`
  Add assertions for the new nav order, new href, and removed footer affordances.
- Modify: `packages/views/locales/en/layout.json`
  Add the `obitaX` nav label.
- Modify: `packages/views/locales/zh-Hans/layout.json`
  Add the `obitaX` nav label.
- Modify: `packages/views/locales/ja/layout.json`
  Add the `obitaX` nav label for locale parity.
- Modify: `packages/views/locales/ko/layout.json`
  Add the `obitaX` nav label for locale parity.
- Create: `packages/views/chat/components/chat-page.tsx`
  Build a full-screen chat page by reusing the existing chat content and interaction model.
- Modify: `packages/views/chat/components/chat-window.tsx`
  Extract shared chat content so the overlay window and full-screen page use the same body.
- Modify: `packages/views/chat/index.ts`
  Export the new `ChatPage`.
- Create: `apps/web/app/[workspaceSlug]/(dashboard)/obitax/page.tsx`
  Mount the shared full-screen `ChatPage`.
- Modify: `apps/web/app/[workspaceSlug]/(dashboard)/layout.tsx`
  Suppress floating `ChatWindow` and `ChatFab` when the current route is `obitax`.
- Create: `docs/system-design/frontend/obitax/design.md`
  Record the current frontend design snapshot for the new route and sidebar behavior.

## Versioning Notes

- New version number: `0.0.1.0` in this plan only as the implementation plan placeholder.
- Version record handling: no `docs/versioning/*` update in implementation unless the user explicitly asks for version registration.
- Fact source status: `docs/versioning/README.md` is missing in this repository, so no active version record target can be confirmed.
- Change summary: add route-backed `obitaX` chat entry and simplify sidebar footer.

### Task 1: Add Route Contract

**Files:**
- Modify: `packages/core/paths/paths.ts`
- Test: `packages/core/paths/paths.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
it("builds the obitax workspace route", () => {
  const ws = paths.workspace("acme");
  expect(ws.obitax()).toBe("/acme/obitax");
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm vitest packages/core/paths/paths.test.ts`
Expected: FAIL because `ws.obitax` does not exist yet.

- [ ] **Step 3: Write minimal implementation**

```ts
function workspaceScoped(slug: string) {
  const ws = `/${encode(slug)}`;
  return {
    root: () => `${ws}/issues`,
    obitax: () => `${ws}/obitax`,
    // existing routes...
  };
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `pnpm vitest packages/core/paths/paths.test.ts`
Expected: PASS.

### Task 2: Change Sidebar Navigation

**Files:**
- Modify: `packages/views/layout/app-sidebar.tsx`
- Modify: `packages/views/layout/app-sidebar.test.tsx`
- Modify: `packages/views/locales/en/layout.json`
- Modify: `packages/views/locales/zh-Hans/layout.json`
- Modify: `packages/views/locales/ja/layout.json`
- Modify: `packages/views/locales/ko/layout.json`

- [ ] **Step 1: Write the failing sidebar test**

```tsx
it("renders obitax above projects and issues below squads", () => {
  render(<AppSidebar />);
  const buttons = screen.getAllByRole("button");
  expect(buttons.some((button) => button.getAttribute("data-href") === "/acme/obitax")).toBe(true);
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm vitest packages/views/layout/app-sidebar.test.tsx`
Expected: FAIL because `/acme/obitax` is not rendered.

- [ ] **Step 3: Implement the sidebar reorder and footer cleanup**

```ts
const workspaceNav = [
  { key: "obitax", labelKey: "obitax", icon: Bot },
  { key: "projects", labelKey: "projects", icon: FolderKanban },
  { key: "autopilots", labelKey: "autopilots", icon: Zap },
  { key: "agents", labelKey: "agents", icon: Bot },
  { key: "squads", labelKey: "squads", icon: Users },
  { key: "issues", labelKey: "issues", icon: ListTodo },
  { key: "usage", labelKey: "usage", icon: BarChart3 },
];

<SidebarFooter className="p-2" />
```

- [ ] **Step 4: Add locale entries**

```json
{
  "nav": {
    "obitax": "obitaX"
  }
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `pnpm vitest packages/views/layout/app-sidebar.test.tsx packages/views/locales/parity.test.ts`
Expected: PASS.

### Task 3: Build The Full-Screen Chat Page

**Files:**
- Create: `packages/views/chat/components/chat-page.tsx`
- Modify: `packages/views/chat/components/chat-window.tsx`
- Modify: `packages/views/chat/index.ts`
- Create: `apps/web/app/[workspaceSlug]/(dashboard)/obitax/page.tsx`
- Modify: `apps/web/app/[workspaceSlug]/(dashboard)/layout.tsx`

- [ ] **Step 1: Write the failing page render test**

```tsx
it("renders the fullscreen chat shell", () => {
  render(<ChatPage />);
  expect(screen.getByText("和你的智能体对话")).toBeInTheDocument();
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm vitest packages/views/chat/components/chat-page.test.tsx`
Expected: FAIL because `ChatPage` does not exist yet.

- [ ] **Step 3: Extract shared chat content and add the page shell**

```tsx
export function ChatPage() {
  return (
    <div className="h-full bg-background">
      <SharedChatSurface mode="page" />
    </div>
  );
}
```

- [ ] **Step 4: Add the web route and hide floating chat on `/obitax`**

```tsx
export default function ObitaXPage() {
  return <ChatPage />;
}
```

```tsx
const pathname = usePathname();
const isObitaX = pathname.endsWith("/obitax");
```

- [ ] **Step 5: Run focused chat tests**

Run: `pnpm vitest packages/views/chat/components/chat-page.test.tsx packages/views/chat/components/chat-window.agent-dropdown.test.tsx`
Expected: PASS.

### Task 4: Update Frontend Design Snapshot

**Files:**
- Create: `docs/system-design/frontend/obitax/design.md`

- [ ] **Step 1: Write the design snapshot**

```md
# ObitaX Frontend Design

- Sidebar places `obitaX` above `项目`.
- `Issues` moves below `小队`.
- `obitaX` is a workspace route at `/{slug}/obitax`.
- The dedicated page reuses the shared chat surface instead of duplicating chat logic.
- The left sidebar footer no longer renders the Discord promo/help bubble area.
```

- [ ] **Step 2: Verify the document reflects the implemented state**

Run: `sed -n '1,120p' docs/system-design/frontend/obitax/design.md`
Expected: Shows the final route and sidebar behavior.

## Self-Review

- Spec coverage: covers new menu item, menu reorder, full-screen chat route, and footer cleanup.
- Placeholder scan: no `TODO` or unspecified paths remain.
- Type consistency: `obitax` naming is used consistently across paths, sidebar keys, and route directory.

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-07-04-obitax-sidebar-chat-plan.md`. User already requested one-pass execution with no further confirmations, so proceed inline with TDD implementation in this session.
