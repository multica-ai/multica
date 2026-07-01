# Mobile: Nav Rearrangement + Collapsible Status Groups Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add per-session collapsible status sections to the project detail screen, and rearrange the bottom nav so Projects/Issues/Pins are top-level tabs while Inbox/My Issues move into the More dropdown.

**Architecture:** Feature 1 adds a Zustand store keyed by `projectId` tracking which status sections are collapsed; `SectionHeader` in `ProjectRelatedIssues` becomes a Pressable. Feature 2 creates five new files with adapted headers, then atomically switches routing and deletes the five old files so the app is never in a broken intermediate state.

**Tech Stack:** Expo Router 55, NativeWind 4, Zustand (no persist middleware), SF Symbols via `expo-image`, `<Header>` component for tab roots, native Stack header for push screens.

## Global Constraints
- Tab root headers: in-body `<Header>` from `@/components/ui/header` (self-handles top safe area via `SafeAreaView edges={["top"]}`)
- Stack push screen headers: native Stack header only; do not use `<Header>` on push screens (its own JSDoc says "tab roots only")
- `<HeaderActions />` from `@/components/ui/app-header-actions` belongs on tab roots only — do not carry it into push screens
- SF Symbols: `expo-image` with `source="sf:<name>"` and `tintColor` from `THEME[colorScheme]`
- No persist middleware on any new Zustand store — in-memory only
- TypeScript strict; no `as T` casts on response bodies
- NativeWind 4; dark mode via `useColorScheme()` + THEME tokens

---

### Task 1: Create project-collapse-store

**Files:**
- Create: `apps/mobile/data/stores/project-collapse-store.ts`
- Create: `apps/mobile/data/stores/project-collapse-store.test.ts`

**Interfaces:**
- Produces: `useProjectCollapseStore` with state `collapsedByProject: Record<string, IssueStatus[]>`, actions `toggle(projectId: string, status: IssueStatus): void` and `isCollapsed(projectId: string, status: IssueStatus): boolean`

- [ ] **Step 1: Write the failing test**

```ts
// apps/mobile/data/stores/project-collapse-store.test.ts
import { describe, it, expect, beforeEach } from "vitest";
import { useProjectCollapseStore } from "./project-collapse-store";

beforeEach(() => {
  useProjectCollapseStore.setState({ collapsedByProject: {} });
});

describe("useProjectCollapseStore", () => {
  it("isCollapsed returns false for unknown project/status", () => {
    const { isCollapsed } = useProjectCollapseStore.getState();
    expect(isCollapsed("proj-1", "todo")).toBe(false);
  });

  it("toggle collapses a status that was open", () => {
    useProjectCollapseStore.getState().toggle("proj-1", "todo");
    expect(useProjectCollapseStore.getState().isCollapsed("proj-1", "todo")).toBe(true);
  });

  it("toggle expands a status that was collapsed", () => {
    useProjectCollapseStore.getState().toggle("proj-1", "todo");
    useProjectCollapseStore.getState().toggle("proj-1", "todo");
    expect(useProjectCollapseStore.getState().isCollapsed("proj-1", "todo")).toBe(false);
  });

  it("toggle on one project does not affect another", () => {
    useProjectCollapseStore.getState().toggle("proj-1", "todo");
    expect(useProjectCollapseStore.getState().isCollapsed("proj-2", "todo")).toBe(false);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd apps/mobile && pnpm test project-collapse-store
```
Expected: FAIL — `Cannot find module './project-collapse-store'`

- [ ] **Step 3: Implement the store**

```ts
// apps/mobile/data/stores/project-collapse-store.ts
import { create } from "zustand";
import type { IssueStatus } from "@multica/core/types";

interface ProjectCollapseState {
  collapsedByProject: Record<string, IssueStatus[]>;
  toggle: (projectId: string, status: IssueStatus) => void;
  isCollapsed: (projectId: string, status: IssueStatus) => boolean;
}

export const useProjectCollapseStore = create<ProjectCollapseState>(
  (set, get) => ({
    collapsedByProject: {},
    toggle: (projectId, status) =>
      set((state) => {
        const current = state.collapsedByProject[projectId] ?? [];
        const next = current.includes(status)
          ? current.filter((s) => s !== status)
          : [...current, status];
        return {
          collapsedByProject: { ...state.collapsedByProject, [projectId]: next },
        };
      }),
    isCollapsed: (projectId, status) =>
      (get().collapsedByProject[projectId] ?? []).includes(status),
  }),
);
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd apps/mobile && pnpm test project-collapse-store
```
Expected: 4 passing

