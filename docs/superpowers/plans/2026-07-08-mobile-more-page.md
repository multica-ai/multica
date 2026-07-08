# Mobile More-Tab Page Conversion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the mobile app's More-tab dropdown popover with a real, navigable page carrying the exact same content and destinations.

**Architecture:** Extract the already-proven `SectionGroup`/`NavRow` list-row components out of `more/settings.tsx` into a shared file, then build the More page on top of them; remove the `tabPress` interception in the tab-bar layout and delete the dropdown component it drove.

**Tech Stack:** Expo Router (file-based routing), `react-i18next` (existing `workspace` namespace), existing `@/components/ui/*` primitives — no new dependencies.

## Global Constraints

- No new feature entry points — same 5 destinations as today: user row → Settings, workspace row → switch-workspace (disabled/no-chevron when the user has only one workspace), Pinned/Issues/Projects rows → their existing routes.
- `SectionGroup`/`NavRow` move to `apps/mobile/components/ui/section-group.tsx`, named exports, signatures unchanged.
- Locale namespace `workspace` (`apps/mobile/locales/{en,zh-Hans}/workspace.json`): rename `more_dropdown` → `more_page` (same leaf keys, same values), add one new key `more_page.section_title` = `"Workspace"` (en) / `"工作区"` (zh-Hans) — matches the existing precedent in `packages/views/locales/{en,zh-Hans}/layout.json`'s `sidebar.workspace_group`.
- No new test files — none of the touched files have existing coverage; verification is typecheck + lint + the locale parity test + manual pass.

---

### Task 1: Extract `SectionGroup`/`NavRow` to a shared component

**Files:**
- Create: `apps/mobile/components/ui/section-group.tsx`
- Modify: `apps/mobile/app/(app)/[workspace]/more/settings.tsx`

**Interfaces:**
- Produces: `NavRow` (props: `onPress: () => void; leading?: React.ReactNode; title: string; subtitle?: string; chevronColor: string`) and `SectionGroup` (props: `title: string; children: React.ReactNode`), both named exports from `@/components/ui/section-group`. Task 2 imports both.

- [ ] **Step 1: Create the shared component file**

Create `apps/mobile/components/ui/section-group.tsx`:

```tsx
import { Pressable, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { Text } from "@/components/ui/text";
import { cn } from "@/lib/utils";

export function NavRow({
  onPress,
  leading,
  title,
  subtitle,
  chevronColor,
}: {
  onPress: () => void;
  leading?: React.ReactNode;
  title: string;
  subtitle?: string;
  chevronColor: string;
}) {
  return (
    <Pressable
      onPress={onPress}
      className={cn(
        "flex-row items-center px-4 py-3.5 active:bg-secondary gap-3",
      )}
    >
      {leading}
      <View className="flex-1">
        <Text className="text-base font-medium text-foreground">{title}</Text>
        {subtitle ? (
          <Text className="text-sm text-muted-foreground mt-0.5">
            {subtitle}
          </Text>
        ) : null}
      </View>
      <Ionicons name="chevron-forward" size={18} color={chevronColor} />
    </Pressable>
  );
}

export function SectionGroup({
  title,
  children,
}: {
  /** Omit for a card with no header label (e.g. an identity row that
   * doesn't need to be captioned). */
  title?: string;
  children: React.ReactNode;
}) {
  return (
    <View className="gap-2">
      {title ? (
        <Text className="text-xs uppercase tracking-wider text-muted-foreground px-1">
          {title}
        </Text>
      ) : null}
      <View className="rounded-md border border-border bg-card overflow-hidden">
        {children}
      </View>
    </View>
  );
}
```

Note: `title` becomes optional here (every existing caller in
`settings.tsx` always passes one, so this is purely additive — no
existing call site changes behavior).

- [ ] **Step 2: Remove the local definitions from `settings.tsx` and import the shared ones**

Open `apps/mobile/app/(app)/[workspace]/more/settings.tsx`.

Delete the two local function definitions (currently at the bottom of the
file, lines 235-286 in the pre-change file):

```tsx
function NavRow({
  onPress,
  leading,
  title,
  subtitle,
  chevronColor,
}: {
  onPress: () => void;
  leading?: React.ReactNode;
  title: string;
  subtitle?: string;
  chevronColor: string;
}) {
  return (
    <Pressable
      onPress={onPress}
      className={cn(
        "flex-row items-center px-4 py-3.5 active:bg-secondary gap-3",
      )}
    >
      {leading}
      <View className="flex-1">
        <Text className="text-base font-medium text-foreground">{title}</Text>
        {subtitle ? (
          <Text className="text-sm text-muted-foreground mt-0.5">
            {subtitle}
          </Text>
        ) : null}
      </View>
      <Ionicons name="chevron-forward" size={18} color={chevronColor} />
    </Pressable>
  );
}

function SectionGroup({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <View className="gap-2">
      <Text className="text-xs uppercase tracking-wider text-muted-foreground px-1">
        {title}
      </Text>
      <View className="rounded-md border border-border bg-card overflow-hidden">
        {children}
      </View>
    </View>
  );
}
```

