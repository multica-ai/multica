# Mobile Skills Browse Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a read-only Skills browse feature (list → detail → file
content viewer) to the mobile More page, mirroring desktop's Skills
section without any of its create/edit/delete/file-editing surface.

**Architecture:** Three new pushed Stack routes under `more/skills` (list,
detail, file viewer), a new mobile-owned data layer (`data/queries/skills.ts`
+ two new `api.ts` methods + Zod schemas), a new `skills.json` i18n
namespace, and one new nav row on the existing More page. No backend
changes — reuses `GET /api/skills` / `GET /api/skills/:id`.

**Tech Stack:** Expo Router (pushed Stack screens + one catch-all dynamic
segment), TanStack Query, Zod (`parseWithFallback`), i18next/react-i18next,
NativeWind.

## Global Constraints

- Round 1 of 4 desktop-feature-migration rounds (Runtimes / Usage / Agents
  are separate future plans) — do not add anything from those domains here.
- Read-only: no create, edit, delete, or skill-to-agent assignment changes.
  No file editing. No search/sort/batch actions.
- Mobile's import whitelist from `packages/` is `@multica/core` only (types
  + pure functions). `packages/views/skills/lib/origin.ts`'s
  `readOrigin`/`totalFileCount` do NOT qualify (they live in
  `packages/views`) — mirror that logic into a mobile-local file instead of
  importing it.
- Every new read-side `api.ts` method takes `opts?: { signal?: AbortSignal }`
  and forwards it; every new `queryFn` destructures `{ signal }` from
  TanStack Query and forwards it to the `api.*` call (apps/mobile/CLAUDE.md
  "Lessons learned" #4 — `grep -n "queryFn: () =>" apps/mobile/data/queries/`
  must stay empty).
- New query key factory follows the existing 3-segment shape
  (`all(wsId)` → `list(wsId)` / `detail(wsId, id)`), same convention as
  every other mobile feature.
- New response types are parsed with `parseWithFallback` + a Zod schema —
  never `as T` on network JSON.
- Chinese copy: `skill` is one of the mixed-rule entities in
  `apps/docs/content/docs/developers/conventions.mdx` (with `issue`/`task`)
  — it has no settled Chinese translation, so it stays lowercase English in
  every zh-Hans UI string, **including page/header titles** (this mobile
  app's own established precedent — see `issues.json`'s
  `all_issues.header_title` = `"issue"` in zh-Hans, even though desktop's
  own sidebar capitalizes "Skills" — mobile already made the stricter call
  during the i18n final review and this plan matches that, not desktop).
- New `_one`/`_other` pluralized keys need a zh-Hans `_one` variant too even
  though it never renders (`Intl.PluralRules` has no "one" category for
  zh-Hans) — the locale parity test checks key sets match, not runtime
  reachability. See `apps/mobile/CLAUDE.md`'s "Pluralization" section.