- [ ] **Step 5: Commit**

```bash
git add apps/mobile/data/stores/project-collapse-store.ts apps/mobile/data/stores/project-collapse-store.test.ts
git commit -m "feat(mobile): add project status collapse store"
```

---

### Task 2: Make status sections collapsible in ProjectRelatedIssues

**Files:**
- Modify: `apps/mobile/components/project/project-related-issues.tsx`

**Interfaces:**
- Consumes: `useProjectCollapseStore` — `toggle(projectId, status)` and `isCollapsed(projectId, status)`

- [ ] **Step 1: Add new imports**

In `project-related-issues.tsx`, add to the existing import block:

```tsx
import { Pressable } from "react-native";
import { Image as ExpoImage } from "expo-image";
import { useProjectCollapseStore } from "@/data/stores/project-collapse-store";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";
```

- [ ] **Step 2: Replace `SectionHeader`**

Remove the existing `SectionHeader` function and replace with:

```tsx
function SectionHeader({
  status,
  count,
  collapsed,
  onToggle,
}: {
  status: IssueStatus;
  count: number;
  collapsed: boolean;
  onToggle: () => void;
}) {
  const { colorScheme } = useColorScheme();
  const t = THEME[colorScheme];
  return (
    <Pressable
      onPress={onToggle}
      className="flex-row items-center gap-2 px-4 py-2 bg-background active:bg-secondary"
      accessibilityRole="button"
      accessibilityLabel={`${STATUS_LABEL[status]}, ${count} issues, ${collapsed ? "collapsed" : "expanded"}`}
    >
      <StatusIcon status={status} size={14} />
      <Text className="flex-1 text-xs uppercase tracking-wider text-muted-foreground font-medium">
        {STATUS_LABEL[status]}
      </Text>
      <Text className="text-xs text-muted-foreground/60 mr-1">{count}</Text>
      <ExpoImage
        source="sf:chevron.right"
        tintColor={t.mutedForeground}
        style={{
          width: 12,
          height: 12,
          transform: [{ rotate: collapsed ? "0deg" : "90deg" }],
        }}
      />
    </Pressable>
  );
}
```

- [ ] **Step 3: Update `ProjectRelatedIssues` to consume the store**

Add store reads after the existing hooks (`wsId`, `wsSlug`, query):

```tsx
  const toggle = useProjectCollapseStore((s) => s.toggle);
  const isCollapsed = useProjectCollapseStore((s) => s.isCollapsed);
```

Replace the `return (` block at the bottom of `ProjectRelatedIssues` (the `<View>` with `BOARD_STATUSES.map`):

```tsx
  return (
    <View>
      {BOARD_STATUSES.map((status) => {
        const issues = byStatus.get(status) ?? [];
        if (issues.length === 0) return null;
        const collapsed = isCollapsed(projectId, status);
        return (
          <View key={status}>
            <SectionHeader
              status={status}
              count={issues.length}
              collapsed={collapsed}
              onToggle={() => toggle(projectId, status)}
            />
            {!collapsed &&
              issues.map((issue) => (
                <IssueRow
                  key={issue.id}
                  issue={issue}
                  onPress={() => navigateToIssue(issue.id)}
                />
              ))}
          </View>
        );
      })}
    </View>
  );
```

- [ ] **Step 4: Typecheck**

```bash
cd apps/mobile && pnpm typecheck
```
Expected: no errors in `project-related-issues.tsx`

- [ ] **Step 5: Commit**

```bash
git add apps/mobile/components/project/project-related-issues.tsx
git commit -m "feat(mobile): collapsible status sections on project detail"
```

---

### Task 3: Create new tab files (projects, issues, pins)

