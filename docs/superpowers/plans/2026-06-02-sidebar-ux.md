# Sidebar Hybrid Navigation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 落地已批准的 hybrid 导航方案，把桌面端改成分组侧边栏加顶部状态栏，并让运行中的番茄钟在桌面和移动端 header 可见。

**Architecture:** 保持现有 `SidebarProvider` 壳层，不做纯顶部导航重写。导航元数据继续集中在 `features/layout/navigation.ts`，新增一个小型番茄状态组件给桌面 header 和移动端 toolbar 复用，再把 sidebar 中重复的搜索、创建、计时入口收口。

**Tech Stack:** TypeScript, React, TanStack Query, shadcn/ui sidebar, lucide-react, Vitest, Testing Library

---

## File Map

| File | Responsibility |
| --- | --- |
| `apps/workspace/src/features/layout/navigation.ts` | 把单层导航定义改成分组结构，并提供当前页面标题解析函数 |
| `apps/workspace/src/features/layout/navigation.test.ts` | 约束分组顺序、入口收口规则、标题映射 |
| `apps/workspace/src/features/layout/components/app-sidebar.tsx` | 渲染桌面分组侧边栏，移除 sidebar header/footer 里的重复全局动作 |
| `apps/workspace/src/features/layout/components/desktop-workspace-header.tsx` | 新增桌面顶部状态栏，承载页面标题、搜索、创建入口、番茄状态 pill |
| `apps/workspace/src/features/layout/components/desktop-workspace-header.test.tsx` | 约束桌面 header 的标题、全局动作和番茄状态挂载 |
| `apps/workspace/src/features/layout/components/dashboard-layout.tsx` | 在桌面内容区上方接入新的 header |
| `apps/workspace/src/features/layout/components/mobile-workspace-toolbar.tsx` | 把移动端 toolbar 从 workspace 名称改为页面标题，并挂载番茄状态 pill |
| `apps/workspace/src/features/layout/components/mobile-workspace-toolbar.test.tsx` | 约束移动端 toolbar 的标题和番茄状态布局 |
| `apps/workspace/src/features/time-tracking/components/PomodoroStatusPill.tsx` | 新增番茄状态 pill，运行时显示阶段和剩余时间，点击跳转 `/pomodoro` |
| `apps/workspace/src/features/time-tracking/components/PomodoroStatusPill.test.tsx` | 约束 idle、work、break 三种状态 |
| `apps/workspace/src/features/time-tracking/lib/pomodoro-display.ts` | 抽取番茄显示逻辑，统一剩余时间和阶段文案计算 |
| `apps/workspace/src/features/time-tracking/components/GlobalTimerWidget.tsx` | 保持原有页面级能力，不再由 shell 直接引用 |
| `apps/workspace/src/features/time-tracking/index.ts` | 从 feature 根导出新的 `PomodoroStatusPill` 组件 |
| `docs/superpowers/specs/2026-06-02-sidebar-ux/*.md` | 用最终实现回写 research/design/spec/tasks 的现状和验收状态 |

---

## Task 1: Restructure navigation metadata and grouped desktop sidebar

**Files:**
- Create: `apps/workspace/src/features/layout/navigation.test.ts`
- Modify: `apps/workspace/src/features/layout/navigation.ts`
- Modify: `apps/workspace/src/features/layout/components/app-sidebar.tsx`

- [ ] **Step 1: Write the failing navigation metadata test**

Create `apps/workspace/src/features/layout/navigation.test.ts`:

```typescript
import { describe, expect, it } from "vitest";
import {
  getWorkspacePageTitle,
  navigationGroups,
  workspaceFooterNav,
} from "./navigation";

describe("layout navigation metadata", () => {
  it("keeps the approved group order and time-entry consolidation", () => {
    expect(navigationGroups.map((group) => group.label)).toEqual([
      "Focus",
      "Planning",
      "Tools",
      "Workspace",
    ]);

    expect(navigationGroups[0].items.map((item) => item.label)).toEqual([
      "Inbox",
      "My Work",
      "Issues",
    ]);

    expect(navigationGroups[2].items.map((item) => item.label)).toEqual(["Pomodoro"]);

    const labels = navigationGroups.flatMap((group) => group.items.map((item) => item.label));
    expect(labels).not.toContain("Notifications");
    expect(labels).not.toContain("Track time");
    expect(labels).not.toContain("My Time");
    expect(labels).not.toContain("Team Time");
  });

  it("maps shell titles from the current pathname", () => {
    expect(getWorkspacePageTitle("/")).toBe("Inbox");
    expect(getWorkspacePageTitle("/notifications")).toBe("Inbox");
    expect(getWorkspacePageTitle("/issues/issue-123")).toBe("Issues");
    expect(getWorkspacePageTitle("/projects/project-1")).toBe("Projects");
    expect(getWorkspacePageTitle("/pomodoro")).toBe("Pomodoro");
    expect(getWorkspacePageTitle("/settings")).toBe("Settings");
  });

  it("keeps settings and logout in the footer-only workspace section", () => {
    expect(workspaceFooterNav.map((item) => item.label)).toEqual([
      "Settings",
      "Log out",
    ]);
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run:

```bash
cd /Users/chenjiaming/Developer/code/github/multica && pnpm --filter @multica/workspace exec vitest run src/features/layout/navigation.test.ts
```

Expected: FAIL because `navigationGroups`, `workspaceFooterNav`, and `getWorkspacePageTitle` do not exist yet.

- [ ] **Step 3: Implement grouped navigation metadata in `navigation.ts`**

Replace the single `primaryNav` / `workspaceNav` structure with grouped metadata:

```typescript
import {
  Bot,
  CalendarDays,
  CalendarRange,
  Columns3,
  FolderKanban,
  Inbox,
  ListTodo,
  LogOut,
  Settings,
  Timer,
  CircleUser,
} from "lucide-react";

export interface WorkspaceNavItem {
  href: string;
  label: string;
  icon: React.ComponentType<{ className?: string }>;
}

export interface WorkspaceNavGroup {
  label: string;
  items: WorkspaceNavItem[];
}

export const navigationGroups: WorkspaceNavGroup[] = [
  {
    label: "Focus",
    items: [
      { href: "/notifications", label: "Inbox", icon: Inbox },
      { href: "/my-work", label: "My Work", icon: CircleUser },
      { href: "/issues", label: "Issues", icon: ListTodo },
    ],
  },
  {
    label: "Planning",
    items: [
      { href: "/projects", label: "Projects", icon: FolderKanban },
      { href: "/board", label: "Board", icon: Columns3 },
      { href: "/backlog", label: "Backlog", icon: ListTodo },
      { href: "/today", label: "Today", icon: CalendarDays },
      { href: "/upcoming", label: "Upcoming", icon: CalendarRange },
      { href: "/calendar", label: "Calendar", icon: CalendarDays },
    ],
  },
  {
    label: "Tools",
    items: [{ href: "/pomodoro", label: "Pomodoro", icon: Timer }],
  },
  {
    label: "Workspace",
    items: [{ href: "/agents", label: "Agents", icon: Bot }],
  },
];

export const workspaceFooterNav: WorkspaceNavItem[] = [
  { href: "/settings", label: "Settings", icon: Settings },
  { href: "/logout", label: "Log out", icon: LogOut },
];

export function getWorkspacePageTitle(pathname: string): string {
  const match = navigationGroups
    .flatMap((group) => group.items)
    .find((item) => isWorkspaceNavActive(pathname, item.href));

  if (match) return match.label;
  if (pathname === "/settings") return "Settings";
  return "Workspace";
}
```

Keep `isWorkspaceNavActive()` and update it so `/notifications` remains active for `/`, `/inbox`, and `/notifications`.

- [ ] **Step 4: Update `AppSidebar` to render groups and remove duplicated global actions**

Replace the sidebar structure with grouped rendering. The header should keep only the workspace switcher. The footer should render settings and logout. Search and “New issue” move to the desktop header in Task 3.

```tsx
<SidebarHeader className="py-3">
  <SidebarMenu className="min-w-0 flex-1">
    <SidebarMenuItem>{/* existing workspace switcher dropdown */}</SidebarMenuItem>
  </SidebarMenu>