- Every new source file must be `git ls-files`-tracked before its task's
  commit (apps/mobile/CLAUDE.md lesson #2) — none of the paths in this plan
  are inside a directory with a generic root `.gitignore` rule, but confirm
  with `git status` after `git add` regardless.

---

### Task 1: Data layer — schemas, API client methods, query options, i18n namespace

**Files:**
- Modify: `apps/mobile/data/schemas.ts`
- Modify: `apps/mobile/data/api.ts`
- Create: `apps/mobile/data/queries/skills.ts`
- Create: `apps/mobile/locales/en/skills.json`
- Create: `apps/mobile/locales/zh-Hans/skills.json`
- Modify: `apps/mobile/locales/index.ts`

**Interfaces:**
- Produces: `SkillSummarySchema`, `SkillSchema`, `SkillSummaryListSchema`,
  `EMPTY_SKILL_SUMMARY_LIST: SkillSummary[]`, `EMPTY_SKILL: Skill` (all in
  `data/schemas.ts`); `api.listSkills(opts?: { signal?: AbortSignal }): Promise<SkillSummary[]>`
  and `api.getSkill(id: string, opts?: { signal?: AbortSignal }): Promise<Skill>`
  (both in `data/api.ts`); `skillKeys`, `skillListOptions(wsId)`,
  `skillDetailOptions(wsId, id)` (in `data/queries/skills.ts`) — Tasks 2-4
  import these three.

- [ ] **Step 1: Add Skill types to the schemas.ts type-only import block**

In `apps/mobile/data/schemas.ts`, the `import type { ... } from "@multica/core/types"` block currently starts:

```ts
import type {
  Agent,
  AgentInvocationTarget,
  AgentTask,
  Attachment,
```

Add `Skill` and `SkillSummary` in alphabetical order (after `SendChatMessageResponse`, before `Squad`):

```ts
import type {
  Agent,
  AgentInvocationTarget,
  AgentTask,
  Attachment,
  ChatMessage,
  ChatPendingTask,
  ChatSession,
  Comment,
  InboxItem,
  IssueLabelsResponse,
  Label,
  ListLabelsResponse,
  ListProjectResourcesResponse,
  ListProjectsResponse,
  MemberWithUser,
  PinnedItem,
  Project,
  ProjectResource,
  RuntimeDevice,
  SearchIssuesResponse,
  SearchProjectsResponse,
  SendChatMessageResponse,
  Skill,
  SkillSummary,
  Squad,
  TaskMessagePayload,
  User,
  Workspace,
} from "@multica/core/types";
```

- [ ] **Step 2: Add Skill schemas + fallbacks**

In `apps/mobile/data/schemas.ts`, find this exact block:

```ts
export const EMPTY_PROJECT: Project = {
  id: "",
  workspace_id: "",
  title: "",
  description: null,
  icon: null,
  status: "planned",
  priority: "none",
  lead_type: null,
  lead_id: null,
  created_at: "",
  updated_at: "",
  issue_count: 0,
  done_count: 0,
  resource_count: 0,
};
```

Add directly after it (before the `// Project resources are typed pointers...` comment):

```ts
// Skill.content routinely runs 50-200KB (see the doc comment on
// SkillSummary in @multica/core/types) — `.loose()` so any future field
// the backend adds passes through unchanged rather than getting stripped.
const SkillFileSchema = z.object({
  id: z.string(),
  skill_id: z.string(),
  path: z.string(),
  content: z.string(),
  created_at: z.string(),
  updated_at: z.string(),
}).loose();

export const SkillSummarySchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  name: z.string(),
  description: z.string(),
  config: z.record(z.string(), z.unknown()).default({}),
  created_by: z.string().nullable(),
  created_at: z.string(),
  updated_at: z.string(),
}).loose();

export const SkillSummaryListSchema = z.array(SkillSummarySchema).default([]);
export const EMPTY_SKILL_SUMMARY_LIST: SkillSummary[] = [];

export const SkillSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  name: z.string(),
  description: z.string(),
  config: z.record(z.string(), z.unknown()).default({}),
  created_by: z.string().nullable(),
  created_at: z.string(),
  updated_at: z.string(),
  content: z.string(),
  files: z.array(SkillFileSchema).default([]),
}).loose();

// Fallback for `GET /api/skills/{id}` when the response shape drifts.
// `id` defaults to empty — caller checks `data.id === ""` to render the
// "not found / shape drifted" error state, same pattern as EMPTY_PROJECT.
export const EMPTY_SKILL: Skill = {
  id: "",
  workspace_id: "",
  name: "",
  description: "",
  config: {},
  created_by: null,
  created_at: "",
  updated_at: "",
  content: "",
  files: [],
};
```

- [ ] **Step 3: Add `listSkills`/`getSkill` to the API client**

In `apps/mobile/data/api.ts`, find this exact block (the end of `getProject`):

```ts
  async getProject(
    id: string,
    opts?: { signal?: AbortSignal },
  ): Promise<Project> {
    const raw = await this.fetch<unknown>(`/api/projects/${id}`, {
      signal: opts?.signal,
    });
    // Drift-safe parse — UI checks `data.id === ""` to render the
    // "project not found / shape drifted" error state instead of a
    // half-populated detail page.
    return parseWithFallback(raw, ProjectSchema, EMPTY_PROJECT, {
      endpoint: "GET /api/projects/:id",
    });
  }
```

Add directly after it (before the `// Write endpoints` comment):

```ts
  async listSkills(opts?: { signal?: AbortSignal }): Promise<SkillSummary[]> {
    return this.fetchValidated(
      "/api/skills",
      SkillSummaryListSchema,
      EMPTY_SKILL_SUMMARY_LIST,
      { ...opts, endpoint: "listSkills" },
    );
  }

  async getSkill(
    id: string,
    opts?: { signal?: AbortSignal },
  ): Promise<Skill> {
    return this.fetchValidated(`/api/skills/${id}`, SkillSchema, EMPTY_SKILL, {
      ...opts,
      endpoint: "GET /api/skills/:id",
    });
  }
```

Then add `Skill` and `SkillSummary` to `api.ts`'s existing
`import type { ... } from "@multica/core/types"` block. Find:

```ts
  SendChatMessageResponse,
  Squad,
```

Replace with:

```ts
  SendChatMessageResponse,
  Skill,
  SkillSummary,
  Squad,
```

And add the four new schema/fallback names to `api.ts`'s existing
`import { ... } from "./schemas"` block. Find:

```ts
  EMPTY_SEARCH_PROJECTS_RESPONSE,
  EMPTY_SQUAD_LIST,
```

Replace with:

```ts
  EMPTY_SEARCH_PROJECTS_RESPONSE,
  EMPTY_SKILL,
  EMPTY_SKILL_SUMMARY_LIST,
  EMPTY_SQUAD_LIST,
```

and find:

```ts
  SearchProjectsResponseSchema,
  SendChatMessageResponseSchema,
```

Replace with:

```ts
  SearchProjectsResponseSchema,
  SendChatMessageResponseSchema,
  SkillSchema,
  SkillSummaryListSchema,
```

- [ ] **Step 4: Create the query options file**

Create `apps/mobile/data/queries/skills.ts`:

```ts
import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

export const skillKeys = {
  all: (wsId: string | null) => ["skills", wsId] as const,
  list: (wsId: string | null) => [...skillKeys.all(wsId), "list"] as const,
  detail: (wsId: string | null, id: string) =>
    [...skillKeys.all(wsId), "detail", id] as const,
};

export const skillListOptions = (wsId: string | null) =>
  queryOptions({
    queryKey: skillKeys.list(wsId),
    queryFn: ({ signal }) => api.listSkills({ signal }),
    enabled: !!wsId,
  });

export const skillDetailOptions = (wsId: string | null, id: string) =>
  queryOptions({
    queryKey: skillKeys.detail(wsId, id),
    queryFn: ({ signal }) => api.getSkill(id, { signal }),
    enabled: !!wsId && !!id,
  });
```

- [ ] **Step 5: Create the `skills.json` locale namespace (en)**

Create `apps/mobile/locales/en/skills.json`:

```json
{
  "list": {
    "header_title": "Skills",
    "error": {
      "load_prefix": "Failed to load skills:",
      "unknown": "unknown error",
      "retry": "Retry"
    },
    "empty": {
      "title": "No skills yet",
      "message": "Skills created on desktop will show up here."
    },
    "used_by_one": "Used by {{count}} agent",
    "used_by_other": "Used by {{count}} agents"
  },
  "origin": {
    "manual": "Manual",
    "runtime_local": "Imported from runtime",
    "clawhub": "ClawHub",
    "skills_sh": "Skills.sh",
    "github": "GitHub"
  },
  "detail": {
    "header_default_title": "Skill",
    "error": {
      "load_prefix": "Failed to load skill:",
      "unknown": "unknown error",
      "retry": "Retry"
    },
    "creator_label": "Created by",
    "created_label": "Created",
    "updated_label": "Updated",
    "used_by_title": "Used by",
    "used_by_empty": "Not used by any agent yet",
    "files_title": "Files",
    "file_root_label": "SKILL.md"
  },
  "file": {
    "header_default_title": "File"
  }
}
```

- [ ] **Step 6: Create the `skills.json` locale namespace (zh-Hans)**

Create `apps/mobile/locales/zh-Hans/skills.json`. `skill` stays lowercase
English per the mixed-rule entity glossary — see Global Constraints above:

```json
{
  "list": {
    "header_title": "skill",
    "error": {
      "load_prefix": "加载 skill 失败：",
      "unknown": "未知错误",
      "retry": "重试"
    },
    "empty": {
      "title": "还没有 skill",
      "message": "在桌面端创建的 skill 会显示在这里。"
    },
    "used_by_one": "{{count}} 个智能体在使用",
    "used_by_other": "{{count}} 个智能体在使用"
  },
  "origin": {
    "manual": "手动创建",
    "runtime_local": "从运行时导入",
    "clawhub": "ClawHub",
    "skills_sh": "Skills.sh",
    "github": "GitHub"
  },
  "detail": {
    "header_default_title": "skill",
    "error": {
      "load_prefix": "加载 skill 失败：",
      "unknown": "未知错误",
      "retry": "重试"
    },
    "creator_label": "创建者",
    "created_label": "创建时间",
    "updated_label": "更新时间",
    "used_by_title": "使用者",
    "used_by_empty": "还没有智能体使用",
    "files_title": "文件",
    "file_root_label": "SKILL.md"
  },
  "file": {
    "header_default_title": "文件"
  }
}
```

- [ ] **Step 7: Register the namespace in the locale registry**

Replace the entire contents of `apps/mobile/locales/index.ts` with (this
adds the `enSkills`/`zhHansSkills` imports in alphabetical order, after
`Settings` and before `Workspace`, and registers `skills` in both
`RESOURCES` entries):

```ts
import enAuth from "./en/auth.json";
import enChat from "./en/chat.json";
import enCommon from "./en/common.json";
import enInbox from "./en/inbox.json";
import enIssues from "./en/issues.json";
import enProjects from "./en/projects.json";
import enSettings from "./en/settings.json";
import enSkills from "./en/skills.json";
import enWorkspace from "./en/workspace.json";
import zhHansAuth from "./zh-Hans/auth.json";
import zhHansChat from "./zh-Hans/chat.json";
import zhHansCommon from "./zh-Hans/common.json";
import zhHansInbox from "./zh-Hans/inbox.json";
import zhHansIssues from "./zh-Hans/issues.json";
import zhHansProjects from "./zh-Hans/projects.json";
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
    settings: zhHansSettings,
    skills: zhHansSkills,
    workspace: zhHansWorkspace,
  },
} satisfies Record<SupportedLocale, Record<string, Record<string, unknown>>>;
```

- [ ] **Step 8: Verify**

```bash
cd apps/mobile
pnpm exec tsc --noEmit -p .
pnpm exec eslint data/schemas.ts data/api.ts data/queries/skills.ts locales/index.ts
pnpm test -- parity
```

Expected: all three commands exit 0. The parity test in particular confirms
the new `skills` namespace has identical key sets in both locale files.

- [ ] **Step 9: Commit**

```bash
git add apps/mobile/data/schemas.ts apps/mobile/data/api.ts apps/mobile/data/queries/skills.ts apps/mobile/locales/index.ts apps/mobile/locales/en/skills.json apps/mobile/locales/zh-Hans/skills.json
git status --short   # confirm both new locale files show as new (A), not untracked-and-ignored
git commit -m "feat(mobile): add Skills data layer and i18n namespace"
```

---

### Task 2: Origin helper, list screen, More-page nav entry

**Files:**
- Create: `apps/mobile/lib/skill-origin.ts`
- Create: `apps/mobile/components/skill/skill-origin-badge.tsx`
- Create: `apps/mobile/components/skill/skill-row.tsx`
- Create: `apps/mobile/app/(app)/[workspace]/more/skills.tsx`
- Modify: `apps/mobile/app/(app)/[workspace]/_layout.tsx`
- Modify: `apps/mobile/app/(app)/[workspace]/(tabs)/more.tsx`
- Modify: `apps/mobile/locales/en/workspace.json`
- Modify: `apps/mobile/locales/zh-Hans/workspace.json`

**Interfaces:**
- Consumes: `skillListOptions(wsId)` (Task 1), `agentListOptions(wsId)`
  (`apps/mobile/data/queries/agents.ts`, pre-existing),
  `selectSkillAssignments` (`@multica/core/workspace/queries`, pre-existing
  pure function — types + pure-function whitelist), `SectionGroup`/`NavRow`
  (`@/components/ui/section-group`, pre-existing).
- Produces: `readSkillOrigin(skill: SkillSummary): SkillOriginInfo` (in
  `lib/skill-origin.ts`) and `<SkillOriginBadge skill={SkillSummary} />` —
  Task 3 imports both.

- [ ] **Step 1: Mirror the origin-parsing logic**

Create `apps/mobile/lib/skill-origin.ts`:

```ts
/**
 * Mirrors packages/views/skills/lib/origin.ts's readOrigin — copied, not
 * imported. Mobile's package-boundary rule only whitelists @multica/core
 * (types + pure functions); packages/views is out of bounds regardless of
 * purity. See apps/mobile/CLAUDE.md "Mobile-owned updaters" for the same
 * mirror-don't-import rationale applied to realtime WS updaters.
 */
import type { SkillSummary } from "@multica/core/types";

export type SkillOriginInfo = {
  type: "runtime_local" | "clawhub" | "skills_sh" | "github" | "manual";
  provider?: string;
  runtime_id?: string;
  source_path?: string;
  source_url?: string;
};

export function readSkillOrigin(skill: SkillSummary): SkillOriginInfo {
  const raw = (skill.config?.origin ?? null) as
    | (SkillOriginInfo & Record<string, unknown>)
    | null;
  if (raw?.type === "runtime_local") return raw;
  if (raw?.type === "clawhub") return raw;
  if (raw?.type === "skills_sh") return raw;
  if (raw?.type === "github") return raw;
  return { type: "manual" };
}
```

- [ ] **Step 2: Build the origin badge component**

Create `apps/mobile/components/skill/skill-origin-badge.tsx`:

```tsx
import { View } from "react-native";
import { useTranslation } from "react-i18next";
import type { SkillSummary } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { readSkillOrigin } from "@/lib/skill-origin";

export function SkillOriginBadge({ skill }: { skill: SkillSummary }) {
  const { t } = useTranslation("skills");
  const origin = readSkillOrigin(skill);

  return (
    <View className="self-start rounded bg-secondary px-1.5 py-0.5">
      <Text className="text-[10px] text-muted-foreground">
        {t(`origin.${origin.type}`)}
      </Text>
    </View>
  );
}
```

- [ ] **Step 3: Build the list row component**

Create `apps/mobile/components/skill/skill-row.tsx`, mirroring the visual
shape of `apps/mobile/components/project/project-row.tsx` (icon + flex
title/description column + right-side count/time column):

```tsx
import { Pressable, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { useTranslation } from "react-i18next";
import type { SkillSummary } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { SkillOriginBadge } from "./skill-origin-badge";
import { timeAgo } from "@/lib/time-ago";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";

interface Props {
  skill: SkillSummary;
  usedByCount: number;
  onPress: () => void;
}

export function SkillRow({ skill, usedByCount, onPress }: Props) {
  const { t } = useTranslation("skills");
  const { colorScheme } = useColorScheme();
  const mutedFg = THEME[colorScheme].mutedForeground;

  return (
    <Pressable onPress={onPress} className="active:bg-secondary px-4 py-3">
      <View className="flex-row items-start gap-3">
        <Ionicons
          name="book-outline"
          size={20}
          color={mutedFg}
          style={{ marginTop: 2 }}
        />
        <View className="flex-1 gap-1">
          <Text
            className="text-base text-foreground font-medium"
            numberOfLines={1}
          >
            {skill.name}
          </Text>
          {skill.description ? (
            <Text className="text-xs text-muted-foreground" numberOfLines={1}>
              {skill.description}
            </Text>
          ) : null}
          <SkillOriginBadge skill={skill} />
        </View>
        <View className="items-end gap-1">
          <Text className="text-xs text-muted-foreground tabular-nums">
            {t("list.used_by", { count: usedByCount })}
          </Text>
          <Text className="text-[11px] text-muted-foreground/70">
            {timeAgo(skill.updated_at)}
          </Text>
        </View>
      </View>
    </Pressable>
  );
}
```

- [ ] **Step 4: Build the list screen**

Create `apps/mobile/app/(app)/[workspace]/more/skills.tsx`, mirroring
`apps/mobile/app/(app)/[workspace]/more/projects.tsx`'s structure (flat
`FlatList` + `RefreshControl`, native Stack header, no in-body header, no
create button since this is read-only):