**Files:**
- Create: `apps/mobile/app/(app)/[workspace]/(tabs)/projects.tsx`
- Create: `apps/mobile/app/(app)/[workspace]/(tabs)/issues.tsx`
- Create: `apps/mobile/app/(app)/[workspace]/(tabs)/pins.tsx`

These files are created but NOT yet wired as tabs (the old `(tabs)/_layout.tsx` still references inbox/my-issues). The app stays fully functional after this commit.

- [ ] **Step 1: Create `(tabs)/projects.tsx`**

```tsx
// apps/mobile/app/(app)/[workspace]/(tabs)/projects.tsx
/**
 * Projects tab. Moved from more/projects.tsx; header is now the in-body
 * <Header> component since tab roots have headerShown: false.
 */
import { useCallback, useMemo } from "react";
import {
  ActivityIndicator,
  FlatList,
  RefreshControl,
  View,
} from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { useQuery } from "@tanstack/react-query";
import { router } from "expo-router";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { IconButton } from "@/components/ui/icon-button";
import { Header } from "@/components/ui/header";
import { HeaderActions } from "@/components/ui/app-header-actions";
import { ProjectRow } from "@/components/project/project-row";
import { projectListOptions } from "@/data/queries/projects";
import { useWorkspaceStore } from "@/data/workspace-store";

export default function ProjectsTab() {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);

  const { data, isLoading, error, refetch, isRefetching } = useQuery(
    projectListOptions(wsId),
  );

  const sorted = useMemo(() => {
    if (!data) return [];
    return [...data].sort(
      (a, b) =>
        new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime(),
    );
  }, [data]);

  const goCreate = useCallback(() => {
    if (wsSlug) router.push(`/${wsSlug}/project/new`);
  }, [wsSlug]);

  return (
    <SafeAreaView className="flex-1 bg-background" edges={[]}>
      <Header
        title="Projects"
        right={
          <>
            <IconButton
              name="add"
              onPress={goCreate}
              accessibilityLabel="New project"
            />
            <HeaderActions />
          </>
        }
      />
      {isLoading ? (
        <View className="flex-1 items-center justify-center">
          <ActivityIndicator />
        </View>
      ) : error ? (
        <View className="px-4 gap-3 pt-4">
          <Text className="text-sm text-destructive">
            Failed to load projects:{" "}
            {error instanceof Error ? error.message : "unknown error"}
          </Text>
          <Button variant="outline" onPress={() => refetch()}>
            <Text>Retry</Text>
          </Button>
        </View>
      ) : sorted.length === 0 ? (
        <EmptyState onCreate={goCreate} />
      ) : (
        <FlatList
          data={sorted}
          keyExtractor={(item) => item.id}
          ItemSeparatorComponent={() => (
            <View className="h-px bg-border ml-4" />
          )}
          renderItem={({ item }) => (
            <ProjectRow
              project={item}
              onPress={() => {
                if (wsSlug) router.push(`/${wsSlug}/project/${item.id}`);
              }}
            />
          )}
          refreshControl={
            <RefreshControl refreshing={isRefetching} onRefresh={refetch} />
          }
          contentContainerClassName="pb-6"
        />
      )}
    </SafeAreaView>
  );
}

function EmptyState({ onCreate }: { onCreate: () => void }) {
  return (
    <View className="flex-1 items-center justify-center px-6 gap-4">
      <Text className="text-base font-medium text-foreground">
        No projects yet
      </Text>
      <Text className="text-sm text-muted-foreground text-center">
        Group related issues into a project to track progress and assign a
        lead.
      </Text>
      <Button variant="default" onPress={onCreate}>
        <Text>Create project</Text>
      </Button>
    </View>
  );
}
```

- [ ] **Step 2: Create `(tabs)/issues.tsx`**

Copy the full content of `apps/mobile/app/(app)/[workspace]/more/issues.tsx` verbatim, then make two changes:

1. Add `import { Header } from "@/components/ui/header";` to the import block
2. In `IssuesPage`'s `return`, add `<Header title="Issues" />` as the first child inside the outer `<View className="flex-1 bg-background">`:

```tsx
  return (
    <View className="flex-1 bg-background">
      <Header title="Issues" />
      <ScopeToolbar ... />
      {hasActiveFilters ? <ActiveFilterChips ... /> : null}
      {isLoading ? (...) : ...}
    </View>
  );
```

Everything else (ScopeToolbar, ActiveFilterChips, SectionList, all helper components) is unchanged.

- [ ] **Step 3: Create `(tabs)/pins.tsx`**

Copy the full content of `apps/mobile/app/(app)/[workspace]/more/pins.tsx`, then:

1. Add `import { Header } from "@/components/ui/header";` to imports
2. Replace `PinsPage`'s three separate early returns with a single-root return:

```tsx
export default function PinsTab() {
  // ...all existing hooks unchanged...

  return (
    <View className="flex-1 bg-background">
      <Header title="Pinned" />
      {isLoading ? (
        <View className="flex-1 items-center justify-center">
          <ActivityIndicator />
        </View>
      ) : error ? (
        <View className="flex-1 px-4 gap-3 pt-4">
          <Text className="text-sm text-destructive">
            Failed to load pins:{" "}
            {error instanceof Error ? error.message : "unknown error"}
          </Text>
          <Button variant="outline" onPress={() => refetch()}>
            <Text>Retry</Text>
          </Button>
        </View>
      ) : pins.length === 0 ? (
        <View className="flex-1 items-center justify-center px-6">
          <Text className="text-sm text-muted-foreground text-center">
            No pins yet. Pin an issue or project from its actions menu to
            surface it here.
          </Text>
        </View>
      ) : (
        <ScrollView
          className="flex-1"
          contentContainerClassName="pb-6"
          refreshControl={
            <RefreshControl
              refreshing={isRefetching}
              onRefresh={() => refetch()}
            />
          }
          showsVerticalScrollIndicator={false}
        >
          {pins.map((pin, idx) => (
            <View key={pin.id}>
              {idx > 0 ? <View className="h-px bg-border ml-4" /> : null}
              <PinRow pin={pin} wsId={wsId} wsSlug={wsSlug} />
            </View>
          ))}
        </ScrollView>
      )}
    </View>
  );
}
```

All helper components (`PinRow`, `IssuePinRow`, `ProjectPinRow`, `SkeletonRow`, `MissingPinRow`) are unchanged.

- [ ] **Step 4: Typecheck**

```bash
cd apps/mobile && pnpm typecheck
```
Expected: no errors in the three new tab files

- [ ] **Step 5: Commit**

```bash
git add \
  "apps/mobile/app/(app)/[workspace]/(tabs)/projects.tsx" \
  "apps/mobile/app/(app)/[workspace]/(tabs)/issues.tsx" \
  "apps/mobile/app/(app)/[workspace]/(tabs)/pins.tsx"
git commit -m "feat(mobile): add projects/issues/pins tab files (pre-wiring)"
```

---

### Task 4: Create new more/ push screens (inbox, my-issues)

**Files:**
- Create: `apps/mobile/app/(app)/[workspace]/more/inbox.tsx`
- Create: `apps/mobile/app/(app)/[workspace]/more/my-issues.tsx`

Push screens use the native Stack header. Remove the in-body `<Header>` and use `Stack.Screen` options from within the component (same pattern as the original `more/projects.tsx`). Do NOT carry `<HeaderActions />` into push screens.

- [ ] **Step 1: Create `more/inbox.tsx`**

Copy the full content of `apps/mobile/app/(app)/[workspace]/(tabs)/inbox.tsx`, then:

1. Add `Stack` to the `expo-router` import: `import { Stack, router } from "expo-router";`
2. Remove `Header` and `HeaderActions` from imports
3. Remove `import { Header } from "@/components/ui/header";` and `import { HeaderActions } from "@/components/ui/app-header-actions";`
4. Add a `useCallback` for `headerRight` (wrapping `onPressMenu`, which is already defined in the component):

```tsx
  const inboxHeaderRight = useCallback(
    () => (
      <IconButton
        name="ellipsis-horizontal"
        onPress={onPressMenu}
        accessibilityLabel="Inbox actions"
      />
    ),
    [onPressMenu],
  );
```