</SidebarHeader>

<SidebarContent>
  {navigationGroups.map((group) => (
    <SidebarGroup key={group.label}>
      <SidebarGroupLabel>{group.label}</SidebarGroupLabel>
      <SidebarGroupContent>
        <SidebarMenu className="gap-0.5">
          {group.items.map((item) => {
            const isActive = isWorkspaceNavActive(pathname, item.href);
            return (
              <SidebarMenuItem key={item.href}>
                <SidebarMenuButton
                  isActive={isActive}
                  render={<Link href={item.href} />}
                  className="text-muted-foreground hover:not-data-active:bg-sidebar-accent/70 data-active:bg-sidebar-accent data-active:text-sidebar-accent-foreground"
                  onClick={closeMobileSidebar}
                >
                  <item.icon />
                  <span>{item.label}</span>
                  {item.label === "Inbox" && unreadCount > 0 && (
                    <span className="ml-auto text-xs">{unreadCount > 99 ? "99+" : unreadCount}</span>
                  )}
                </SidebarMenuButton>
              </SidebarMenuItem>
            );
          })}
        </SidebarMenu>
      </SidebarGroupContent>
    </SidebarGroup>
  ))}
</SidebarContent>

<SidebarFooter>
  <SidebarMenu className="gap-0.5">
    <SidebarMenuItem>
      <SidebarMenuButton render={<Link href="/settings" />} onClick={closeMobileSidebar}>
        <Settings />
        <span>Settings</span>
      </SidebarMenuButton>
    </SidebarMenuItem>
    <SidebarMenuItem>
      <SidebarMenuButton className="text-muted-foreground hover:text-destructive" onClick={logout}>
        <LogOut />
        <span>Log out</span>
      </SidebarMenuButton>
    </SidebarMenuItem>
  </SidebarMenu>
</SidebarFooter>
```

- [ ] **Step 5: Run the focused test and typecheck**

Run:

```bash
cd /Users/chenjiaming/Developer/code/github/multica && pnpm --filter @multica/workspace exec vitest run src/features/layout/navigation.test.ts && pnpm typecheck
```

Expected: PASS for the new test, and no new type errors.

- [ ] **Step 6: Commit**

```bash
git add apps/workspace/src/features/layout/navigation.ts \
        apps/workspace/src/features/layout/navigation.test.ts \
        apps/workspace/src/features/layout/components/app-sidebar.tsx
git commit -m "feat(workspace): regroup sidebar navigation around focus planning and tools

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 2: Add a reusable Pomodoro status pill for shell headers

**Files:**
- Create: `apps/workspace/src/features/time-tracking/lib/pomodoro-display.ts`
- Create: `apps/workspace/src/features/time-tracking/components/PomodoroStatusPill.tsx`
- Create: `apps/workspace/src/features/time-tracking/components/PomodoroStatusPill.test.tsx`
- Modify: `apps/workspace/src/features/time-tracking/index.ts`

- [ ] **Step 1: Write the failing status-pill test**

Create `apps/workspace/src/features/time-tracking/components/PomodoroStatusPill.test.tsx`:

```tsx
import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { PomodoroStatusPill } from "./PomodoroStatusPill";

const queryMocks = vi.hoisted(() => ({
  usePomodoroQuery: vi.fn(),
}));

vi.mock("../hooks/use-pomodoro", () => ({
  usePomodoroQuery: queryMocks.usePomodoroQuery,
}));

vi.mock("@/shared/router", () => ({
  Link: ({ href, children }: { href: string; children: React.ReactNode }) => (
    <a href={href}>{children}</a>
  ),
}));

describe("PomodoroStatusPill", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-06-02T00:00:00.000Z"));
  });

  it("renders nothing when there is no active running session", () => {
    queryMocks.usePomodoroQuery.mockReturnValue({ data: null });
    const { container } = render(<PomodoroStatusPill />);
    expect(container).toBeEmptyDOMElement();
  });

  it("shows the work label and remaining time for a running work session", () => {
    queryMocks.usePomodoroQuery.mockReturnValue({
      data: {
        status: "running",
        phase: "work",
        elapsed_seconds: 120,
        phase_duration_seconds: 1500,
        started_at: "2026-06-02T00:00:00.000Z",
      },
    });

    render(<PomodoroStatusPill />);

    expect(screen.getByRole("link", { name: /focus/i })).toHaveAttribute("href", "/pomodoro");
    expect(screen.getByText(/focus/i)).toBeInTheDocument();
    expect(screen.getByText("23:00")).toBeInTheDocument();
  });

  it("shows the break label for a running break session", () => {
    queryMocks.usePomodoroQuery.mockReturnValue({
      data: {
        status: "running",
        phase: "short_break",
        elapsed_seconds: 60,
        phase_duration_seconds: 300,
        started_at: "2026-06-02T00:00:00.000Z",
      },
    });

    render(<PomodoroStatusPill />);
    expect(screen.getByText(/break/i)).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run:

```bash
cd /Users/chenjiaming/Developer/code/github/multica && pnpm --filter @multica/workspace exec vitest run src/features/time-tracking/components/PomodoroStatusPill.test.tsx
```

Expected: FAIL because the pill component and display helper do not exist yet.

- [ ] **Step 3: Implement the shared display helper and pill component**

Create `apps/workspace/src/features/time-tracking/lib/pomodoro-display.ts`:

```typescript
import type { PomodoroSession } from "@/shared/types";

export function getPomodoroRemaining(session: PomodoroSession): number {
  if (session.status === "running" && session.started_at) {
    const runningFor = (Date.now() - new Date(session.started_at).getTime()) / 1000;
    return Math.max(0, Math.round(session.phase_duration_seconds - session.elapsed_seconds - runningFor));
  }
  return Math.max(0, session.phase_duration_seconds - session.elapsed_seconds);
}

export function getPomodoroHeaderLabel(session: PomodoroSession): "Focus" | "Break" {
  return session.phase === "work" ? "Focus" : "Break";
}

export function formatPomodoroRemaining(totalSeconds: number): string {
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  return `${String(minutes).padStart(2, "0")}:${String(seconds).padStart(2, "0")}`;
}
```

Create `apps/workspace/src/features/time-tracking/components/PomodoroStatusPill.tsx`:

```tsx
"use client";

import { useEffect, useState } from "react";
import { Timer } from "lucide-react";
import { Link } from "@/shared/router";
import { usePomodoroQuery } from "../hooks/use-pomodoro";
import {
  formatPomodoroRemaining,
  getPomodoroHeaderLabel,
  getPomodoroRemaining,
} from "../lib/pomodoro-display";

