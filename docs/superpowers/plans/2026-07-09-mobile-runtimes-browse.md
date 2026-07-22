# Mobile Runtimes Browse Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a read-only Runtimes browse feature (list → detail) to the
mobile More page, mirroring desktop's Runtimes section's status/ownership/
health/CLI-update-signal display without any of its connect/delete/
profile-config/usage-chart surface.

**Architecture:** Two new pushed Stack routes under `more/runtimes` (list,
detail). No new backend endpoint and no new detail query — mobile already
fetches the full runtime list (`runtimeListOptions`, built earlier for the
presence-dot system) and, like desktop itself, the detail screen finds a
runtime by id in that already-fetched list rather than hitting a dedicated
endpoint. Two small mobile-local files mirror presentational/comparison
logic that lives in `packages/views` or is module-private in
`packages/core` (out of mobile's import whitelist either way); the pure
`deriveRuntimeHealth`/`readRuntimeCliVersion` functions themselves ARE
imported directly from `@multica/core/runtimes` since they're actually
exported and pure.

**Tech Stack:** Expo Router (pushed Stack screens), TanStack Query, Zod
(`parseWithFallback` — already wired for `RuntimeDevice` from an earlier
feature), i18next/react-i18next, NativeWind.

## Global Constraints

- Round 2 of 4 desktop-feature-migration rounds — round 1 (Skills) is
  already shipped; rounds 3-4 (Usage, Agents) are separate future plans.
  Do not add anything from those domains here.
- Read-only: no connect-new-runtime, no delete, no profile/pricing
  configuration, no update-triggering action (CLI-update is a static badge
  only, never a button, never polled).
- No per-runtime usage/cost charts — that's round 3's territory.
- No tap-through from the "agents on this runtime" list to an agent detail
  screen — round 4 (Agents) hasn't shipped, so this list is static
  name/avatar rows, not navigable.
- Mobile's import whitelist from `packages/` is `@multica/core` only
  (types + pure functions). `packages/views/runtimes/components/shared.tsx`'s
  presentational health mapping does NOT qualify (lives in `packages/views`)
  — mirror it into a mobile-local file. `packages/core/runtimes/hooks.ts`'s
  `isNewer`/`runtimeNeedsUpdate` are module-private (not exported even
  though the file is in `packages/core`) — mirror those into a mobile-local
  file too. `deriveRuntimeHealth`, `RuntimeHealth`, and
  `readRuntimeCliVersion` ARE exported pure functions/types from
  `@multica/core/runtimes` — import them directly, don't mirror them.
- Every new read-side `api.ts` method (none needed this round — see Task 1)
  would take `opts?: { signal?: AbortSignal }`; this round adds no new
  `api.ts` methods, only a `queryOptions` that calls GitHub's public API
  directly (matching desktop's own `latestCliVersionOptions`), so this
  constraint doesn't add new surface but is noted for consistency.
- Chinese copy: "runtime" is a fully-translated concept (not one of the
  `issue`/`skill`/`task` mixed-rule trio) — desktop's own
  `packages/views/locales/zh-Hans/layout.json` sidebar entry uses "运行时"
  for the nav label; every zh-Hans string in this round's new namespace
  uses "运行时" throughout, and health-label wording exactly mirrors
  desktop's own `packages/views/locales/zh-Hans/runtimes.json`'s
  `health.*.label` values (在线 / 最近失联 / 离线 / 即将清理) rather than
  inventing new phrasing.
- Every new source file must be `git ls-files`-tracked before its task's
  commit (apps/mobile/CLAUDE.md lesson #2).

---

### Task 1: Data layer — CLI-version query, health/update-check mirrors, i18n namespace

**Files:**
- Modify: `apps/mobile/data/queries/runtimes.ts`
- Create: `apps/mobile/lib/runtime-health.ts`
- Create: `apps/mobile/lib/runtime-update-check.ts`
- Create: `apps/mobile/locales/en/runtimes.json`
- Create: `apps/mobile/locales/zh-Hans/runtimes.json`
- Modify: `apps/mobile/locales/index.ts`

**Interfaces:**
- Consumes: `RuntimeDevice` type (`@multica/core/types`, already used by
  the existing `runtimeListOptions`), `deriveRuntimeHealth` / `RuntimeHealth`
  / `readRuntimeCliVersion` (`@multica/core/runtimes`, all exported pure
  functions/types).
- Produces: `latestCliVersionOptions()` (in `data/queries/runtimes.ts`),
  `HEALTH_DOT_CLASS: Record<RuntimeHealth, string>` (in `lib/runtime-health.ts`),
  `runtimeNeedsUpdate(runtime, latestVersion, userId): boolean` (in
  `lib/runtime-update-check.ts`) — Tasks 2-3 import all three.

- [ ] **Step 1: Add `latestCliVersionOptions` to the existing runtimes query file**

`apps/mobile/data/queries/runtimes.ts` currently contains only
`runtimeListOptions`. Leave that function exactly as-is (it's load-bearing
for the presence-dot system — don't touch its key shape) and append:

```ts
export const runtimeKeys = {
  latestVersion: () => ["runtimes", "latestVersion"] as const,
};

const GITHUB_RELEASES_URL =
  "https://api.github.com/repos/multica-ai/multica/releases/latest";

// Mirrors packages/core/runtimes/queries.ts's latestCliVersionOptions —
// same GitHub public-API call, same silent-null-on-failure behavior (a
// flaky network on a phone should never surface as a query error for
// something as inconsequential as an update-available badge).
export const latestCliVersionOptions = () =>
  queryOptions({
    queryKey: runtimeKeys.latestVersion(),
    queryFn: async (): Promise<string | null> => {
      try {
        const resp = await fetch(GITHUB_RELEASES_URL, {
          headers: { Accept: "application/vnd.github+json" },
        });
        if (!resp.ok) return null;
        const data = await resp.json();
        return (data.tag_name as string) ?? null;
      } catch {
        return null;
      }
    },
    staleTime: 10 * 60 * 1000, // 10 minutes
  });
```

The full file after this change:

```ts
import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

// Runtime list — workspace-scoped. Feeds the availability dimension of the
// presence dot via @multica/core/agents/derive-presence (status + last_seen_at).
// Invalidated by daemon:register / sweeper-driven status changes; see
// data/realtime/use-presence-realtime.ts.
export const runtimeListOptions = (wsId: string | null) =>
  queryOptions({
    queryKey: ["runtimes", wsId] as const,
    queryFn: ({ signal }) => api.listRuntimes({ signal }),
    enabled: !!wsId,
  });

export const runtimeKeys = {
  latestVersion: () => ["runtimes", "latestVersion"] as const,
};

const GITHUB_RELEASES_URL =
  "https://api.github.com/repos/multica-ai/multica/releases/latest";

// Mirrors packages/core/runtimes/queries.ts's latestCliVersionOptions —
// same GitHub public-API call, same silent-null-on-failure behavior (a
// flaky network on a phone should never surface as a query error for
// something as inconsequential as an update-available badge).
export const latestCliVersionOptions = () =>
  queryOptions({
    queryKey: runtimeKeys.latestVersion(),
    queryFn: async (): Promise<string | null> => {
      try {
        const resp = await fetch(GITHUB_RELEASES_URL, {
          headers: { Accept: "application/vnd.github+json" },
        });
        if (!resp.ok) return null;
        const data = await resp.json();
        return (data.tag_name as string) ?? null;
      } catch {
        return null;
      }
    },
    staleTime: 10 * 60 * 1000, // 10 minutes
  });
```

- [ ] **Step 2: Mirror the health-color mapping**

Create `apps/mobile/lib/runtime-health.ts`:

```ts
/**
 * Mobile-local mirror of packages/views/runtimes/components/shared.tsx's
 * HEALTH_VISUAL dot-color mapping — mirrored, not imported, since that
 * file lives in packages/views (out of mobile's @multica/core-only
 * whitelist). Labels come from runtimes.json via i18n
 * (t(`health.${health}.label`)) instead of a hardcoded English fallback.
 */
import type { RuntimeHealth } from "@multica/core/runtimes";

export const HEALTH_DOT_CLASS: Record<RuntimeHealth, string> = {
  online: "bg-success",
  recently_lost: "bg-warning",
  offline: "bg-muted-foreground/40",
  about_to_gc: "bg-destructive",
};
```

- [ ] **Step 3: Mirror the CLI-update-needed comparison**

Create `apps/mobile/lib/runtime-update-check.ts`:

```ts
/**
 * Mobile-local mirror of packages/core/runtimes/hooks.ts's isNewer /
 * runtimeNeedsUpdate. Those are module-private (not exported from
 * @multica/core/runtimes) — the exported hooks built on them
 * (useMyRuntimesNeedUpdate, useUpdatableRuntimeIds) internally call
 * packages/core's OWN runtimeListOptions/latestCliVersionOptions, binding
 * to a different QueryClient/key-factory instance than mobile owns. Same
 * hazard apps/mobile/CLAUDE.md's "Mobile-owned updaters" section documents
 * for realtime WS updaters — mirror the comparison logic instead of trying
 * to reuse the hooks. readRuntimeCliVersion IS imported below since it's
 * actually exported and purely reads a field off `metadata`.
 */
import type { RuntimeDevice } from "@multica/core/types";
import { readRuntimeCliVersion } from "@multica/core/runtimes";

function stripV(v: string): string {
  return v.replace(/^v/, "");
}

function isNewer(latest: string, current: string): boolean {
  const l = stripV(latest).split(".").map(Number);
  const c = stripV(current).split(".").map(Number);
  for (let i = 0; i < Math.max(l.length, c.length); i++) {
    const lv = l[i] ?? 0;
    const cv = c[i] ?? 0;
    if (lv > cv) return true;
    if (lv < cv) return false;
  }
  return false;
}

/**
 * Whether to show a static "update available" badge for this runtime.
 * Mirrors desktop's exact gating (packages/core/runtimes/hooks.ts's
 * runtimeNeedsUpdate): local runtimes only, only for the signed-in owner,
 * never for desktop-launched runtimes (Desktop has its own auto-updater).
 */
export function runtimeNeedsUpdate(
  runtime: RuntimeDevice,
  latestVersion: string | null | undefined,
  userId: string | null | undefined,
): boolean {
  if (!latestVersion || !userId) return false;
  if (runtime.runtime_mode !== "local") return false;
  if (runtime.owner_id !== userId) return false;
  if (runtime.metadata && runtime.metadata.launched_by === "desktop") {
    return false;
  }
  const cliVersion = readRuntimeCliVersion(runtime.metadata);
  if (!cliVersion) return false;
  return isNewer(latestVersion, cliVersion);
}
```

- [ ] **Step 4: Create the `runtimes.json` locale namespace (en)**

Create `apps/mobile/locales/en/runtimes.json`:

```json
{
  "list": {
    "header_title": "Runtimes",
    "error": {
      "load_prefix": "Failed to load runtimes:",
      "unknown": "unknown error",
      "retry": "Retry"
    },
    "empty": {
      "title": "No runtimes yet",
      "message": "Runtimes connected on desktop will show up here."
    }
  },
  "health": {
    "online": { "label": "Online" },
    "recently_lost": { "label": "Recently lost" },
    "offline": { "label": "Offline" },
    "about_to_gc": { "label": "About to GC" }
  },
  "detail": {
    "header_default_title": "Runtime",
    "error": {
      "load_prefix": "Failed to load runtime:",
      "unknown": "unknown error",
      "retry": "Retry"
    },
    "owner_label": "Owner",
    "mode_label": "Mode",
    "mode": {
      "local": "Local",
      "cloud": "Cloud"
    },
    "visibility_label": "Visibility",
    "visibility": {
      "private": "Private",
      "public": "Public"
    },
    "device_label": "Device",
    "created_label": "Created",
    "updated_label": "Updated",
    "cli_version_label": "CLI Version",
    "update_available": "Update available ({{version}})",
    "agents_title": "Agents on this runtime",
    "agents_empty": "No agents configured on this runtime yet"
  }
}
```

- [ ] **Step 5: Create the `runtimes.json` locale namespace (zh-Hans)**

Create `apps/mobile/locales/zh-Hans/runtimes.json`. "运行时" throughout —
this matches desktop's own precedent for the entity name (confirmed in
`packages/views/locales/zh-Hans/layout.json`), and the four health labels
exactly mirror desktop's `packages/views/locales/zh-Hans/runtimes.json`'s
`health.*.label` values:

```json
{
  "list": {
    "header_title": "运行时",
    "error": {
      "load_prefix": "加载运行时失败：",
      "unknown": "未知错误",
      "retry": "重试"
    },
    "empty": {
      "title": "还没有运行时",
      "message": "在桌面端连接的运行时会显示在这里。"
    }
  },
  "health": {
    "online": { "label": "在线" },
    "recently_lost": { "label": "最近失联" },
    "offline": { "label": "离线" },
    "about_to_gc": { "label": "即将清理" }
  },
  "detail": {
    "header_default_title": "运行时",
    "error": {
      "load_prefix": "加载运行时失败：",
      "unknown": "未知错误",
      "retry": "重试"
    },
    "owner_label": "所有者",
    "mode_label": "模式",
    "mode": {
      "local": "本地",
      "cloud": "云端"
    },
    "visibility_label": "可见性",
    "visibility": {
      "private": "私有",
      "public": "公开"
    },
    "device_label": "设备",
    "created_label": "创建时间",
    "updated_label": "更新时间",
    "cli_version_label": "CLI 版本",
    "update_available": "有新版本可用（{{version}}）",
    "agents_title": "运行在此运行时上的智能体",
    "agents_empty": "还没有智能体配置在此运行时上"
  }
}
```

- [ ] **Step 6: Register the namespace in the locale registry**

Replace the entire contents of `apps/mobile/locales/index.ts` with (this
adds the `enRuntimes`/`zhHansRuntimes` imports in alphabetical order,
after `Projects` and before `Settings`, and registers `runtimes` in both
`RESOURCES` entries):

```ts
import enAuth from "./en/auth.json";
import enChat from "./en/chat.json";
import enCommon from "./en/common.json";
import enInbox from "./en/inbox.json";
import enIssues from "./en/issues.json";
import enProjects from "./en/projects.json";
import enRuntimes from "./en/runtimes.json";
import enSettings from "./en/settings.json";
import enSkills from "./en/skills.json";
import enWorkspace from "./en/workspace.json";
import zhHansAuth from "./zh-Hans/auth.json";
import zhHansChat from "./zh-Hans/chat.json";
import zhHansCommon from "./zh-Hans/common.json";
import zhHansInbox from "./zh-Hans/inbox.json";
import zhHansIssues from "./zh-Hans/issues.json";
import zhHansProjects from "./zh-Hans/projects.json";
import zhHansRuntimes from "./zh-Hans/runtimes.json";
import zhHansSettings from "./zh-Hans/settings.json";
import zhHansSkills from "./zh-Hans/skills.json";
import zhHansWorkspace from "./zh-Hans/workspace.json";

export type SupportedLocale = "en" | "zh-Hans";

export const RESOURCES = {
  en: {
    auth: enAuth,
    chat: enChat,
    common: enCommon,
    inbox: enInbox,
    issues: enIssues,
    projects: enProjects,
    runtimes: enRuntimes,
    settings: enSettings,
    skills: enSkills,
    workspace: enWorkspace,
  },
  "zh-Hans": {
    auth: zhHansAuth,
    chat: zhHansChat,
    common: zhHansCommon,
    inbox: zhHansInbox,
    issues: zhHansIssues,
    projects: zhHansProjects,
    runtimes: zhHansRuntimes,
    settings: zhHansSettings,
    skills: zhHansSkills,
    workspace: zhHansWorkspace,
  },
} satisfies Record<SupportedLocale, Record<string, Record<string, unknown>>>;
```

- [ ] **Step 7: Verify**

```bash
cd apps/mobile
pnpm exec tsc --noEmit -p .
pnpm exec eslint data/queries/runtimes.ts lib/runtime-health.ts lib/runtime-update-check.ts locales/index.ts
pnpm test -- parity
```

Expected: all three commands exit 0.

- [ ] **Step 8: Commit**

```bash
git add apps/mobile/data/queries/runtimes.ts apps/mobile/lib/runtime-health.ts apps/mobile/lib/runtime-update-check.ts apps/mobile/locales/index.ts apps/mobile/locales/en/runtimes.json apps/mobile/locales/zh-Hans/runtimes.json
git status --short   # confirm both new locale files and both new lib files show as tracked, not ignored
git commit -m "feat(mobile): add Runtimes data layer and i18n namespace"
```

---

### Task 2: List screen + More-page nav entry

**Files:**
- Create: `apps/mobile/components/runtime/runtime-row.tsx`
- Create: `apps/mobile/app/(app)/[workspace]/more/runtimes.tsx`
- Modify: `apps/mobile/app/(app)/[workspace]/_layout.tsx`
- Modify: `apps/mobile/app/(app)/[workspace]/(tabs)/more.tsx`
- Modify: `apps/mobile/locales/en/workspace.json`
- Modify: `apps/mobile/locales/zh-Hans/workspace.json`

**Interfaces:**
- Consumes: `runtimeListOptions(wsId)` (pre-existing,
  `apps/mobile/data/queries/runtimes.ts`), `HEALTH_DOT_CLASS` (Task 1,
  `@/lib/runtime-health`), `deriveRuntimeHealth` (`@multica/core/runtimes`).
- Produces: none new for later tasks — Task 3 (detail screen) reuses
  `runtimeListOptions` directly, not anything from `RuntimeRow`.

- [ ] **Step 1: Build the list row component**

Create `apps/mobile/components/runtime/runtime-row.tsx`, mirroring the
visual shape of `apps/mobile/components/skill/skill-row.tsx` (icon + flex
title/status column + right-side visibility/time column):

```tsx
import { Pressable, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { useTranslation } from "react-i18next";
import type { RuntimeDevice } from "@multica/core/types";
import { deriveRuntimeHealth } from "@multica/core/runtimes";
import { Text } from "@/components/ui/text";
import { HEALTH_DOT_CLASS } from "@/lib/runtime-health";
import { timeAgo } from "@/lib/time-ago";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";

interface Props {
  runtime: RuntimeDevice;
  onPress: () => void;
}

export function RuntimeRow({ runtime, onPress }: Props) {
  const { t } = useTranslation("runtimes");
  const { colorScheme } = useColorScheme();
  const mutedFg = THEME[colorScheme].mutedForeground;
  const health = deriveRuntimeHealth(runtime, Date.now());

  return (
    <Pressable onPress={onPress} className="active:bg-secondary px-4 py-3">
      <View className="flex-row items-start gap-3">
        <Ionicons
          name={
            runtime.runtime_mode === "cloud"
              ? "cloud-outline"
              : "desktop-outline"
          }
          size={20}
          color={mutedFg}
          style={{ marginTop: 2 }}
        />
        <View className="flex-1 gap-1">
          <Text
            className="text-base text-foreground font-medium"
            numberOfLines={1}
          >
            {runtime.name}
          </Text>
          <View className="flex-row items-center gap-1.5">
            <View
              className={`size-2 rounded-full ${HEALTH_DOT_CLASS[health]}`}
            />
            <Text className="text-xs text-muted-foreground">
              {t(`health.${health}.label`)}
            </Text>
          </View>
        </View>
        <View className="items-end gap-1">
          <Ionicons
            name={
              runtime.visibility === "public"
                ? "globe-outline"
                : "lock-closed-outline"
            }
            size={14}
            color={mutedFg}
          />
          <Text className="text-[11px] text-muted-foreground/70">
            {timeAgo(runtime.last_seen_at ?? runtime.updated_at)}
          </Text>
        </View>
      </View>
    </Pressable>
  );
}
```

- [ ] **Step 2: Build the list screen**

Create `apps/mobile/app/(app)/[workspace]/more/runtimes.tsx`, mirroring
`apps/mobile/app/(app)/[workspace]/more/skills.tsx`'s structure (flat
`FlatList` + `RefreshControl`, native Stack header, no in-body header, no
create button since this is read-only). Sort: online first, then
`last_seen_at` desc:

```tsx
/**
 * Runtimes browse page. Flat FlatList over the workspace's runtimes,
 * read-only (no connect/delete/profile config — see
 * docs/superpowers/specs/2026-07-09-mobile-runtimes-browse-design.md).
 * Sort: online first, then last_seen_at desc (surfaces live runtimes at
 * the top).
 */
import { useMemo } from "react";
import {
  ActivityIndicator,
  FlatList,
  RefreshControl,
  View,
} from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { useQuery } from "@tanstack/react-query";
import { router } from "expo-router";
import { useTranslation } from "react-i18next";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { RuntimeRow } from "@/components/runtime/runtime-row";
import { runtimeListOptions } from "@/data/queries/runtimes";
import { useWorkspaceStore } from "@/data/workspace-store";

export default function RuntimesPage() {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const { t } = useTranslation("runtimes");

  const { data, isLoading, error, refetch, isRefetching } = useQuery(
    runtimeListOptions(wsId),
  );

  const sorted = useMemo(() => {
    if (!data) return [];
    return [...data].sort((a, b) => {
      if (a.status !== b.status) return a.status === "online" ? -1 : 1;
      const aTime = a.last_seen_at ? new Date(a.last_seen_at).getTime() : 0;
      const bTime = b.last_seen_at ? new Date(b.last_seen_at).getTime() : 0;
      return bTime - aTime;
    });
  }, [data]);

  return (
    <SafeAreaView className="flex-1 bg-background" edges={[]}>
      {isLoading ? (
        <View className="flex-1 items-center justify-center">
          <ActivityIndicator />
        </View>
      ) : error ? (
        <View className="px-4 gap-3 pt-4">
          <Text className="text-sm text-destructive">
            {t("list.error.load_prefix")}{" "}
            {error instanceof Error ? error.message : t("list.error.unknown")}
          </Text>
          <Button variant="outline" onPress={() => refetch()}>
            <Text>{t("list.error.retry")}</Text>
          </Button>
        </View>
      ) : sorted.length === 0 ? (
        <View className="flex-1 items-center justify-center px-6 gap-2">
          <Text className="text-base font-medium text-foreground">
            {t("list.empty.title")}
          </Text>
          <Text className="text-sm text-muted-foreground text-center">
            {t("list.empty.message")}
          </Text>
        </View>
      ) : (
        <FlatList
          data={sorted}
          keyExtractor={(item) => item.id}
          ItemSeparatorComponent={() => (
            <View className="h-px bg-border ml-4" />
          )}
          renderItem={({ item }) => (
            <RuntimeRow
              runtime={item}
              onPress={() => {
                if (wsSlug) router.push(`/${wsSlug}/more/runtimes/${item.id}`);
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
```

- [ ] **Step 3: Register the list route's header**

In `apps/mobile/app/(app)/[workspace]/_layout.tsx`, add the `runtimes`
namespace hook next to the others. Find:

```ts
  const { t: tSkills } = useTranslation("skills");
```

Replace with:

```ts
  const { t: tRuntimes } = useTranslation("runtimes");
  const { t: tSkills } = useTranslation("skills");
```

Then find this exact block (the last `more/skills*` registration):

```tsx
        <Stack.Screen
          name="more/skills/[id]/file/[...path]"
          options={{
            title: tSkills("file.header_default_title"),
            headerBackTitle: tSkills("detail.header_default_title"),
          }}
        />
```

Add directly after it:

```tsx
        <Stack.Screen
          name="more/runtimes"
          options={{
            title: tRuntimes("list.header_title"),
            headerBackTitle: tCommon("nav.back"),
          }}
        />
```

- [ ] **Step 4: Add the More-page nav row**

In `apps/mobile/locales/en/workspace.json`, find the `more_page.nav`
object (it currently has `pinned`/`issues`/`projects`/`skills`) and add
`runtimes` after `skills`:

```json
"nav": {
  "pinned": "Pinned",
  "issues": "Issues",
  "projects": "Projects",
  "skills": "Skills",
  "runtimes": "Runtimes"
}
```

In `apps/mobile/locales/zh-Hans/workspace.json`, same location — "运行时"
per the Global Constraints entity-name rule:

```json
"nav": {
  "pinned": "置顶",
  "issues": "issue",
  "projects": "项目",
  "skills": "skill",
  "runtimes": "运行时"
}
```

(Use the actual existing `pinned`/`issues`/`projects`/`skills` values
already in each file — only add the new `runtimes` key, don't change the
others.)

In `apps/mobile/app/(app)/[workspace]/(tabs)/more.tsx`, find the
`SectionGroup`'s last `NavRow` (find this exact block):

```tsx
          <NavRow
            onPress={() => slug && router.push(`/${slug}/more/skills`)}
            chevronColor={mutedFg}
            title={t("more_page.nav.skills")}
          />
        </SectionGroup>
```

Add a fifth `NavRow` after Skills:

```tsx
          <NavRow
            onPress={() => slug && router.push(`/${slug}/more/skills`)}
            chevronColor={mutedFg}
            title={t("more_page.nav.skills")}
          />
          <NavRow
            onPress={() => slug && router.push(`/${slug}/more/runtimes`)}
            chevronColor={mutedFg}
            title={t("more_page.nav.runtimes")}
          />
        </SectionGroup>
```

- [ ] **Step 5: Verify**

```bash
cd apps/mobile
pnpm exec tsc --noEmit -p .
pnpm exec eslint components/runtime/runtime-row.tsx "app/(app)/[workspace]/more/runtimes.tsx" "app/(app)/[workspace]/_layout.tsx" "app/(app)/[workspace]/(tabs)/more.tsx"
pnpm test -- parity
```

Expected: all commands exit 0.

- [ ] **Step 6: Commit**

```bash
git add apps/mobile/components/runtime/runtime-row.tsx "apps/mobile/app/(app)/[workspace]/more/runtimes.tsx" "apps/mobile/app/(app)/[workspace]/_layout.tsx" "apps/mobile/app/(app)/[workspace]/(tabs)/more.tsx" apps/mobile/locales/en/workspace.json apps/mobile/locales/zh-Hans/workspace.json
git status --short   # confirm components/runtime/ and the new runtimes.tsx route are tracked, not ignored
git commit -m "feat(mobile): add Runtimes list screen and More-page nav entry"
```

---

### Task 3: Runtime detail screen

**Files:**
- Create: `apps/mobile/app/(app)/[workspace]/more/runtimes/[id].tsx`
- Modify: `apps/mobile/app/(app)/[workspace]/_layout.tsx`

**Interfaces:**
- Consumes: `runtimeListOptions(wsId)` + `latestCliVersionOptions()` (Task 1,
  `@/data/queries/runtimes`), `agentListOptions(wsId)` (pre-existing,
  `@/data/queries/agents`), `HEALTH_DOT_CLASS` (Task 1,
  `@/lib/runtime-health`), `runtimeNeedsUpdate` (Task 1,
  `@/lib/runtime-update-check`), `deriveRuntimeHealth` /
  `readRuntimeCliVersion` (`@multica/core/runtimes`), `ActorAvatar`
  (`@/components/ui/actor-avatar`), `useActorLookup`
  (`@/data/use-actor-name`), `SectionGroup` (`@/components/ui/section-group`
  — `NavRow` is NOT needed this task, every row here is a static
  non-interactive info row).
- Produces: nothing consumed by a later task in this plan.

- [ ] **Step 1: Build the detail screen**

Create `apps/mobile/app/(app)/[workspace]/more/runtimes/[id].tsx`:

```tsx
/**
 * Runtime detail — read-only. Metadata, health, a static CLI-update
 * signal (no action, no polling), and the agents configured to use this
 * runtime. No separate detail endpoint — found by id in the already-
 * fetched runtimeListOptions list, matching desktop's own approach. No
 * create/edit/delete/connect on mobile — see
 * docs/superpowers/specs/2026-07-09-mobile-runtimes-browse-design.md.
 */
import { useMemo } from "react";
import { ActivityIndicator, ScrollView, View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import { Stack, useLocalSearchParams } from "expo-router";
import { useTranslation } from "react-i18next";
import { Ionicons } from "@expo/vector-icons";
import {
  deriveRuntimeHealth,
  readRuntimeCliVersion,
} from "@multica/core/runtimes";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { SectionGroup } from "@/components/ui/section-group";
import {
  latestCliVersionOptions,
  runtimeListOptions,
} from "@/data/queries/runtimes";
import { agentListOptions } from "@/data/queries/agents";
import { useActorLookup } from "@/data/use-actor-name";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { timeAgo } from "@/lib/time-ago";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";
import { HEALTH_DOT_CLASS } from "@/lib/runtime-health";
import { runtimeNeedsUpdate } from "@/lib/runtime-update-check";

export default function RuntimeDetailPage() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { t } = useTranslation("runtimes");
  const { colorScheme } = useColorScheme();
  const mutedFg = THEME[colorScheme].mutedForeground;
  const { getName } = useActorLookup();
  const currentUserId = useAuthStore((s) => s.user?.id);

  const {
    data: runtimes,
    isLoading,
    error,
    refetch,
  } = useQuery(runtimeListOptions(wsId));
  const { data: agents } = useQuery(agentListOptions(wsId));
  const { data: latestVersion } = useQuery(latestCliVersionOptions());

  const runtime = runtimes?.find((r) => r.id === id);
  const notFound = !isLoading && !runtime;

  const runtimeAgents = useMemo(() => {
    if (!agents || !runtime) return [];
    return agents.filter((a) => a.runtime_id === runtime.id);
  }, [agents, runtime]);

  const cliVersion = runtime ? readRuntimeCliVersion(runtime.metadata) : "";
  const needsUpdate = runtime
    ? runtimeNeedsUpdate(runtime, latestVersion, currentUserId)
    : false;
  const health = runtime ? deriveRuntimeHealth(runtime, Date.now()) : null;

  return (
    <View className="flex-1 bg-background">
      <Stack.Screen
        options={{ title: runtime?.name || t("detail.header_default_title") }}
      />
      {isLoading ? (
        <View className="flex-1 items-center justify-center">
          <ActivityIndicator />
        </View>
      ) : error || notFound ? (
        <View className="px-4 gap-3 pt-4">
          <Text className="text-sm text-destructive">
            {t("detail.error.load_prefix")}{" "}
            {error instanceof Error ? error.message : t("detail.error.unknown")}
          </Text>
          <Button variant="outline" onPress={() => refetch()}>
            <Text>{t("detail.error.retry")}</Text>
          </Button>
        </View>
      ) : (
        <ScrollView contentContainerClassName="px-4 py-4 gap-6">
          <View className="gap-2">
            <Text className="text-xl font-semibold text-foreground">
              {runtime!.name}
            </Text>
            <View className="flex-row items-center gap-1.5">
              <View
                className={`size-2 rounded-full ${HEALTH_DOT_CLASS[health!]}`}
              />
              <Text className="text-sm text-muted-foreground">
                {t(`health.${health}.label`)}
              </Text>
            </View>
          </View>

          <SectionGroup>
            <View className="flex-row items-center justify-between px-4 py-3">
              <Text className="text-sm text-muted-foreground">
                {t("detail.owner_label")}
              </Text>
              <Text className="text-sm text-foreground">
                {runtime!.owner_id ? getName("member", runtime!.owner_id) : "—"}
              </Text>
            </View>
            <View className="flex-row items-center justify-between px-4 py-3">
              <Text className="text-sm text-muted-foreground">
                {t("detail.mode_label")}
              </Text>
              <Text className="text-sm text-foreground">
                {t(`detail.mode.${runtime!.runtime_mode}`)}
              </Text>
            </View>
            <View className="flex-row items-center justify-between px-4 py-3">
              <Text className="text-sm text-muted-foreground">
                {t("detail.visibility_label")}
              </Text>
              <View className="flex-row items-center gap-1.5">
                <Ionicons
                  name={
                    runtime!.visibility === "public"
                      ? "globe-outline"
                      : "lock-closed-outline"
                  }
                  size={14}
                  color={mutedFg}
                />
                <Text className="text-sm text-foreground">
                  {t(`detail.visibility.${runtime!.visibility}`)}
                </Text>
              </View>
            </View>
            {runtime!.device_info ? (
              <View className="flex-row items-center justify-between px-4 py-3">
                <Text className="text-sm text-muted-foreground">
                  {t("detail.device_label")}
                </Text>
                <Text className="text-sm text-foreground" numberOfLines={1}>
                  {runtime!.device_info}
                </Text>
              </View>
            ) : null}
            <View className="flex-row items-center justify-between px-4 py-3">
              <Text className="text-sm text-muted-foreground">
                {t("detail.created_label")}
              </Text>
              <Text className="text-sm text-foreground">
                {timeAgo(runtime!.created_at)}
              </Text>
            </View>
            <View className="flex-row items-center justify-between px-4 py-3">
              <Text className="text-sm text-muted-foreground">
                {t("detail.updated_label")}
              </Text>
              <Text className="text-sm text-foreground">
                {timeAgo(runtime!.updated_at)}
              </Text>
            </View>
          </SectionGroup>

          {cliVersion ? (
            <SectionGroup>
              <View className="flex-row items-center justify-between px-4 py-3">
                <Text className="text-sm text-muted-foreground">
                  {t("detail.cli_version_label")}
                </Text>
                <Text className="text-sm text-foreground">{cliVersion}</Text>
              </View>
              {needsUpdate ? (
                <View className="px-4 py-3">
                  <Text className="text-sm text-warning">
                    {t("detail.update_available", { version: latestVersion })}
                  </Text>
                </View>
              ) : null}
            </SectionGroup>
          ) : null}

          <SectionGroup title={t("detail.agents_title")}>
            {runtimeAgents.length === 0 ? (
              <View className="px-4 py-3.5">
                <Text className="text-sm text-muted-foreground">
                  {t("detail.agents_empty")}
                </Text>
              </View>
            ) : (
              runtimeAgents.map((agent) => (
                <View
                  key={agent.id}
                  className="flex-row items-center gap-3 px-4 py-3"
                >
                  <ActorAvatar type="agent" id={agent.id} size={24} />
                  <Text className="text-sm text-foreground">{agent.name}</Text>
                </View>
              ))
            )}
          </SectionGroup>
        </ScrollView>
      )}
    </View>
  );
}
```

Note: the `runtime!` non-null assertions are safe here for the same
reason `skill!` was safe in the Skills detail screen — they're only
reached in the branch where `isLoading` is `false` and `notFound` is
`false`, which TypeScript can't narrow through the JSX ternary but the
runtime guard already excludes. `health!` is safe the same way: `health`
is only `null` when `runtime` is falsy, and the ternary that reaches
`health!` already established `runtime` exists.

- [ ] **Step 2: Register the detail route**

In `apps/mobile/app/(app)/[workspace]/_layout.tsx`, add directly after the
`more/runtimes` Stack.Screen from Task 2 Step 3:

```tsx
        <Stack.Screen
          name="more/runtimes/[id]"
          options={{
            title: tRuntimes("detail.header_default_title"),
            headerBackTitle: tCommon("nav.back"),
          }}
        />
```

- [ ] **Step 3: Verify**

```bash
cd apps/mobile
pnpm exec tsc --noEmit -p .
pnpm exec eslint "app/(app)/[workspace]/more/runtimes/[id].tsx" "app/(app)/[workspace]/_layout.tsx"
```

Expected: both commands exit 0.

- [ ] **Step 4: Commit**

```bash
git add "apps/mobile/app/(app)/[workspace]/more/runtimes/[id].tsx" "apps/mobile/app/(app)/[workspace]/_layout.tsx"
git status --short   # confirm the new [id].tsx route is tracked
git commit -m "feat(mobile): add Runtime detail screen"
```

---

### Task 4: Manual bilingual verification

**Files:** none (verification only).

**Interfaces:** none — this task only exercises Tasks 1-3's surface.

- [ ] **Step 1: Full-suite automated check**

```bash
cd apps/mobile
pnpm typecheck
pnpm lint
pnpm test
```

Expected: all three exit 0.

- [ ] **Step 2: Manual pass — English**

With the dev build running on the device/simulator used for prior manual
verification in this branch:

1. Tap the More tab → tap the "Runtimes" row (after Skills) → list screen
   renders with correct health dots, visibility icons, and last-seen
   times, sorted online-first.
2. If the workspace has zero runtimes, confirm the empty state renders
   instead of a blank list.
3. Tap a runtime → detail screen renders: name, health badge, owner, mode,
   visibility, device info (if present), created/updated timestamps.
4. If the runtime has a CLI version recorded, confirm the "CLI Version"
   row shows it; confirm the "update available" line only appears when
   the runtime is genuinely local + owned by the signed-in user + not
   desktop-launched + behind the latest GitHub release — if none of the
   workspace's runtimes qualify, confirm the badge correctly does NOT
   appear rather than forcing the scenario.
5. Confirm "Agents on this runtime" lists exactly the agents whose
   `runtime_id` matches this runtime (cross-check against desktop's
   runtime detail page for the same runtime, or against the Agents list
   filtered by runtime if desktop exposes that).

- [ ] **Step 3: Manual pass — Chinese**

Switch language to 简体中文 (Settings → Language) and repeat Step 2's
5 checks. Confirm every string reads "运行时" (never left in English,
never a different term), and the four health labels read 在线 / 最近失联 /
离线 / 即将清理, matching desktop's own wording exactly.

- [ ] **Step 4: Confirm no regression on adjacent screens**

Tap through Pinned/Issues/Projects/Skills from the More page (the four
rows Runtimes was added after) — confirm they still navigate correctly
and the new NavRow didn't disturb their layout or ordering.

No commit for this task — it's verification-only. If any step surfaces a
defect, fix it in the task file it belongs to and re-run that task's
verification before moving on.