```tsx
/**
 * Skills browse page. Flat FlatList over the workspace's skills, read-only
 * (no create/edit/delete — see docs/superpowers/specs/2026-07-08-mobile-
 * skills-browse-design.md). Sort: client-side by `updated_at` desc,
 * mirrors the Projects list's default ordering.
 */
import { useMemo } from "react";
import { ActivityIndicator, FlatList, RefreshControl, View } from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { useQuery } from "@tanstack/react-query";
import { router } from "expo-router";
import { useTranslation } from "react-i18next";
import { selectSkillAssignments } from "@multica/core/workspace/queries";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { SkillRow } from "@/components/skill/skill-row";
import { skillListOptions } from "@/data/queries/skills";
import { agentListOptions } from "@/data/queries/agents";
import { useWorkspaceStore } from "@/data/workspace-store";

export default function SkillsPage() {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const { t } = useTranslation("skills");

  const { data, isLoading, error, refetch, isRefetching } = useQuery(
    skillListOptions(wsId),
  );
  const { data: agents } = useQuery(agentListOptions(wsId));

  const assignments = useMemo(
    () => selectSkillAssignments(agents),
    [agents],
  );

  const sorted = useMemo(() => {
    if (!data) return [];
    return [...data].sort(
      (a, b) =>
        new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime(),
    );
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
            <SkillRow
              skill={item}
              usedByCount={assignments.get(item.id)?.length ?? 0}
              onPress={() => {
                if (wsSlug) router.push(`/${wsSlug}/more/skills/${item.id}`);
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

- [ ] **Step 5: Register the list route's header**

In `apps/mobile/app/(app)/[workspace]/_layout.tsx`, add the `skills`
namespace hook next to the others (find this block):

```ts
  const { t: tIssues } = useTranslation("issues");
  const { t: tProjects } = useTranslation("projects");
  const { t: tSettings } = useTranslation("settings");
  const { t: tWorkspace } = useTranslation("workspace");
  const { t: tChat } = useTranslation("chat");
  const { t: tCommon } = useTranslation("common");