export function PomodoroStatusPill() {
  const { data: session } = usePomodoroQuery();
  const [remaining, setRemaining] = useState<number | null>(null);

  useEffect(() => {
    if (!session || session.status !== "running") {
      setRemaining(null);
      return;
    }

    const tick = () => setRemaining(getPomodoroRemaining(session));
    tick();
    const id = window.setInterval(tick, 1000);
    return () => window.clearInterval(id);
  }, [session]);

  if (!session || session.status !== "running" || remaining === null) {
    return null;
  }

  return (
    <Link
      href="/pomodoro"
      className="inline-flex items-center gap-2 rounded-full border bg-background px-3 py-1 text-sm font-medium text-foreground shadow-sm"
      aria-label={`${getPomodoroHeaderLabel(session)} ${formatPomodoroRemaining(remaining)}`}
    >
      <Timer className="size-3.5 text-brand" />
      <span>{getPomodoroHeaderLabel(session)}</span>
      <span className="tabular-nums text-muted-foreground">
        {formatPomodoroRemaining(remaining)}
      </span>
    </Link>
  );
}
```

Export the component from `apps/workspace/src/features/time-tracking/index.ts`:

```typescript
export { PomodoroStatusPill } from "./components/PomodoroStatusPill";
```

- [ ] **Step 4: Run the new pill test and the existing pomodoro tests**

Run:

```bash
cd /Users/chenjiaming/Developer/code/github/multica && pnpm --filter @multica/workspace exec vitest run \
  src/features/time-tracking/components/PomodoroStatusPill.test.tsx \
  src/features/time-tracking/components/PomodoroTimer.test.tsx \
  src/features/time-tracking/components/GlobalTimerWidget.test.tsx
```

Expected: PASS. The new pill test passes, and existing timer tests continue to pass.

- [ ] **Step 5: Commit**

```bash
git add apps/workspace/src/features/time-tracking/lib/pomodoro-display.ts \
        apps/workspace/src/features/time-tracking/components/PomodoroStatusPill.tsx \
        apps/workspace/src/features/time-tracking/components/PomodoroStatusPill.test.tsx \
        apps/workspace/src/features/time-tracking/index.ts
git commit -m "feat(workspace): add reusable pomodoro status pill for shell headers

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 3: Add the desktop header and remove duplicated sidebar actions

**Files:**
- Create: `apps/workspace/src/features/layout/components/desktop-workspace-header.tsx`
- Create: `apps/workspace/src/features/layout/components/desktop-workspace-header.test.tsx`
- Modify: `apps/workspace/src/features/layout/components/dashboard-layout.tsx`
- Modify: `apps/workspace/src/features/layout/components/app-sidebar.tsx`

- [ ] **Step 1: Write the failing desktop header test**

Create `apps/workspace/src/features/layout/components/desktop-workspace-header.test.tsx`:

```tsx
import React from "react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { DesktopWorkspaceHeader } from "./desktop-workspace-header";

const modalMocks = vi.hoisted(() => ({
  open: vi.fn(),
}));

const searchMocks = vi.hoisted(() => ({
  open: vi.fn(),
}));

let mockPathname = "/issues";

vi.mock("@/shared/router", () => ({
  usePathname: () => mockPathname,
}));

vi.mock("@/features/modals", () => ({
  useModalStore: {
    getState: () => modalMocks,
  },
}));

vi.mock("@/features/search", () => ({
  useSearchStore: {
    getState: () => searchMocks,
  },
}));

vi.mock("@/features/time-tracking", () => ({
  PomodoroStatusPill: () => <div>Pomodoro pill</div>,
}));

describe("DesktopWorkspaceHeader", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockPathname = "/issues";
  });

  it("renders the current page title and global actions", async () => {
    const user = userEvent.setup();
    render(<DesktopWorkspaceHeader />);

    expect(screen.getByText("Issues")).toBeInTheDocument();
    expect(screen.getByText("Pomodoro pill")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /search/i }));
    expect(searchMocks.open).toHaveBeenCalledTimes(1);

    await user.click(screen.getByRole("button", { name: /new issue/i }));
    expect(modalMocks.open).toHaveBeenCalledWith("create-issue");
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run:

```bash
cd /Users/chenjiaming/Developer/code/github/multica && pnpm --filter @multica/workspace exec vitest run src/features/layout/components/desktop-workspace-header.test.tsx
```

Expected: FAIL because `DesktopWorkspaceHeader` does not exist yet.

- [ ] **Step 3: Create the desktop header component**

Create `apps/workspace/src/features/layout/components/desktop-workspace-header.tsx`:

```tsx
"use client";