5. Replace `<Header title="Inbox" right={...}>` with a `Stack.Screen` as the first child of the outer `<View>`:

```tsx
  return (
    <View className="flex-1 bg-background">
      <Stack.Screen
        options={{
          headerRight: inboxHeaderRight,
        }}
      />
      {isLoading ? (
        <InboxLoading />
      ) : ...
    </View>
  );
```

The Stack.Screen sets `headerRight` only — title and `headerBackTitle` are already set by the workspace `_layout.tsx` entry added in Task 5.

- [ ] **Step 2: Create `more/my-issues.tsx`**

Copy the full content of `apps/mobile/app/(app)/[workspace]/(tabs)/my-issues.tsx`, then:

1. Remove `Header` and `HeaderActions` imports
2. Remove `<Header title="My Issues" right={<HeaderActions />} />` from the return — the Stack title comes from the layout entry added in Task 5 Step 1; no headerRight is needed for My Issues

Everything else (scope toolbar, filter chips, SectionList, all helpers) is unchanged.

- [ ] **Step 3: Typecheck**

```bash
cd apps/mobile && pnpm typecheck
```
Expected: no errors in the two new files

- [ ] **Step 4: Commit**

```bash
git add \
  "apps/mobile/app/(app)/[workspace]/more/inbox.tsx" \
  "apps/mobile/app/(app)/[workspace]/more/my-issues.tsx"
git commit -m "feat(mobile): add inbox/my-issues as more/ push screen files (pre-wiring)"
```

---

### Task 5: Wire routing and remove old files (atomic switch)

**Files:**
- Modify: `apps/mobile/app/(app)/[workspace]/_layout.tsx`
- Modify: `apps/mobile/app/(app)/[workspace]/(tabs)/_layout.tsx`
- Modify: `apps/mobile/components/nav/more-tab-dropdown.tsx`
- Delete: `apps/mobile/app/(app)/[workspace]/more/projects.tsx`
- Delete: `apps/mobile/app/(app)/[workspace]/more/issues.tsx`
- Delete: `apps/mobile/app/(app)/[workspace]/more/pins.tsx`
- Delete: `apps/mobile/app/(app)/[workspace]/(tabs)/inbox.tsx`
- Delete: `apps/mobile/app/(app)/[workspace]/(tabs)/my-issues.tsx`

All changes in this task should be made before typechecking — intermediate states will have broken references.

- [ ] **Step 1: Update workspace `_layout.tsx` Stack route declarations**

Around lines 292–307, replace the four `more/*` Stack.Screen entries:

```tsx
        <Stack.Screen
          name="more/issues"
          options={{ title: "Issues", headerBackTitle: "Back" }}
        />
        <Stack.Screen
          name="more/projects"
          options={{ title: "Projects", headerBackTitle: "Back" }}
        />
        <Stack.Screen
          name="more/agents"
          options={{ title: "Agents", headerBackTitle: "Back" }}
        />
        <Stack.Screen
          name="more/pins"
          options={{ title: "Pinned", headerBackTitle: "Back" }}
        />
```

With:

```tsx
        <Stack.Screen
          name="more/agents"
          options={{ title: "Agents", headerBackTitle: "Back" }}
        />
        <Stack.Screen
          name="more/inbox"
          options={{ title: "Inbox", headerBackTitle: "Back" }}
        />
        <Stack.Screen
          name="more/my-issues"
          options={{ title: "My Issues", headerBackTitle: "Back" }}
        />
```

- [ ] **Step 2: Update `(tabs)/_layout.tsx`**

Replace the four `Tabs.Screen` entries (inbox, my-issues, chat, more) with five (projects, issues, pins, chat, more). The `inboxBadge` logic is unchanged — it now shows on the More tab instead of Inbox.

Also update the More tab's anchor width: with 5 tabs each tab is `20%` of screen width, so update the `MoreTabDropdownAnchor` positioning from `width: "25%"` to `width: "20%"`. **This change is in `more-tab-dropdown.tsx` (Step 3), not here.**

Replace the full `<Tabs>` block inside the `return`:

```tsx
      <Tabs
        screenOptions={{
          headerShown: false,
          tabBarActiveTintColor: t.foreground,
          tabBarInactiveTintColor: t.mutedForeground,
          tabBarStyle: { backgroundColor: t.background },
          tabBarLabelStyle: { fontSize: 11 },
        }}
      >
        <Tabs.Screen
          name="projects"
          options={{
            title: "Projects",
            tabBarIcon: ({ color, size, focused }) => (
              <Image
                source={focused ? "sf:square.stack.fill" : "sf:square.stack"}
                tintColor={color}
                style={{ width: size, height: size }}
              />
            ),
          }}
        />
        <Tabs.Screen
          name="issues"
          options={{
            title: "Issues",
            tabBarIcon: ({ color, size }) => (
              <Image
                source="sf:list.bullet"
                tintColor={color}
                style={{ width: size, height: size }}
              />
            ),
          }}
        />
        <Tabs.Screen
          name="pins"
          options={{
            title: "Pins",
            tabBarIcon: ({ color, size, focused }) => (
              <Image
                source={focused ? "sf:pin.fill" : "sf:pin"}
                tintColor={color}
                style={{ width: size, height: size }}
              />
            ),
          }}
        />
        <Tabs.Screen
          name="chat"
          options={{
            title: "Chat",
            tabBarBadge: chatBadge,
            tabBarBadgeStyle: BADGE_STYLE,
            tabBarIcon: ({ color, size, focused }) => (
              <Image
                source={focused ? "sf:bubble.left.fill" : "sf:bubble.left"}
                tintColor={color}
                style={{ width: size, height: size }}
              />
            ),
          }}
        />
        <Tabs.Screen
          name="more"
          options={{
            title: "More",
            tabBarBadge: inboxBadge,
            tabBarBadgeStyle: BADGE_STYLE,
            tabBarIcon: ({ color, size }) => (
              <Image
                source="sf:ellipsis"
                tintColor={color}
                style={{ width: size, height: size }}
              />
            ),
          }}
          listeners={() => ({
            tabPress: (e) => {
              e.preventDefault();
              moreTriggerRef.current?.open();
            },
          })}
        />
      </Tabs>
```

- [ ] **Step 3: Update `more-tab-dropdown.tsx`**

Two changes:

**a) Update `NAV_ITEMS`** — replace:

```tsx
const NAV_ITEMS: NavItem[] = [
  { label: "Pinned", icon: "pin", path: "/more/pins" },
  { label: "Issues", icon: "list.bullet", path: "/more/issues" },
  { label: "Projects", icon: "square.stack", path: "/more/projects" },
];
```

With:

```tsx
const NAV_ITEMS: NavItem[] = [
  { label: "Inbox", icon: "tray", path: "/more/inbox" },
  { label: "My Issues", icon: "checklist", path: "/more/my-issues" },
];
```

**b) Update the anchor width** — with 5 tabs the More tab occupies the rightmost 20% (not 25%). In the `style` prop of the anchor `<View>`:

```tsx
      style={{
        position: "absolute",
        right: 0,
        bottom: insets.bottom,
        width: "20%",   // was "25%" with 4 tabs; 5 tabs = 20% each
        height: TAB_BAR_HEIGHT,
      }}
```

- [ ] **Step 4: Delete old files**

```bash
git rm "apps/mobile/app/(app)/[workspace]/more/projects.tsx"
git rm "apps/mobile/app/(app)/[workspace]/more/issues.tsx"
git rm "apps/mobile/app/(app)/[workspace]/more/pins.tsx"
git rm "apps/mobile/app/(app)/[workspace]/(tabs)/inbox.tsx"
git rm "apps/mobile/app/(app)/[workspace]/(tabs)/my-issues.tsx"
```

- [ ] **Step 5: Typecheck**

```bash
cd apps/mobile && pnpm typecheck
```
Expected: no errors

- [ ] **Step 6: Commit**

```bash
git add \
  "apps/mobile/app/(app)/[workspace]/_layout.tsx" \
  "apps/mobile/app/(app)/[workspace]/(tabs)/_layout.tsx" \
  "apps/mobile/components/nav/more-tab-dropdown.tsx"
git commit -m "feat(mobile): complete nav rearrangement — wire routing, remove old routes"
```