```

becomes:

```ts
  const { t: tIssues } = useTranslation("issues");
  const { t: tProjects } = useTranslation("projects");
  const { t: tSettings } = useTranslation("settings");
  const { t: tSkills } = useTranslation("skills");
  const { t: tWorkspace } = useTranslation("workspace");
  const { t: tChat } = useTranslation("chat");
  const { t: tCommon } = useTranslation("common");
```

Then find this exact block (the `more/pins` registration):

```tsx
        <Stack.Screen
          name="more/pins"
          options={{
            title: tWorkspace("pins.header_title"),
            headerBackTitle: tCommon("nav.back"),
          }}
        />
```

Add directly after it:

```tsx
        <Stack.Screen
          name="more/skills"
          options={{
            title: tSkills("list.header_title"),
            headerBackTitle: tCommon("nav.back"),
          }}
        />
```

- [ ] **Step 6: Add the More-page nav row**

In `apps/mobile/locales/en/workspace.json`, find the `more_page.nav` object
(it currently has `pinned`/`issues`/`projects`) and add `skills` after
`projects`:

```json
"nav": {
  "pinned": "Pinned",
  "issues": "Issues",
  "projects": "Projects",
  "skills": "Skills"
}
```

In `apps/mobile/locales/zh-Hans/workspace.json`, same location — `skill`
stays lowercase English per the mixed-rule glossary, matching how
`nav.issues` is already `"issue"` there:

```json
"nav": {
  "pinned": "置顶",
  "issues": "issue",
  "projects": "项目",
  "skills": "skill"
}
```

(Use the actual existing `pinned`/`issues`/`projects` values already in
each file — only add the new `skills` key, don't change the others.)

In `apps/mobile/app/(app)/[workspace]/(tabs)/more.tsx`, find the second
`SectionGroup` (find this exact block):

```tsx
        <SectionGroup title={t("more_page.section_title")}>
          <NavRow
            onPress={() => slug && router.push(`/${slug}/more/pins`)}
            chevronColor={mutedFg}
            title={t("more_page.nav.pinned")}
          />
          <NavRow
            onPress={() => slug && router.push(`/${slug}/more/issues`)}
            chevronColor={mutedFg}
            title={t("more_page.nav.issues")}
          />
          <NavRow
            onPress={() => slug && router.push(`/${slug}/more/projects`)}
            chevronColor={mutedFg}
            title={t("more_page.nav.projects")}
          />
        </SectionGroup>