import { Search, SquarePen } from "lucide-react";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useIssueDraftStore } from "@/features/issues/stores/draft-store";
import { useModalStore } from "@/features/modals";
import { useSearchStore } from "@/features/search";
import { PomodoroStatusPill } from "@/features/time-tracking";
import { usePathname } from "@/shared/router";
import { getWorkspacePageTitle } from "../navigation";

function DraftDot() {
  const hasDraft = useIssueDraftStore((s) => !!(s.draft.title || s.draft.description));
  if (!hasDraft) return null;
  return <span className="absolute top-0 right-0 size-1.5 rounded-full bg-brand" />;
}

export function DesktopWorkspaceHeader() {
  const pathname = usePathname();

  return (
    <div className="hidden h-14 shrink-0 items-center justify-between border-b bg-background/95 px-4 backdrop-blur md:flex">
      <div className="min-w-0">
        <span className="block truncate text-sm font-semibold">
          {getWorkspacePageTitle(pathname)}
        </span>
      </div>

      <div className="flex items-center gap-2">
        <PomodoroStatusPill />
        <Tooltip>
          <TooltipTrigger
            className="relative flex h-8 w-8 items-center justify-center rounded-lg bg-background text-foreground shadow-sm hover:bg-accent"
            aria-label="Search"
            onClick={() => useSearchStore.getState().open()}
          >
            <Search className="size-4" />
          </TooltipTrigger>
          <TooltipContent side="bottom">Search (⌘K)</TooltipContent>
        </Tooltip>
        <Tooltip>
          <TooltipTrigger
            className="relative flex h-8 w-8 items-center justify-center rounded-lg bg-background text-foreground shadow-sm hover:bg-accent"
            aria-label="New issue"
            onClick={() => useModalStore.getState().open("create-issue")}
          >
            <SquarePen className="size-4" />
            <DraftDot />
          </TooltipTrigger>
          <TooltipContent side="bottom">New issue</TooltipContent>
        </Tooltip>
      </div>
    </div>
  );
}
```

- [ ] **Step 4: Mount the desktop header and strip duplicated sidebar timer/actions**

Update `dashboard-layout.tsx` to mount the new header:

```tsx
import { DesktopWorkspaceHeader } from "./desktop-workspace-header";

<SidebarInset className="overflow-hidden">
  <MobileWorkspaceToolbar />
  <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
    <DesktopWorkspaceHeader />
    {workspace ? (
      children
    ) : (
      <div className="flex flex-1 items-center justify-center">
        <MulticaIcon className="size-6 animate-pulse" />
      </div>
    )}
  </div>
</SidebarInset>
```

Then remove search / create buttons from `AppSidebar` and remove `GlobalTimerWidget` from the footer. After this step, the shell has exactly one high-visibility Pomodoro surface: the new header pill.

```tsx
<SidebarFooter>
  <SidebarMenu className="gap-0.5">
    {workspaceFooterNav.map((item) => (
      <SidebarMenuItem key={item.href}>
        <SidebarMenuButton
          render={item.href === "/logout" ? undefined : <Link href={item.href} />}
          onClick={
            item.href === "/logout"
              ? async () => {
                  await logout();
                  closeMobileSidebar();
                }
              : closeMobileSidebar
          }
        >
          <item.icon />
          <span>{item.label}</span>
        </SidebarMenuButton>
      </SidebarMenuItem>
    ))}
  </SidebarMenu>
</SidebarFooter>
```

- [ ] **Step 5: Run the desktop-header test and typecheck**

Run:

```bash
cd /Users/chenjiaming/Developer/code/github/multica && pnpm --filter @multica/workspace exec vitest run \
  src/features/layout/components/desktop-workspace-header.test.tsx \
  src/features/layout/navigation.test.ts && pnpm typecheck