(Leave `WorkspaceRow` — the third helper function further below — exactly
where it is; it's specific to this screen, not part of this extraction.)

Add the import, next to the other `@/components/ui/*` imports near the
top of the file:

```tsx
import { NavRow, SectionGroup } from "@/components/ui/section-group";
```

Everything else in the file (`SettingsPage`, `WorkspaceRow`,
`initialsOf`) is unchanged — this is a pure move, not a behavior change.

- [ ] **Step 3: Verify typecheck, lint, and tests**

```bash
pnpm --filter @multica/mobile typecheck
pnpm --filter @multica/mobile lint
pnpm --filter @multica/mobile test
```

Expected: all three `PASS`. Typecheck will catch a missing import or a
leftover reference to the deleted local functions.

- [ ] **Step 4: Manual check — Settings screen unchanged**

Launch the app (or reload if already running), navigate to Settings via
the More tab's dropdown (still present until Task 2). Confirm every
section (Account, Workspaces, Appearance, Language) and the sign-out
button render and behave exactly as before — this step is purely to
confirm the extraction didn't change anything visually or functionally.

- [ ] **Step 5: Commit**

```bash
git add apps/mobile/components/ui/section-group.tsx apps/mobile/app/"(app)"/"[workspace]"/more/settings.tsx
git commit -m "refactor(mobile): extract SectionGroup/NavRow to a shared component

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

---

### Task 2: Build the More page, remove the dropdown

**Files:**
- Modify: `apps/mobile/locales/en/workspace.json`
- Modify: `apps/mobile/locales/zh-Hans/workspace.json`
- Modify: `apps/mobile/app/(app)/[workspace]/(tabs)/more.tsx`
- Modify: `apps/mobile/app/(app)/[workspace]/(tabs)/_layout.tsx`
- Delete: `apps/mobile/components/nav/more-tab-dropdown.tsx`

**Interfaces:**
- Consumes: `NavRow`/`SectionGroup` from `@/components/ui/section-group` (Task 1).
- Produces: `more.tsx` is now a real screen; the More `Tabs.Screen` navigates to it normally (no `listeners` override).

- [ ] **Step 1: Rename the locale namespace section, add the new key**

Open `apps/mobile/locales/en/workspace.json`. Change:

```json
  "more_dropdown": {
    "nav": {
      "pinned": "Pinned",
      "issues": "Issues",
      "projects": "Projects"
    },
    "account_settings_a11y": "Account settings",
    "switch_workspace_a11y": "Switch workspace",
    "workspace_fallback": "Workspace"
  },
```

to:

```json
  "more_page": {
    "section_title": "Workspace",
    "nav": {
      "pinned": "Pinned",
      "issues": "Issues",
      "projects": "Projects"
    },
    "account_settings_a11y": "Account settings",
    "switch_workspace_a11y": "Switch workspace",
    "workspace_fallback": "Workspace"
  },
```

Open `apps/mobile/locales/zh-Hans/workspace.json`. Change:

```json
  "more_dropdown": {
    "nav": {
      "pinned": "固定",
      "issues": "issue",
      "projects": "项目"
    },
    "account_settings_a11y": "账号设置",
    "switch_workspace_a11y": "切换工作区",
    "workspace_fallback": "工作区"
  },
```

to:

```json
  "more_page": {
    "section_title": "工作区",
    "nav": {
      "pinned": "固定",
      "issues": "issue",
      "projects": "项目"
    },
    "account_settings_a11y": "账号设置",
    "switch_workspace_a11y": "切换工作区",
    "workspace_fallback": "工作区"
  },
```

- [ ] **Step 2: Run the parity test**

```bash
pnpm --filter @multica/mobile test
```

Expected: `PASS`.

- [ ] **Step 3: Write the More page**

Replace the full contents of `apps/mobile/app/(app)/[workspace]/(tabs)/more.tsx`:

```tsx
/**
 * More tab — a real page (not a popover). Mirrors what the dropdown it
 * replaced showed: the user's identity (→ Settings), the current
 * workspace (→ switch-workspace), and shortcuts to Pinned/Issues/Projects.
 *
 * SectionGroup/NavRow are the same shared components more/settings.tsx
 * uses — this is the pattern's second call site, not a new one.
 */
import { Image, View } from "react-native";
import { router } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Ionicons } from "@expo/vector-icons";
import { Text } from "@/components/ui/text";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { NavRow, SectionGroup } from "@/components/ui/section-group";
import { WorkspaceAvatar } from "@/components/workspace/workspace-avatar";
import { workspaceListOptions } from "@/data/queries/workspaces";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";