```

Add a fourth `NavRow` after Projects:

```tsx
        <SectionGroup title={t("more_page.section_title")}>
          <NavRow
            onPress={() => slug && router.push(`/${slug}/more/pins`)}
            chevronColor={mutedFg}
            title={t("more_page.nav.pinned")}
          />
          <NavRow
            onPress={() => slug && router.push(`/${slug}/more/issues`)}
            chevronColor={mutedFg}
            title={t("more_page.nav.issues")}
          />
          <NavRow
            onPress={() => slug && router.push(`/${slug}/more/projects`)}
            chevronColor={mutedFg}
            title={t("more_page.nav.projects")}
          />
          <NavRow
            onPress={() => slug && router.push(`/${slug}/more/skills`)}
            chevronColor={mutedFg}
            title={t("more_page.nav.skills")}
          />
        </SectionGroup>
```

- [ ] **Step 7: Verify**

```bash
cd apps/mobile
pnpm exec tsc --noEmit -p .
pnpm exec eslint lib/skill-origin.ts components/skill/skill-origin-badge.tsx components/skill/skill-row.tsx "app/(app)/[workspace]/more/skills.tsx" "app/(app)/[workspace]/_layout.tsx" "app/(app)/[workspace]/(tabs)/more.tsx"
pnpm test -- parity
```

Expected: all commands exit 0.

- [ ] **Step 8: Commit**

```bash
git add apps/mobile/lib/skill-origin.ts apps/mobile/components/skill/skill-origin-badge.tsx apps/mobile/components/skill/skill-row.tsx "apps/mobile/app/(app)/[workspace]/more/skills.tsx" "apps/mobile/app/(app)/[workspace]/_layout.tsx" "apps/mobile/app/(app)/[workspace]/(tabs)/more.tsx" apps/mobile/locales/en/workspace.json apps/mobile/locales/zh-Hans/workspace.json
git status --short   # confirm components/skill/ and the new skills.tsx route are tracked, not ignored
git commit -m "feat(mobile): add Skills list screen and More-page nav entry"
```

---

### Task 3: Skill detail screen

**Files:**
- Create: `apps/mobile/app/(app)/[workspace]/more/skills/[id].tsx`
- Modify: `apps/mobile/app/(app)/[workspace]/_layout.tsx`

**Interfaces:**
- Consumes: `skillDetailOptions(wsId, id)` (Task 1),
  `agentListOptions(wsId)` + `selectSkillAssignments` (pre-existing, same
  as Task 2), `<SkillOriginBadge skill={...} />` (Task 2),
  `SectionGroup`/`NavRow` (`@/components/ui/section-group`), `ActorAvatar`
  (`@/components/ui/actor-avatar`), `useActorLookup`
  (`@/data/use-actor-name`).
- Produces: pushes `more/skills/${id}/file/${encodedPath}` — Task 4's route
  must accept that URL shape.

- [ ] **Step 1: Build the detail screen**

Create `apps/mobile/app/(app)/[workspace]/more/skills/[id].tsx`:

```tsx
/**
 * Skill detail — read-only. Metadata, the agents that use this skill, and
 * its file list. Tapping a file pushes the read-only file viewer
 * (more/skills/[id]/file/[...path]). No create/edit/delete on mobile —
 * see docs/superpowers/specs/2026-07-08-mobile-skills-browse-design.md.
 */