```

Expected: PASS for the new desktop header test and no new type errors.

- [ ] **Step 6: Commit**

```bash
git add apps/workspace/src/features/layout/components/desktop-workspace-header.tsx \
        apps/workspace/src/features/layout/components/desktop-workspace-header.test.tsx \
        apps/workspace/src/features/layout/components/dashboard-layout.tsx \
        apps/workspace/src/features/layout/components/app-sidebar.tsx
git commit -m "feat(workspace): move global shell actions and pomodoro status into desktop header

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 4: Align the mobile toolbar with the approved small-screen shell

**Files:**
- Create: `apps/workspace/src/features/layout/components/mobile-workspace-toolbar.test.tsx`
- Modify: `apps/workspace/src/features/layout/components/mobile-workspace-toolbar.tsx`

- [ ] **Step 1: Write the failing mobile-toolbar test**

Create `apps/workspace/src/features/layout/components/mobile-workspace-toolbar.test.tsx`:

```tsx
import React from "react";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { MobileWorkspaceToolbar } from "./mobile-workspace-toolbar";

let mockPathname = "/notifications";

vi.mock("@/shared/router", () => ({
  usePathname: () => mockPathname,
}));

vi.mock("@/components/ui/sidebar", () => ({
  SidebarTrigger: (props: React.ButtonHTMLAttributes<HTMLButtonElement>) => (
    <button type="button" {...props}>Open</button>
  ),
}));

vi.mock("@/features/time-tracking", () => ({
  PomodoroStatusPill: () => <div>Pomodoro pill</div>,
}));

describe("MobileWorkspaceToolbar", () => {
  beforeEach(() => {
    mockPathname = "/notifications";
  });

  it("renders the current page title instead of the workspace label", () => {
    render(<MobileWorkspaceToolbar />);
    expect(screen.getByText("Inbox")).toBeInTheDocument();
    expect(screen.queryByText("Workspace")).not.toBeInTheDocument();
    expect(screen.getByText("Pomodoro pill")).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run:

```bash
cd /Users/chenjiaming/Developer/code/github/multica && pnpm --filter @multica/workspace exec vitest run src/features/layout/components/mobile-workspace-toolbar.test.tsx
```

Expected: FAIL because the toolbar still renders workspace name and has no pill slot.

- [ ] **Step 3: Update the mobile toolbar to use page title + Pomodoro pill**

Replace `mobile-workspace-toolbar.tsx` with a title-first toolbar:

```tsx
"use client";

import { SquarePen } from "lucide-react";
import { SidebarTrigger } from "@/components/ui/sidebar";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useIssueDraftStore } from "@/features/issues/stores/draft-store";
import { useModalStore } from "@/features/modals";
import { PomodoroStatusPill } from "@/features/time-tracking";
import { usePathname } from "@/shared/router";
import { getWorkspacePageTitle } from "../navigation";

function DraftDot() {
  const hasDraft = useIssueDraftStore((s) => !!(s.draft.title || s.draft.description));
  if (!hasDraft) return null;
  return <span className="absolute top-0.5 right-0.5 size-1.5 rounded-full bg-brand" />;
}

export function MobileWorkspaceToolbar() {
  const pathname = usePathname();

  return (
    <div className="sticky top-0 z-20 flex h-14 shrink-0 items-center gap-2 border-b bg-background/95 px-3 backdrop-blur md:hidden">
      <SidebarTrigger aria-label="Open navigation" className="text-muted-foreground" />
      <div className="min-w-0 flex-1">
        <span className="block truncate text-sm font-semibold">
          {getWorkspacePageTitle(pathname)}
        </span>
      </div>
      <div className="shrink-0">
        <PomodoroStatusPill />
      </div>
      <Tooltip>
        <TooltipTrigger
          className="relative flex h-7 w-7 items-center justify-center rounded-lg bg-muted text-foreground hover:bg-accent"
          aria-label="New issue"
          onClick={() => useModalStore.getState().open("create-issue")}
        >
          <SquarePen className="size-3.5" />
          <DraftDot />
        </TooltipTrigger>
        <TooltipContent side="bottom">New issue</TooltipContent>
      </Tooltip>
    </div>
  );
}
```

- [ ] **Step 4: Run the mobile-toolbar test and a full focused shell suite**

Run:

```bash
cd /Users/chenjiaming/Developer/code/github/multica && pnpm --filter @multica/workspace exec vitest run \
  src/features/layout/navigation.test.ts \
  src/features/layout/components/desktop-workspace-header.test.tsx \
  src/features/layout/components/mobile-workspace-toolbar.test.tsx \
  src/features/time-tracking/components/PomodoroStatusPill.test.tsx
