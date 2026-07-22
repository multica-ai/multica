# Mobile Android Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `apps/mobile` build and launch on a connected Android device via `expo run:android`, with every existing interaction — including the 8 screens/hooks that currently call `ActionSheetIOS` — working without crashing or silently no-opping.

**Architecture:** Add an `android` platform block to Expo's config and matching `package.json` scripts (mirroring the existing `ios` ones), ignore the native project directories Expo generates, then replace the iOS-only `ActionSheetIOS` API with the cross-platform `@expo/react-native-action-sheet` library at all 7 call sites across 6 files. No business logic changes — this is platform-config plus a mechanical API swap.

**Tech Stack:** Expo SDK 55, React Native 0.82, Expo Router 55, TypeScript strict, `@expo/react-native-action-sheet` (new dependency, pinned `4.1.1`).

## Global Constraints

- Android `package` names: prod `ai.multica.mobile`, staging `ai.multica.mobile.staging`, dev `ai.multica.mobile.dev` — hardcoded, no env-var override (unlike iOS's `bundleIdentifier`, Android has no local-signing ownership restriction).
- No `android.adaptiveIcon` config this round — accept Expo's default single-icon fallback.
- No Play Store signing, no EAS Android build profile, no bottom-sheet/formSheet visual parity work — out of scope for this plan (tracked as a separate follow-up spec).
- `@expo/react-native-action-sheet` pinned at `4.1.1` (confirmed via `pnpm view @expo/react-native-action-sheet dist-tags` — `latest` is `4.1.1`; do not install `next`/`4.0.0-rc.1`).
- Every new source path must be verified with `git ls-files` after commit — this repo has a documented history (`apps/mobile/CLAUDE.md` Lesson #2) of mobile source silently matching a root `.gitignore` rule.
- All commands below run with `apps/mobile` as the working directory unless a `pnpm --filter @multica/mobile` form is shown (which can run from the repo root).

---

## Prerequisite (manual, local-only, not committed)

`ANDROID_HOME` is unset in the shell even though the Android SDK is
installed at `~/Library/Android/sdk` (`adb` only works today because
`platform-tools` happens to already be on `PATH`). `expo run:android`
needs `ANDROID_HOME` to find the SDK and toolchain.

**Before starting Task 4 (the first task that runs `expo run:android`),
confirm with the user and then add to `~/.zshrc` (or `~/.zprofile`):**

```bash
export ANDROID_HOME="$HOME/Library/Android/sdk"
export PATH="$PATH:$ANDROID_HOME/platform-tools:$ANDROID_HOME/emulator"
```

Then open a new shell (or `source ~/.zshrc`) and confirm:

```bash
echo $ANDROID_HOME
# Expected: /Users/<you>/Library/Android/sdk
adb devices
# Expected: at least one line ending in "device" (not "unauthorized"/"offline")
```

This is a one-time local machine change, not a repo change — do not add
it to any committed file.

---

### Task 1: Android platform configuration (app.config.ts, package.json, .gitignore)

**Files:**
- Modify: `apps/mobile/app.config.ts`
- Modify: `apps/mobile/package.json`
- Modify: `/Users/zongkelong/workspace/multica/.gitignore`

**Interfaces:**
- Produces: `android` scripts (`android`, `android:staging`, `android:prod`) runnable via `pnpm --filter @multica/mobile <script>`; resolved Expo config exposes `android.package` for each `APP_ENV`.

- [ ] **Step 1: Add the `android` block to `app.config.ts`**

Open `apps/mobile/app.config.ts`. It currently ends its returned object with an `ios` block followed by `plugins` and `extra`. Add a sibling `android` block immediately after the `ios` block (after its closing `},` around line 46, before `plugins: [`):

```ts
    android: {
      package: isProd
        ? "ai.multica.mobile"
        : isStaging
          ? "ai.multica.mobile.staging"
          : "ai.multica.mobile.dev",
    },
```

No `adaptiveIcon` key — Expo falls back to the top-level `icon` field
(same file, `icon: "./assets/icon.png"`), matching current iOS behavior.

- [ ] **Step 2: Verify the config resolves correctly for Android**

Run:

```bash
pnpm --filter @multica/mobile exec expo config --type public --platform android --json | grep -A1 '"package"'
```

Expected output includes:

```
"package": "ai.multica.mobile.dev",
```

(dev is the default `APP_ENV` when unset, matching the existing iOS
default of `"Multica (Dev)"` / `ai.multica.mobile.dev`).

- [ ] **Step 3: Add `android` scripts to `package.json`**

Open `apps/mobile/package.json`. Immediately after the existing
`"ios:device:prod:release"` script line, add:

```json
    "android": "expo run:android",
    "android:staging": "dotenv -e .env.staging -- cross-env APP_ENV=staging expo run:android",
    "android:prod": "dotenv -e .env.production -- cross-env APP_ENV=production expo run:android",
```

(No `android:device*` variants — unlike iOS, which needs a separate
script to target a physical device vs. simulator, `expo run:android`
targets whatever device/emulator is connected or selected interactively;
there's no separate code path to script for.)

- [ ] **Step 4: Add native-directory ignore rules to the root `.gitignore`**

Open `/Users/zongkelong/workspace/multica/.gitignore`. Add a new section
at the end of the file:

```
# Expo continuous native generation (regenerated by `expo prebuild` / `expo run:*`)
apps/mobile/android/
apps/mobile/ios/
```

- [ ] **Step 5: Confirm `package.json` is valid JSON and scripts are wired**

```bash
pnpm --filter @multica/mobile run android --help 2>&1 | head -5
```

Expected: no JSON parse error; the command starts resolving `expo
run:android`'s own help/usage output (it may then fail because no
Android target is built yet — that's fine, this step only confirms the
script is registered and invokable, not that the build succeeds).

- [ ] **Step 6: Commit**

```bash
git add apps/mobile/app.config.ts apps/mobile/package.json .gitignore
git commit -m "feat(mobile): add Android platform config and scripts

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

---

### Task 2: Introduce `@expo/react-native-action-sheet` and wire the provider

**Files:**
- Modify: `apps/mobile/package.json` (dependency)
- Modify: `apps/mobile/app/_layout.tsx`
- Modify: `apps/mobile/CLAUDE.md`

**Interfaces:**
- Consumes: nothing new from Task 1.
- Produces: `<ActionSheetProvider>` mounted above the app's `<Stack>`, so `useActionSheet()` (from `@expo/react-native-action-sheet`) is callable from any screen/hook in Task 3.

- [ ] **Step 1: Install the pinned version**

```bash
pnpm add @expo/react-native-action-sheet@4.1.1 --filter @multica/mobile
```

- [ ] **Step 2: Verify typecheck still passes with the new dependency present but unused**

```bash
pnpm --filter @multica/mobile typecheck
```

Expected: `PASS` (no errors) — confirms the package installed cleanly
and its types resolve.

- [ ] **Step 3: Wrap the root layout in `ActionSheetProvider`**

Open `apps/mobile/app/_layout.tsx`. Add the import alongside the other
provider imports (after the `PortalHost` import, line 11):

```ts
import { ActionSheetProvider } from "@expo/react-native-action-sheet";
```

Then wrap the existing `<GestureHandlerRootView>` subtree. Change:

```tsx
    <GestureHandlerRootView style={{ flex: 1 }}>
      <SafeAreaProvider>
```

to:

```tsx
    <GestureHandlerRootView style={{ flex: 1 }}>
      <ActionSheetProvider>
        <SafeAreaProvider>
```

and close it at the matching end of the tree. Change:

```tsx
        </SafeAreaProvider>
      </GestureHandlerRootView>
```

to:

```tsx
        </SafeAreaProvider>
      </ActionSheetProvider>
    </GestureHandlerRootView>
```

`ActionSheetProvider` must sit above every screen that will call
`useActionSheet()` in Task 3, and below `GestureHandlerRootView` (the
library's Android sheet implementation renders through the gesture
handler root, same requirement as other overlay providers already in
this tree).

- [ ] **Step 4: Verify typecheck and lint pass**

```bash
pnpm --filter @multica/mobile typecheck
pnpm --filter @multica/mobile lint
```

Expected: both `PASS`.

- [ ] **Step 5: Add the new dependency to the mobile tech-stack baseline doc**

Open `apps/mobile/CLAUDE.md`. In the "Tech-stack baseline" section,
immediately after the `expo-secure-store` bullet, add:

```markdown
- **@expo/react-native-action-sheet** — cross-platform action sheet (iOS
  native-styled sheet + Android Material bottom drawer). Replaces direct
  `ActionSheetIOS` calls now that mobile targets both platforms; every
  call site uses the `useActionSheet()` hook instead of the static
  `ActionSheetIOS.showActionSheetWithOptions` API.
```

- [ ] **Step 6: Commit**

```bash
git add apps/mobile/package.json apps/mobile/pnpm-lock.yaml apps/mobile/app/_layout.tsx apps/mobile/CLAUDE.md
git commit -m "feat(mobile): add ActionSheetProvider for cross-platform action sheets

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

(If the repo uses a single root `pnpm-lock.yaml` rather than a per-app
one, `git add pnpm-lock.yaml` from the repo root instead — check
`git status` output before committing to add the actual changed lockfile
path.)

---

### Task 3: Migrate all `ActionSheetIOS` call sites to `useActionSheet()`

**Files:**
- Modify: `apps/mobile/app/(app)/[workspace]/(tabs)/inbox.tsx`
- Modify: `apps/mobile/app/(app)/[workspace]/issue/[id].tsx`
- Modify: `apps/mobile/app/(app)/[workspace]/project/[id].tsx`
- Modify: `apps/mobile/app/(app)/[workspace]/more/settings/profile.tsx`
- Modify: `apps/mobile/components/chat/message-long-press.tsx`
- Modify: `apps/mobile/components/issue/comment-context-menu.tsx`
- Modify: `apps/mobile/components/chat/chat-message-list.tsx` (comment only)
- Modify: `apps/mobile/components/issue/comment-card.tsx` (comment only)

**Interfaces:**
- Consumes: `ActionSheetProvider` mounted in Task 2, so `useActionSheet()` is callable anywhere in this tree.
- Produces: zero remaining `ActionSheetIOS` imports or calls in `apps/mobile` (grep-verified in Step 9).

There are **7 call sites across 6 files** (not 8 — two of the originally
suspected files only *mention* `ActionSheetIOS` in a comment; they
consume the hooks defined in `message-long-press.tsx` /
`comment-context-menu.tsx` and need no functional change, only a comment
fix).

- [ ] **Step 1: `inbox.tsx` — top-level screen component, single call**

Open `apps/mobile/app/(app)/[workspace]/(tabs)/inbox.tsx`.

Change the import (line 1-7): remove `ActionSheetIOS` from the
`react-native` import and add the hook import:

```ts
import { Alert, FlatList, View } from "react-native";
import { useActionSheet } from "@expo/react-native-action-sheet";
```

Inside `export default function Inbox()`, call the hook at the top of
the component body (right after the `useColorScheme()` line):

```ts
  const { showActionSheetWithOptions } = useActionSheet();
```

In `onPressMenu`, change:

```ts
    ActionSheetIOS.showActionSheetWithOptions(
```

to:

```ts
    showActionSheetWithOptions(
```

(the rest of the call — options object and callback — is unchanged).

- [ ] **Step 2: `issue/[id].tsx` — top-level screen component, single call**

Open `apps/mobile/app/(app)/[workspace]/issue/[id].tsx`.

Change the import (lines 14-20): remove `ActionSheetIOS` from the
`react-native` import and add:

```ts
import { ActivityIndicator, Alert, Linking, View } from "react-native";
import { useActionSheet } from "@expo/react-native-action-sheet";
```

Inside `export default function IssueDetail()`, call the hook near the
top (right after `const qc = useQueryClient();`):

```ts
  const { showActionSheetWithOptions } = useActionSheet();
```

In `onPressMore`, change:

```ts
    ActionSheetIOS.showActionSheetWithOptions(
```

to:

```ts
    showActionSheetWithOptions(
```

and add `showActionSheetWithOptions` to the `useCallback` dependency
array on the line after the callback body (currently `[issue, wsSlug,
deleteIssue, isPinned, createPin, deletePin]` → `[issue, wsSlug,
deleteIssue, isPinned, createPin, deletePin, showActionSheetWithOptions]`).

- [ ] **Step 3: `project/[id].tsx` — top-level screen component, single call**

Open `apps/mobile/app/(app)/[workspace]/project/[id].tsx`.

Change the import (lines 17-25): remove `ActionSheetIOS` and add:

```ts
import {
  ActivityIndicator,
  Alert,
  Linking,
  RefreshControl,
  ScrollView,
  View,
} from "react-native";
import { useActionSheet } from "@expo/react-native-action-sheet";
```

Inside `export default function ProjectDetail()`, call the hook near the
top (right after `const qc = useQueryClient();`):

```ts
  const { showActionSheetWithOptions } = useActionSheet();
```

In `onPressMore`, change:

```ts
    ActionSheetIOS.showActionSheetWithOptions(
```

to:

```ts
    showActionSheetWithOptions(
```

- [ ] **Step 4: `profile.tsx` — top-level screen component, single call**

Open `apps/mobile/app/(app)/[workspace]/more/settings/profile.tsx`.

Change the import (lines 14-21): remove `ActionSheetIOS` and add:

```ts
import {
  Alert,
  ActivityIndicator,
  Pressable,
  ScrollView,
  View,
} from "react-native";
import { useActionSheet } from "@expo/react-native-action-sheet";
```

Inside `export default function ProfileSettingsScreen()`, call the hook
near the top (right after `const [uploading, setUploading] =
useState(false);`):

```ts
  const { showActionSheetWithOptions } = useActionSheet();
```

In `handleAvatarPick`, change:

```ts
    ActionSheetIOS.showActionSheetWithOptions(
```

to:

```ts
    showActionSheetWithOptions(
```

- [ ] **Step 5: `message-long-press.tsx` — custom hook, single call**

Open `apps/mobile/components/chat/message-long-press.tsx`.

This defines a **custom hook** (`useChatMessageLongPress`), not a
component — calling `useActionSheet()` here is still legal because
hooks may call other hooks, and this hook is itself only ever called
from inside a component's render body (`chat-message-list.tsx`).

Change the import (line 19):

```ts
import { useActionSheet } from "@expo/react-native-action-sheet";
```

Update the file header comment (lines 1-17) to stop describing this as
iOS-only:

```ts
/**
 * Long-press handler for a chat message bubble. Exposes `onLongPress`
 * (drives a cross-platform action sheet) and `isPressed` (drives the
 * caller's highlight ring while the sheet is on screen).
 *
 * Uses `@expo/react-native-action-sheet` per apps/mobile/CLAUDE.md
 * §Tech-stack baseline — native-styled sheet on iOS, Material bottom
 * drawer on Android. Zero custom layout, zero animation, zero overflow
 * math.
 *
 * Item set (v1, conditional):
 *   Copy · Select Text · Cancel
 *
 * Mirrors `useCommentLongPress` in `components/issue/comment-context-
 * menu.tsx` — kept as a sibling rather than a shared primitive because
 * we have only 2 callers (chat + comments). Below the "3 callers + no
 * native alternative" threshold in apps/mobile/CLAUDE.md.
 */
```

Inside `useChatMessageLongPress`, add the hook call at the top of the
function body (right after `const [isPressed, setIsPressed] =
useState(false);`):

```ts
  const { showActionSheetWithOptions } = useActionSheet();
```

Change:

```ts
    ActionSheetIOS.showActionSheetWithOptions(
```

to:

```ts
    showActionSheetWithOptions(
```

and add `showActionSheetWithOptions` to the `useCallback` dependency
array (currently `[message]` → `[message, showActionSheetWithOptions]`).

- [ ] **Step 6: `comment-context-menu.tsx` — custom hook with a nested plain-function call, two calls**

Open `apps/mobile/components/issue/comment-context-menu.tsx`. This file
has **two** `ActionSheetIOS.showActionSheetWithOptions` calls: one
directly inside the `useCommentLongPress` hook, and one inside
`presentReactSheet` — a **plain helper function**, not a hook, called
from inside `useCommentLongPress`'s completion callback.

`useActionSheet()` is a React hook and can only be called from a
component or another hook's body — it **cannot** be called inside
`presentReactSheet` directly. Instead, pass the
`showActionSheetWithOptions` function down as a parameter.

Change the import (line 21):

```ts
import { Alert } from "react-native";
import { useActionSheet } from "@expo/react-native-action-sheet";
```

Update the file header comment (lines 1-19) to stop describing this as
iOS-only:

```ts
/**
 * Long-press handler for a comment bubble. Exposes `onLongPress` (drives a
 * cross-platform action sheet) and `isPressed` (drives the caller's
 * highlight ring while the sheet is on screen).
 *
 * Uses `@expo/react-native-action-sheet` per apps/mobile/CLAUDE.md
 * §Tech-stack baseline — native-styled sheet on iOS, Material bottom
 * drawer on Android. Zero custom layout, zero animation, zero overflow
 * math.
 *
 * Item set (conditional, mirrors web's comment context menu):
 *   Reply (stub) · React… (opens nested sheet) · Copy · Select Text ·
 *   Copy Link · Resolve/Unresolve Thread (root only) · Delete (own only) ·
 *   Cancel
 *
 * The nested React… sheet (5 quick emojis + More reactions… + Cancel) is
 * fired from INSIDE the outer sheet's completion callback rather than
 * inline, because a second sheet cannot be presented while the first is
 * still dismissing — the callback runs after dismissal completes.
 */
```

Inside `useCommentLongPress`, add the hook call at the top of the
function body (right after `const { getName } = useActorLookup();`):

```ts
  const { showActionSheetWithOptions } = useActionSheet();
```

Change the first call site:

```ts
    ActionSheetIOS.showActionSheetWithOptions(
```

to:

```ts
    showActionSheetWithOptions(
```

and add `showActionSheetWithOptions` to the `useCallback` dependency
array (currently `[entry, issueId, issueIdentifier, userId, wsSlug,
toggleReaction, deleteComment, resolveComment]` → append
`showActionSheetWithOptions`).

In the `"react"` case inside that callback, thread the function through
to `presentReactSheet`. Change:

```ts
          case "react":
            // Present the nested React sheet from inside this completion
            // callback — see file header for why.
            presentReactSheet({
              entry,
              reactions,
              userId,
              wsSlug,
              issueId,
              toggle: (emoji, existing) =>
                toggleReaction.mutate({
                  commentId: entry.id,
                  emoji,
                  existing,
                }),
            });
            return;
```

to:

```ts
          case "react":
            // Present the nested React sheet from inside this completion
            // callback — see file header for why.
            presentReactSheet({
              entry,
              reactions,
              userId,
              wsSlug,
              issueId,
              showActionSheetWithOptions,
              toggle: (emoji, existing) =>
                toggleReaction.mutate({
                  commentId: entry.id,
                  emoji,
                  existing,
                }),
            });
            return;
```

Update `presentReactSheet`'s signature and body. Change:

```ts
function presentReactSheet(args: {
  entry: TimelineEntry;
  reactions: Reaction[];
  userId: string | undefined;
  wsSlug: string | null;
  issueId: string;
  toggle: (emoji: string, existing: Reaction | undefined) => void;
}) {
  const { entry, reactions, userId, wsSlug, issueId, toggle } = args;
  const emojis = QUICK_EMOJIS.slice(0, QUICK_ROW_SIZE);
  const options = [...emojis, "More reactions…", "Cancel"];
  const cancelButtonIndex = options.length - 1;

  ActionSheetIOS.showActionSheetWithOptions(
```

to:

```ts
function presentReactSheet(args: {
  entry: TimelineEntry;
  reactions: Reaction[];
  userId: string | undefined;
  wsSlug: string | null;
  issueId: string;
  showActionSheetWithOptions: ReturnType<
    typeof useActionSheet
  >["showActionSheetWithOptions"];
  toggle: (emoji: string, existing: Reaction | undefined) => void;
}) {
  const {
    entry,
    reactions,
    userId,
    wsSlug,
    issueId,
    showActionSheetWithOptions,
    toggle,
  } = args;
  const emojis = QUICK_EMOJIS.slice(0, QUICK_ROW_SIZE);
  const options = [...emojis, "More reactions…", "Cancel"];
  const cancelButtonIndex = options.length - 1;

  showActionSheetWithOptions(
```

- [ ] **Step 7: Fix the two stale comment-only mentions**

Open `apps/mobile/components/chat/chat-message-list.tsx`. On line 22,
change:

```ts
 * `ActionSheetIOS` (Copy / Select Text / Cancel). While the sheet is on
```

to:

```ts
 * a cross-platform action sheet (Copy / Select Text / Cancel). While the sheet is on
```

Open `apps/mobile/components/issue/comment-card.tsx`. On line 13,
change:

```ts
 * `ActionSheetIOS` with the comment's actions (Reply, React…, Copy,
```

to:

```ts
 * a cross-platform action sheet with the comment's actions (Reply, React…, Copy,
```

And on line 464, change:

```ts
  // When NOT selecting: long-press fires the native ActionSheetIOS via
```

to:

```ts
  // When NOT selecting: long-press fires the cross-platform action sheet via
```

- [ ] **Step 8: Run typecheck and lint**

```bash
pnpm --filter @multica/mobile typecheck
pnpm --filter @multica/mobile lint
```

Expected: both `PASS`. Typecheck is the meaningful correctness signal
here — the `presentReactSheet` parameter type change and the
`useCallback` dependency arrays are exactly the kind of mistake
TypeScript strict mode catches (mismatched argument shape, unused/missing
deps).

- [ ] **Step 9: Grep-verify no `ActionSheetIOS` references remain**

```bash
grep -rn "ActionSheetIOS" apps/mobile --include="*.ts" --include="*.tsx"
```

Expected: no output (zero matches). If anything remains, it was missed
in one of Steps 1-7 above.

- [ ] **Step 10: Run the existing mobile test suite**

```bash
pnpm --filter @multica/mobile test
```

Expected: `PASS` — this plan doesn't add new test files (none of the 6
touched files have existing test coverage to extend; the spec's
verification approach for this change is typecheck/lint plus the
manual device pass in Task 4, not new unit tests for previously-untested
screens).

- [ ] **Step 11: Commit**

```bash
git add apps/mobile/app/"(app)"/"[workspace]"/"(tabs)"/inbox.tsx \
        "apps/mobile/app/(app)/[workspace]/issue/[id].tsx" \
        "apps/mobile/app/(app)/[workspace]/project/[id].tsx" \
        "apps/mobile/app/(app)/[workspace]/more/settings/profile.tsx" \
        apps/mobile/components/chat/message-long-press.tsx \
        apps/mobile/components/issue/comment-context-menu.tsx \
        apps/mobile/components/chat/chat-message-list.tsx \
        apps/mobile/components/issue/comment-card.tsx
git commit -m "refactor(mobile): migrate ActionSheetIOS call sites to useActionSheet

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

(Use plain `git add <path>` with normal shell quoting for the
bracket/paren directory names — the paths above are shown
quoted for clarity; adjust quoting to match your shell.)

---

### Task 4: Build, launch, and verify on a connected Android device

**Files:** none (verification only).

**Interfaces:**
- Consumes: everything from Tasks 1-3.
- Produces: a verified working Android build — the deliverable of this whole plan.

- [ ] **Step 1: Confirm the prerequisite `ANDROID_HOME` setup is done**

```bash
echo $ANDROID_HOME
adb devices
```

Expected: `ANDROID_HOME` prints the SDK path; `adb devices` lists at
least one device/emulator in `device` state. If not, stop and complete
the Prerequisite section above first.

- [ ] **Step 2: Run the full check suite**

```bash
pnpm --filter @multica/mobile typecheck
pnpm --filter @multica/mobile lint
pnpm --filter @multica/mobile test
```

Expected: all three `PASS`.

- [ ] **Step 3: Build and launch on the connected device**

```bash
pnpm --filter @multica/mobile android
```

Expected: Gradle build completes, the app installs, and launches
automatically on the connected device, reaching the login screen (or
workspace home if already authenticated on that device).

- [ ] **Step 4: Manually verify all 7 action-sheet entry points**

On the running Android build, exercise each of the following and
confirm a sheet appears and the selected action completes (no crash, no
silent no-op):

1. Inbox tab → "…" menu (top-right) → any of Mark all read / Archive
   all read / Archive completed / Archive all.
2. Open any issue → "…" menu → Pin/Unpin, Edit details, Delete issue.
3. Open any project → "…" menu → Pin/Unpin, Edit details, Delete.
4. Settings → Profile → tap avatar → Take Photo / Choose from Library /
   Remove Photo.
5. Any chat message → long-press → Copy / Select Text.
6. Any issue comment → long-press → Reply / Copy / Select Text /
   Resolve / Delete (as applicable).
7. From the comment long-press sheet, tap "React…" → confirm the nested
   emoji sheet opens correctly (this is the `presentReactSheet` path
   fixed in Task 3 Step 6).

- [ ] **Step 5: Confirm no generated native files leaked into git**

```bash
git status --porcelain apps/mobile/android apps/mobile/ios
```

Expected: no output — both directories exist on disk (Gradle/Xcode
projects generated by the build) but are fully ignored per Task 1's
`.gitignore` change.

- [ ] **Step 6: Final commit (if Step 4 required any fixes)**

If manual verification in Step 4 surfaced a bug, fix it, re-run Steps
2-4, then commit the fix separately with a `fix(mobile):` prefix
describing exactly what broke. If Step 4 passed cleanly with no changes
needed, there is nothing to commit for this task — the plan is done as
of Task 3's commit.