import { useMemo } from "react";
import { ActivityIndicator, ScrollView, View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import { Stack, router, useLocalSearchParams } from "expo-router";
import { useTranslation } from "react-i18next";
import { selectSkillAssignments } from "@multica/core/workspace/queries";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { NavRow, SectionGroup } from "@/components/ui/section-group";
import { SkillOriginBadge } from "@/components/skill/skill-origin-badge";
import { skillDetailOptions } from "@/data/queries/skills";
import { agentListOptions } from "@/data/queries/agents";
import { useActorLookup } from "@/data/use-actor-name";
import { useWorkspaceStore } from "@/data/workspace-store";
import { timeAgo } from "@/lib/time-ago";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";

export default function SkillDetailPage() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const { t } = useTranslation("skills");
  const { colorScheme } = useColorScheme();
  const mutedFg = THEME[colorScheme].mutedForeground;
  const { getName } = useActorLookup();

  const { data: skill, isLoading, error, refetch } = useQuery(
    skillDetailOptions(wsId, id),
  );
  const { data: agents } = useQuery(agentListOptions(wsId));

  const usedBy = useMemo(() => {
    if (!skill || !agents) return [];
    return selectSkillAssignments(agents).get(skill.id) ?? [];
  }, [skill, agents]);

  const goFile = (path: string) => {
    if (!wsSlug || !skill) return;
    const encodedPath = path.split("/").map(encodeURIComponent).join("/");
    router.push(`/${wsSlug}/more/skills/${skill.id}/file/${encodedPath}`);
  };

  const notFound = !isLoading && (!skill || skill.id === "");

  return (
    <View className="flex-1 bg-background">
      <Stack.Screen
        options={{ title: skill?.name || t("detail.header_default_title") }}
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
              {skill!.name}
            </Text>
            {skill!.description ? (
              <Text className="text-sm text-muted-foreground">
                {skill!.description}
              </Text>
            ) : null}
            <SkillOriginBadge skill={skill!} />
          </View>

          <SectionGroup>
            <View className="flex-row items-center gap-3 px-4 py-3.5">
              <ActorAvatar type="member" id={skill!.created_by} size={28} />
              <View className="flex-1">
                <Text className="text-sm font-medium text-foreground">
                  {getName("member", skill!.created_by)}
                </Text>
                <Text className="text-xs text-muted-foreground">
                  {t("detail.creator_label")}
                </Text>
              </View>
            </View>
            <View className="flex-row items-center justify-between px-4 py-3">
              <Text className="text-sm text-muted-foreground">
                {t("detail.created_label")}
              </Text>
              <Text className="text-sm text-foreground">
                {timeAgo(skill!.created_at)}
              </Text>
            </View>
            <View className="flex-row items-center justify-between px-4 py-3">
              <Text className="text-sm text-muted-foreground">
                {t("detail.updated_label")}
              </Text>
              <Text className="text-sm text-foreground">
                {timeAgo(skill!.updated_at)}
              </Text>
            </View>
          </SectionGroup>

          <SectionGroup title={t("detail.used_by_title")}>
            {usedBy.length === 0 ? (
              <View className="px-4 py-3.5">
                <Text className="text-sm text-muted-foreground">
                  {t("detail.used_by_empty")}
                </Text>
              </View>
            ) : (
              usedBy.map((agent) => (
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

          <SectionGroup title={t("detail.files_title")}>
            <NavRow
              onPress={() => goFile("SKILL.md")}
              chevronColor={mutedFg}
              title={t("detail.file_root_label")}
            />
            {skill!.files.map((file) => (
              <NavRow
                key={file.id}
                onPress={() => goFile(file.path)}
                chevronColor={mutedFg}
                title={file.path}
              />
            ))}
          </SectionGroup>
        </ScrollView>
      )}
    </View>
  );
}
```

Note: the `skill!` non-null assertions are safe here because they're only
reached in the branch where `isLoading` is `false` and `notFound` is
`false` — TypeScript can't narrow through the JSX ternary structure, but
the runtime guard already excludes the null/empty cases.

- [ ] **Step 2: Register the detail route**

In `apps/mobile/app/(app)/[workspace]/_layout.tsx`, add directly after the
`more/skills` Stack.Screen from Task 2 Step 5:

```tsx
        <Stack.Screen
          name="more/skills/[id]"
          options={{
            title: tSkills("detail.header_default_title"),
            headerBackTitle: tCommon("nav.back"),
          }}
        />
```

- [ ] **Step 3: Verify**

```bash
cd apps/mobile
pnpm exec tsc --noEmit -p .
pnpm exec eslint "app/(app)/[workspace]/more/skills/[id].tsx" "app/(app)/[workspace]/_layout.tsx"
```

Expected: both commands exit 0.

- [ ] **Step 4: Commit**

```bash
git add "apps/mobile/app/(app)/[workspace]/more/skills/[id].tsx" "apps/mobile/app/(app)/[workspace]/_layout.tsx"
git status --short   # confirm the new [id].tsx route is tracked
git commit -m "feat(mobile): add Skill detail screen"
```

---

### Task 4: File viewer screen

**Files:**
- Create: `apps/mobile/app/(app)/[workspace]/more/skills/[id]/file/[...path].tsx`
- Modify: `apps/mobile/app/(app)/[workspace]/_layout.tsx`

**Interfaces:**
- Consumes: `skillDetailOptions(wsId, id)` (Task 1), `<Markdown content={string} />`
  (`@/lib/markdown`, pre-existing).
- Consumed by: Task 3's `goFile()` navigates here.

- [ ] **Step 1: Build the file viewer**

Create `apps/mobile/app/(app)/[workspace]/more/skills/[id]/file/[...path].tsx`.
`[...path]` is a catch-all segment because file paths can be nested (e.g.
`scripts/helper.py`) — a single `[path]` segment can't carry a slash. Expo
Router decodes each segment and returns them as a `string[]` via
`useLocalSearchParams`:

```tsx
/**
 * Skill file viewer — read-only. `.md` (including the synthesized
 * "SKILL.md" root) renders through the shared Markdown wrapper; every
 * other extension renders as plain monospaced text. No editing — see
 * docs/superpowers/specs/2026-07-08-mobile-skills-browse-design.md.
 */
import { ScrollView, View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import { Stack, useLocalSearchParams } from "expo-router";
import { useTranslation } from "react-i18next";
import { Text } from "@/components/ui/text";
import { Markdown } from "@/lib/markdown";
import { skillDetailOptions } from "@/data/queries/skills";
import { useWorkspaceStore } from "@/data/workspace-store";

export default function SkillFilePage() {
  const { id, path } = useLocalSearchParams<{ id: string; path: string[] }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { t } = useTranslation("skills");
  const { data: skill } = useQuery(skillDetailOptions(wsId, id));

  const fullPath = Array.isArray(path) ? path.join("/") : (path ?? "");
  const isRoot = fullPath === "SKILL.md";
  const content = isRoot
    ? skill?.content
    : skill?.files.find((f) => f.path === fullPath)?.content;
  const isMarkdown = fullPath.toLowerCase().endsWith(".md");

  return (
    <View className="flex-1 bg-background">
      <Stack.Screen
        options={{ title: fullPath || t("file.header_default_title") }}
      />
      <ScrollView contentContainerClassName="px-4 py-4">
        {content === undefined ? null : isMarkdown ? (
          <Markdown content={content} />
        ) : (
          <Text className="text-xs font-mono text-foreground">{content}</Text>
        )}
      </ScrollView>
    </View>
  );
}
```

- [ ] **Step 2: Register the file viewer route**

In `apps/mobile/app/(app)/[workspace]/_layout.tsx`, add directly after the
`more/skills/[id]` Stack.Screen from Task 3 Step 2:

```tsx
        <Stack.Screen
          name="more/skills/[id]/file/[...path]"
          options={{
            title: tSkills("file.header_default_title"),
            headerBackTitle: tSkills("detail.header_default_title"),
          }}
        />
```

- [ ] **Step 3: Verify**

```bash
cd apps/mobile
pnpm exec tsc --noEmit -p .
pnpm exec eslint "app/(app)/[workspace]/more/skills/[id]/file/[...path].tsx" "app/(app)/[workspace]/_layout.tsx"
```

Expected: both commands exit 0.

- [ ] **Step 4: Commit**

```bash
git add "apps/mobile/app/(app)/[workspace]/more/skills/[id]/file/[...path].tsx" "apps/mobile/app/(app)/[workspace]/_layout.tsx"
git status --short   # confirm the catch-all route file is tracked, not swallowed by a generic ignore rule
git commit -m "feat(mobile): add Skill file viewer screen"
```

---

### Task 5: Manual bilingual verification

**Files:** none (verification only).

**Interfaces:** none — this task only exercises Tasks 1-4's surface.

- [ ] **Step 1: Full-suite automated check**

```bash
cd apps/mobile
pnpm typecheck
pnpm lint
pnpm test
```

Expected: all three exit 0 (matches the plan's Global Constraints and every
prior task's per-step verification, run once more together at the end).

- [ ] **Step 2: Manual pass — English**

With the dev build running on the device/simulator used for prior manual
verification in this branch:

1. Tap the More tab → tap the "Skills" row → list screen renders with the
   correct title, per-row description/origin badge/used-by count/updated
   time.
2. If the workspace has zero skills, confirm the empty state renders
   instead of a blank list.
3. Tap a skill → detail screen renders: name, description, origin badge,
   creator name + avatar, created/updated timestamps, "Used by" section
   (agent rows if any, empty-state text if none), "Files" section listing
   SKILL.md plus every attached file.
4. Tap "SKILL.md" → markdown renders correctly, back button returns to the
   detail screen.
5. If any skill in the test workspace has an attached file, tap it →
   plain monospaced text renders (not markdown-parsed) unless the file
   itself ends in `.md`.
6. If any attached file has a nested path (contains `/`), confirm the file
   viewer still opens correctly and the title shows the full path — this
   is the one part of this plan with no prior in-repo precedent
   (`[...path]` catch-all segment), so it's worth explicitly confirming.

- [ ] **Step 3: Manual pass — Chinese**

Switch language to 简体中文 (Settings → Language) and repeat Step 2's
6 checks. Confirm every string uses lowercase `skill` (never `技能`, never
capitalized `Skills`) per the mixed-rule glossary, and the list/detail
header titles read `skill` (lowercase), matching how the Issues list
screen's header already reads `issue`.

- [ ] **Step 4: Confirm no regression on adjacent screens**

Tap through Pinned/Issues/Projects from the More page (the three rows
Skills was added after) — confirm they still navigate correctly and the
new NavRow didn't disturb their layout or ordering.

No commit for this task — it's verification-only. If any step surfaces a
defect, fix it in the task file it belongs to and re-run that task's
verification before moving on.