function initialsOf(name: string | undefined): string {
  if (!name) return "?";
  return name
    .split(" ")
    .map((w) => w[0])
    .filter(Boolean)
    .slice(0, 2)
    .join("")
    .toUpperCase();
}

export default function MorePage() {
  const { t } = useTranslation("workspace");
  const { colorScheme } = useColorScheme();
  const mutedFg = THEME[colorScheme].mutedForeground;

  const slug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const user = useAuthStore((s) => s.user);
  const { data: workspaces } = useQuery(workspaceListOptions());
  const currentWorkspace = workspaces?.find((w) => w.slug === slug);
  const canSwitchWorkspace = (workspaces?.length ?? 0) > 1;
  const workspaceFallback = t("more_page.workspace_fallback");

  const goSettings = () => slug && router.push(`/${slug}/more/settings`);
  const goSwitchWorkspace = () =>
    slug && router.push(`/${slug}/switch-workspace`);

  return (
    <View className="flex-1 bg-background px-4 py-4 gap-6">
      <SectionGroup>
        <NavRow
          onPress={goSettings}
          chevronColor={mutedFg}
          leading={
            <Avatar
              alt={user?.name ?? t("more_page.account_settings_a11y")}
              className="size-10"
            >
              {user?.avatar_url ? (
                <AvatarImage source={{ uri: user.avatar_url }} />
              ) : null}
              <AvatarFallback>
                <Text className="text-sm font-semibold text-muted-foreground">
                  {initialsOf(user?.name)}
                </Text>
              </AvatarFallback>
            </Avatar>
          }
          title={user?.name ?? "—"}
          subtitle={user?.email}
        />
        {canSwitchWorkspace ? (
          <NavRow
            onPress={goSwitchWorkspace}
            chevronColor={mutedFg}
            leading={
              <WorkspaceAvatar
                name={currentWorkspace?.name ?? workspaceFallback}
                avatarUrl={currentWorkspace?.avatar_url}
                size={32}
              />
            }
            title={currentWorkspace?.name ?? workspaceFallback}
          />
        ) : (
          <View className="flex-row items-center px-4 py-3.5 gap-3">
            <WorkspaceAvatar
              name={currentWorkspace?.name ?? workspaceFallback}
              avatarUrl={currentWorkspace?.avatar_url}
              size={32}
            />
            <View className="flex-1">
              <Text className="text-base font-medium text-foreground">
                {currentWorkspace?.name ?? workspaceFallback}
              </Text>
            </View>
          </View>
        )}
      </SectionGroup>

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
    </View>
  );
}
```

Note: the first `SectionGroup` (identity + workspace row) intentionally
has no `title` — an account/workspace-identity row doesn't need a
caption, same convention as the very top of iOS's own Settings app. Only
the second section (Pinned/Issues/Projects) is captioned, with
`more_page.section_title` ("Workspace"/"工作区").

- [ ] **Step 4: Remove the tab-press interception and dropdown mount**

Open `apps/mobile/app/(app)/[workspace]/(tabs)/_layout.tsx`.

Remove the `useRef`/`TriggerRef` import and the dropdown import. Change:

```tsx
import { useRef } from "react";
import { Tabs } from "expo-router";
import { Image } from "expo-image";
import { Platform, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { useTranslation } from "react-i18next";
import type { TriggerRef } from "@rn-primitives/dropdown-menu";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";
import {
  useInboxUnreadCount,
  useChatUnreadSessionCount,
} from "@/lib/unread-counts";
import { MoreTabDropdownAnchor } from "@/components/nav/more-tab-dropdown";
```

to:

```tsx
import { Tabs } from "expo-router";
import { Image } from "expo-image";
import { Platform, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { useTranslation } from "react-i18next";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";
import {
  useInboxUnreadCount,
  useChatUnreadSessionCount,
} from "@/lib/unread-counts";
```

Remove the `moreTriggerRef` declaration. Change:

```tsx
  const chatBadge =
    chatUnread > 0 ? (chatUnread > 9 ? "9+" : String(chatUnread)) : undefined;

  // Imperative handle into the More tab's dropdown — listeners.tabPress
  // calls .open(); the @rn-primitives Trigger measures itself inside
  // open() so the popover anchors to MoreTabDropdownAnchor's rect.
  const moreTriggerRef = useRef<TriggerRef>(null);

  return (
```

to:

```tsx
  const chatBadge =
    chatUnread > 0 ? (chatUnread > 9 ? "9+" : String(chatUnread)) : undefined;

  return (
```

Remove the `listeners` prop from the More `Tabs.Screen` and the
`<MoreTabDropdownAnchor />` mount. Change:

```tsx
        <Tabs.Screen
          name="more"
          options={{
            title: tCommon("tabs.more"),
            tabBarIcon: ({ color, size }) => (
              <TabIcon
                sfSymbol="sf:ellipsis"
                ionicon="ellipsis-horizontal"
                color={color}
                size={size}
              />
            ),
          }}
          listeners={() => ({
            tabPress: (e) => {
              // Don't navigate to the (stub) /more screen — open the
              // dropdown popover instead. The trigger is invisible and
              // mounted in MoreTabDropdownAnchor below; ref.open() also
              // measures its rect so the popover anchors correctly.
              e.preventDefault();
              moreTriggerRef.current?.open();
            },
          })}
        />
      </Tabs>

      <MoreTabDropdownAnchor triggerRef={moreTriggerRef} />
    </View>
  );
}
```

to:

```tsx
        <Tabs.Screen
          name="more"
          options={{
            title: tCommon("tabs.more"),
            tabBarIcon: ({ color, size }) => (
              <TabIcon
                sfSymbol="sf:ellipsis"
                ionicon="ellipsis-horizontal"
                color={color}
                size={size}
              />
            ),
          }}
        />
      </Tabs>
    </View>
  );
}
```

Also update the file's header doc comment (lines 1-22), since it
describes the now-removed interception behavior. Change:

```tsx
/**
 * Bottom tab bar — JS `<Tabs>` from expo-router (react-navigation under the
 * hood). We tried NativeTabs first but its `canPreventDefault: false`
 * constraint makes "tap More → open something" impossible. JS Tabs
 * supports `listeners.tabPress + e.preventDefault()`, the canonical RN
 * pattern for tab-as-action.
 *
 * The "More" tab is **not a navigation target** — its press opens a
 * DropdownMenu popover anchored above the tab. The popover is rendered
 * by `<MoreTabDropdownAnchor />` as a sibling of `<Tabs>`, NOT as a
 * `tabBarButton` replacement: keeping the real tab button intact means
 * the icon + "More" label render identically to the other three tabs.
 * We just open the dropdown imperatively from `listeners.tabPress` via
 * the exposed `TriggerRef.open()`.
 *
 * The stub (tabs)/more.tsx file still exists only because expo-router
 * requires every Tabs.Screen to have a backing route file — the press
 * is preventDefault'd so we never actually navigate to it.
 *
 * Active / inactive tint colors are derived from the current colour
 * scheme via THEME so dark mode picks contrasting values automatically.
 */