```

Expected: PASS. The shell metadata and both headers are now covered.

- [ ] **Step 5: Commit**

```bash
git add apps/workspace/src/features/layout/components/mobile-workspace-toolbar.tsx \
        apps/workspace/src/features/layout/components/mobile-workspace-toolbar.test.tsx
git commit -m "feat(workspace): align mobile toolbar with hybrid navigation shell

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 5: Sync the approved spec package and run final verification

**Files:**
- Modify: `docs/superpowers/specs/2026-06-02-sidebar-ux/spec.md`
- Modify: `docs/superpowers/specs/2026-06-02-sidebar-ux/research.md`
- Modify: `docs/superpowers/specs/2026-06-02-sidebar-ux/design.md`
- Modify: `docs/superpowers/specs/2026-06-02-sidebar-ux/tasks.md`

- [ ] **Step 1: Update research/spec/design/tasks with implementation reality**

Apply these content changes after the code lands:

```md
- `research.md`: replace “待设计落地” with the actual component/file names that now own grouped nav and shell headers.
- `spec.md`: keep the ASCII diagrams, add a short “Implemented in” note pointing at `desktop-workspace-header.tsx`, `mobile-workspace-toolbar.tsx`, and `PomodoroStatusPill.tsx`.
- `design.md`: change the risk section from future tense to outcome, and mark the accepted compromise that `Agents` lives in the `Workspace` group.
- `tasks.md`: mark all task slices complete and replace speculative file globs with the exact files changed in implementation.
```

- [ ] **Step 2: Run the final workspace verification commands**

Run:

```bash
cd /Users/chenjiaming/Developer/code/github/multica && pnpm typecheck && pnpm --filter @multica/workspace exec vitest run \
  src/features/layout/navigation.test.ts \
  src/features/layout/components/desktop-workspace-header.test.tsx \
  src/features/layout/components/mobile-workspace-toolbar.test.tsx \
  src/features/time-tracking/components/PomodoroStatusPill.test.tsx \
  src/features/time-tracking/components/PomodoroTimer.test.tsx \
  src/features/time-tracking/components/GlobalTimerWidget.test.tsx
```

Expected: PASS. No new type errors and all targeted tests pass.

- [ ] **Step 3: Commit docs + verification-ready state**

```bash
git add docs/superpowers/specs/2026-06-02-sidebar-ux/spec.md \
        docs/superpowers/specs/2026-06-02-sidebar-ux/research.md \
        docs/superpowers/specs/2026-06-02-sidebar-ux/design.md \
        docs/superpowers/specs/2026-06-02-sidebar-ux/tasks.md
git commit -m "docs: sync sidebar hybrid navigation spec package with implementation

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Self-Review Checklist

- Spec coverage:
  - hybrid desktop shell → Task 1 + Task 3
  - header Pomodoro status → Task 2 + Task 3 + Task 4
  - mobile-friendly drawer/header mapping → Task 4
  - spec package reverse-sync → Task 5
- Placeholder scan:
  - no `TODO` / `TBD`
  - every code step includes concrete snippets
  - every verification step includes exact commands
- Type consistency:
  - `navigationGroups`, `workspaceFooterNav`, `getWorkspacePageTitle`
  - `PomodoroStatusPill`
  - `DesktopWorkspaceHeader`
  - `MobileWorkspaceToolbar`