```

to:

```tsx
/**
 * Bottom tab bar — JS `<Tabs>` from expo-router (react-navigation under the
 * hood). All four tabs, including More, are plain navigation targets;
 * More pushes to `(tabs)/more.tsx`, a real page (previously this tab
 * intercepted tabPress to open a dropdown popover instead — see git
 * history if you need that shape again).
 *
 * Active / inactive tint colors are derived from the current colour
 * scheme via THEME so dark mode picks contrasting values automatically.
 */
```

- [ ] **Step 5: Delete the dropdown component**

```bash
rm apps/mobile/components/nav/more-tab-dropdown.tsx
```

- [ ] **Step 6: Verify typecheck, lint, and tests**

```bash
pnpm --filter @multica/mobile typecheck
pnpm --filter @multica/mobile lint
pnpm --filter @multica/mobile test
```

Expected: all three `PASS`. Typecheck will catch any leftover reference
to the deleted `more-tab-dropdown.tsx` or `TriggerRef` import.

- [ ] **Step 7: Manual verification**

On a running build:
1. Tap the More tab. Confirm it navigates to a real page (no popover
   animation) titled "More" (or "更多" in Chinese).
2. Tap the user row → lands on Settings.
3. Tap the workspace row (if you have more than one workspace) →
   switch-workspace sheet opens; on a single-workspace account, confirm
   the row renders with no chevron and doesn't navigate on tap.
4. Tap Pinned, Issues, Projects → each lands on its existing screen.
5. Switch language to 简体中文, repeat steps 1-4, confirm all text is
   Chinese.

- [ ] **Step 8: Commit**

```bash
git add apps/mobile/locales apps/mobile/app/"(app)"/"[workspace]"/"(tabs)"/more.tsx apps/mobile/app/"(app)"/"[workspace]"/"(tabs)"/_layout.tsx
git rm apps/mobile/components/nav/more-tab-dropdown.tsx
git commit -m "feat(mobile): convert More tab from dropdown to a real page

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```
