# Mobile i18n Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up `apps/mobile`'s own i18n runtime (library, device-locale detection, persisted override, namespace file structure) and translate one pilot cluster — the Settings screens (`settings.tsx`, `profile.tsx`, `notifications.tsx`) — into English and Simplified Chinese.

**Architecture:** `i18next` + `react-i18next`, initialized once at app startup from `apps/mobile/lib/i18n/index.ts`. Device locale detected via `expo-localization`; user override persisted to `expo-secure-store` under key `language-preference`, mirroring the existing `theme-preference` pattern in `apps/mobile/lib/use-color-scheme.ts` exactly. Translation resources live in `apps/mobile/locales/<lang>/<namespace>.json`, mirroring `packages/views/locales/` in spirit only — no shared code or imports between mobile and web's i18n (per the root `CLAUDE.md` "Mobile is independent" rule).

**Tech Stack:** `i18next@26.3.4`, `react-i18next@17.0.8`, `expo-localization` (installed via `expo install`, resolves to the SDK 55-aligned version).

## Global Constraints

- Two languages only this round: `en`, `zh-Hans`. `ko`/`ja` are out of scope (see spec's Follow-up section).
- Chinese copy follows `apps/docs/content/docs/developers/conventions.mdx` exactly: entities (`issue`/`skill`/`task`) stay lowercase English in UI strings; concepts (Workspace → 工作区, Settings → 设置, etc.) are fully translated; full-width punctuation; brands (Multica) never translated.
- `expo-localization` MUST be installed via `pnpm exec expo install expo-localization`, not `pnpm add` — per `apps/mobile/CLAUDE.md` Lesson #1, `pnpm add` installs npm `latest` (currently `57.0.0`), which outpaces this project's Expo SDK 55.
- `i18next` and `react-i18next` are not Expo-managed packages — pin the exact versions `26.3.4` and `17.0.8` (verified compatible: `react-i18next@17.0.8` requires `i18next >= 26.2.0` and `react >= 16.8.0`, both satisfied).
- Persisted preference key is exactly `language-preference` (parallel to the existing `theme-preference` key) with values `"en" | "zh-Hans" | "system"`.
- No new page-level test files for the 3 pilot screens (none have existing coverage) — verification is typecheck + the new `parity.test.ts` + manual bilingual pass, matching how the Android foundation plan handled the same situation for its `ActionSheetIOS` migration.
- `apps/mobile/vitest.config.ts` only picks up `lib/**/*.test.ts` (intentionally scoped — see the file's own header comment). The parity test MUST live under `apps/mobile/lib/i18n/parity.test.ts`, not `apps/mobile/locales/`, so it's discovered without editing `vitest.config.ts`.

---

### Task 1: i18n runtime + `common` namespace + parity test

**Files:**
- Modify: `apps/mobile/package.json` (add 3 dependencies)
- Create: `apps/mobile/locales/en/common.json`
- Create: `apps/mobile/locales/zh-Hans/common.json`
- Create: `apps/mobile/locales/index.ts`
- Create: `apps/mobile/lib/i18n/index.ts`
- Create: `apps/mobile/lib/i18n/use-locale.ts`
- Create: `apps/mobile/lib/i18n/parity.test.ts`
- Modify: `apps/mobile/app/_layout.tsx`
- Modify: `apps/mobile/CLAUDE.md` (Tech-stack baseline)

**Interfaces:**
- Produces: `RESOURCES: Record<"en" | "zh-Hans", Record<string, Record<string, unknown>>>` from `apps/mobile/locales/index.ts`; `SupportedLocale = "en" | "zh-Hans"` type; default export `i18n` (the initialized i18next instance) and named export `detectDeviceLocale(): SupportedLocale` from `apps/mobile/lib/i18n/index.ts`; `useLocale(): { preference: "en" | "zh-Hans" | "system"; setPreference: (p: "en" | "zh-Hans" | "system") => void }` from `apps/mobile/lib/i18n/use-locale.ts`. Later tasks (2-4) add namespace files to `locales/index.ts`'s `RESOURCES` map and call `useTranslation()` from `react-i18next` directly (no wrapper needed).

- [ ] **Step 1: Install dependencies**

```bash
pnpm exec expo install expo-localization
pnpm add i18next@26.3.4 react-i18next@17.0.8 --filter @multica/mobile
```

- [ ] **Step 2: Verify typecheck still passes with unused new dependencies**

```bash
pnpm --filter @multica/mobile typecheck
```

Expected: `PASS`.

- [ ] **Step 3: Create the `common` namespace JSON files**

Create `apps/mobile/locales/en/common.json`:

```json
{
  "cancel": "Cancel",
  "save": "Save",
  "sign_out": "Sign out"
}
```

Create `apps/mobile/locales/zh-Hans/common.json`:

```json
{
  "cancel": "取消",
  "save": "保存",
  "sign_out": "退出登录"
}
```

- [ ] **Step 4: Create the resources index**

Create `apps/mobile/locales/index.ts`:

```ts
import enCommon from "./en/common.json";
import zhHansCommon from "./zh-Hans/common.json";

export type SupportedLocale = "en" | "zh-Hans";

export const RESOURCES = {
  en: {
    common: enCommon,
  },
  "zh-Hans": {
    common: zhHansCommon,
  },
} satisfies Record<SupportedLocale, Record<string, Record<string, unknown>>>;
```

- [ ] **Step 5: Create the i18next runtime init**

Create `apps/mobile/lib/i18n/index.ts`:

```ts
/**
 * Mobile-owned i18next init. Independent from packages/core/i18n and
 * packages/views/locales — mobile owns its own i18n per the root
 * CLAUDE.md "Mobile is independent" rule. Only two languages so far:
 * en, zh-Hans. See apps/mobile/CLAUDE.md for the full language list.
 */
import i18next from "i18next";
import { initReactI18next } from "react-i18next";
import * as Localization from "expo-localization";
import { RESOURCES, type SupportedLocale } from "@/locales";

export function detectDeviceLocale(): SupportedLocale {
  const locales = Localization.getLocales();
  const languageCode = locales[0]?.languageCode ?? "en";
  return languageCode.startsWith("zh") ? "zh-Hans" : "en";
}

void i18next.use(initReactI18next).init({
  resources: RESOURCES,
  lng: detectDeviceLocale(),
  fallbackLng: "en",
  ns: Object.keys(RESOURCES.en),
  defaultNS: "common",
  interpolation: { escapeValue: false },
});

export default i18next;
export type { SupportedLocale };
```

- [ ] **Step 6: Verify the installed `expo-localization` API shape before relying on it**

`getLocales()`'s return shape is a stable, long-standing part of the
`expo-localization` API, but per `apps/mobile/CLAUDE.md` Lesson #1 don't
trust memory for library APIs — confirm against the installed package:

```bash
grep -A10 "getLocales" node_modules/expo-localization/build/Localization.types.d.ts
```

Expected: a `Locale` type with a `languageCode: string | null` field and
a `getLocales(): Locale[]` signature. If the installed version's shape
differs (field renamed, return type changed), adjust Step 5's
`detectDeviceLocale` to match the actual field name before proceeding —
do not silently keep code that doesn't match the installed types.

- [ ] **Step 7: Create the persisted-preference hook**

Create `apps/mobile/lib/i18n/use-locale.ts`:

```ts
/**
 * Wraps i18next's changeLanguage with persistence in expo-secure-store.
 * Mirrors apps/mobile/lib/use-color-scheme.ts's theme-preference pattern
 * exactly — same async-read-after-mount tradeoff (a kill-and-relaunch may
 * briefly show the device-detected language before the saved preference
 * applies; acceptable, see use-color-scheme.ts for the precedent).
 */
import { useEffect, useState } from "react";
import * as SecureStore from "expo-secure-store";
import i18n, { detectDeviceLocale } from "./index";

const STORAGE_KEY = "language-preference";

export type LanguagePreference = "en" | "zh-Hans" | "system";

export function useLocale() {
  const [preference, setPreferenceState] =
    useState<LanguagePreference>("system");

  useEffect(() => {
    let cancelled = false;
    SecureStore.getItemAsync(STORAGE_KEY)
      .then((saved) => {
        if (cancelled) return;
        if (saved === "en" || saved === "zh-Hans" || saved === "system") {
          setPreferenceState(saved);
          void i18n.changeLanguage(
            saved === "system" ? detectDeviceLocale() : saved,
          );
        }
      })
      .catch(() => {
        // Read failures are non-fatal; keep default 'system'.
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const setPreference = (p: LanguagePreference) => {
    setPreferenceState(p);
    void i18n.changeLanguage(p === "system" ? detectDeviceLocale() : p);
    void SecureStore.setItemAsync(STORAGE_KEY, p);
  };

  return { preference, setPreference };
}
```

- [ ] **Step 8: Write the parity test**

Create `apps/mobile/lib/i18n/parity.test.ts`:

```ts
import { readdirSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { RESOURCES } from "@/locales";

// Schema-level guard: every key in the EN bundle must have a counterpart
// in the zh-Hans bundle and vice-versa. Catches retrofit drift where a
// new EN key lands without its translation, which would silently fall
// back to the English string in production. Mirrors
// packages/views/locales/parity.test.ts (web's equivalent guard).
const LOCALES_DIR = resolve(
  dirname(fileURLToPath(import.meta.url)),
  "../../locales",
);

function jsonNamespacesIn(locale: string): string[] {
  return readdirSync(resolve(LOCALES_DIR, locale))
    .filter((name) => name.endsWith(".json"))
    .map((name) => name.replace(/\.json$/, ""))
    .sort();
}

type Json = Record<string, unknown>;

function flattenKeys(obj: unknown, prefix = ""): string[] {
  if (obj === null || typeof obj !== "object") return [prefix];
  const entries = Object.entries(obj as Json);
  if (entries.length === 0) return [];
  return entries.flatMap(([k, v]) =>
    flattenKeys(v, prefix ? `${prefix}.${k}` : k),
  );
}

function keySet(bundle: Record<string, unknown>): Set<string> {
  return new Set(flattenKeys(bundle));
}

const en = RESOURCES.en;
const zhHans = RESOURCES["zh-Hans"];

describe("mobile locale bundle parity", () => {
  it("registers every JSON file in RESOURCES (en)", () => {
    expect(Object.keys(en).sort()).toEqual(jsonNamespacesIn("en"));
  });

  it("declares the same namespaces in en and zh-Hans", () => {
    expect(Object.keys(zhHans).sort()).toEqual(Object.keys(en).sort());
  });

  it("registers every JSON file in RESOURCES (zh-Hans)", () => {
    expect(Object.keys(zhHans).sort()).toEqual(jsonNamespacesIn("zh-Hans"));
  });

  for (const ns of Object.keys(en)) {
    it(`${ns}: zh-Hans covers every en key`, () => {
      const enKeys = keySet(en[ns as keyof typeof en] ?? {});
      const zhKeys = keySet(zhHans[ns as keyof typeof zhHans] ?? {});
      const missing = [...enKeys].filter((k) => !zhKeys.has(k));
      expect(missing).toEqual([]);
    });

    it(`${ns}: en covers every zh-Hans key`, () => {
      const enKeys = keySet(en[ns as keyof typeof en] ?? {});
      const zhKeys = keySet(zhHans[ns as keyof typeof zhHans] ?? {});
      const extra = [...zhKeys].filter((k) => !enKeys.has(k));
      expect(extra).toEqual([]);
    });
  }
});
```

- [ ] **Step 9: Run the parity test**

```bash
pnpm --filter @multica/mobile test
```

Expected: `PASS` (the new `lib/i18n/parity.test.ts` suite passes alongside
the existing 3 test files).

- [ ] **Step 10: Wire i18n init into the app root**

Open `apps/mobile/app/_layout.tsx`. Add the import right after the
existing `prewarmHighlighter` import (it's a module-load side-effect
import, same pattern):

```ts
import "@/lib/i18n";
```

Add the `useLocale` import alongside the other hook imports:

```ts
import { useLocale } from "@/lib/i18n/use-locale";
```

Inside `RootLayout`, call the hook right after `useColorScheme()`:

```ts
  const { colorScheme, isDarkColorScheme } = useColorScheme();
  useLocale();
```

- [ ] **Step 11: Verify typecheck, lint, and tests all pass**

```bash
pnpm --filter @multica/mobile typecheck
pnpm --filter @multica/mobile lint
pnpm --filter @multica/mobile test
```

Expected: all three `PASS`.

- [ ] **Step 12: Add the three new dependencies to the mobile tech-stack baseline doc**

Open `apps/mobile/CLAUDE.md`. In the "Tech-stack baseline" section,
immediately after the `@expo/react-native-action-sheet` bullet, add:

```markdown
- **i18next** + **react-i18next** + **expo-localization** — mobile-owned
  i18n runtime (independent from `packages/core/i18n` /
  `packages/views/locales`). Two languages so far: `en`, `zh-Hans`.
  Resources live in `apps/mobile/locales/<lang>/<namespace>.json`;
  device-locale detection + persisted override live in
  `apps/mobile/lib/i18n/`. Chinese copy follows the glossary in
  `apps/docs/content/docs/developers/conventions.mdx`.
```

- [ ] **Step 13: Commit**

```bash
git add apps/mobile/package.json apps/mobile/pnpm-lock.yaml \
        apps/mobile/locales apps/mobile/lib/i18n \
        apps/mobile/app/_layout.tsx apps/mobile/CLAUDE.md
git commit -m "feat(mobile): add i18n runtime with en/zh-Hans common namespace

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

(Check `git status` first — if the repo uses a single root
`pnpm-lock.yaml` rather than a per-app one, add the actual changed
lockfile path instead.)

---

### Task 2: Migrate `settings.tsx` + add language switcher

**Files:**
- Modify: `apps/mobile/locales/en/settings.json` (create)
- Modify: `apps/mobile/locales/zh-Hans/settings.json` (create)
- Modify: `apps/mobile/locales/index.ts`
- Modify: `apps/mobile/app/(app)/[workspace]/more/settings.tsx`

**Interfaces:**
- Consumes: `useLocale()` from Task 1's `apps/mobile/lib/i18n/use-locale.ts`; `useTranslation` from `react-i18next`.
- Produces: `settings` namespace registered in `RESOURCES`, with `account`, `workspaces`, `appearance`, `language`, and `sign_out_message` keys — Tasks 3 and 4 add sibling `profile` and `notifications` sections to the same two JSON files.

- [ ] **Step 1: Create the `settings` namespace JSON files (account/workspaces/appearance/language sections)**

Create `apps/mobile/locales/en/settings.json`:

```json
{
  "account": {
    "title": "Account",
    "user_avatar_alt": "User avatar",
    "notifications_row": {
      "title": "Notifications",
      "subtitle": "Inbox and system alerts"
    }
  },
  "workspaces": {
    "title": "Workspaces",
    "error": "Failed to load workspaces"
  },
  "appearance": {
    "title": "Appearance",
    "theme": {
      "light": "Light",
      "dark": "Dark",
      "system": "System"
    }
  },
  "language": {
    "title": "Language",
    "options": {
      "english": "English",
      "chinese": "中文",
      "system": "System"
    }
  },
  "sign_out_message": "You'll need to sign in again to use Multica on this device."
}
```

Create `apps/mobile/locales/zh-Hans/settings.json`:

```json
{
  "account": {
    "title": "账号",
    "user_avatar_alt": "用户头像",
    "notifications_row": {
      "title": "通知",
      "subtitle": "收件箱与系统提醒"
    }
  },
  "workspaces": {
    "title": "工作区",
    "error": "加载工作区失败"
  },
  "appearance": {
    "title": "外观",
    "theme": {
      "light": "浅色",
      "dark": "深色",
      "system": "跟随系统"
    }
  },
  "language": {
    "title": "语言",
    "options": {
      "english": "English",
      "chinese": "中文",
      "system": "跟随系统"
    }
  },
  "sign_out_message": "退出后需要重新登录才能使用 Multica。"
}
```

(`language.options.english` / `.chinese` are the same string in both
locale files — language names in a language picker are shown in their
own script regardless of the current UI language, matching
`packages/views/locales/*/settings.json`'s `preferences.language.*`
precedent.)

- [ ] **Step 2: Register the namespace in the resources index**

Edit `apps/mobile/locales/index.ts`:

```ts
import enCommon from "./en/common.json";
import enSettings from "./en/settings.json";
import zhHansCommon from "./zh-Hans/common.json";
import zhHansSettings from "./zh-Hans/settings.json";

export type SupportedLocale = "en" | "zh-Hans";

export const RESOURCES = {
  en: {
    common: enCommon,
    settings: enSettings,
  },
  "zh-Hans": {
    common: zhHansCommon,
    settings: zhHansSettings,
  },
} satisfies Record<SupportedLocale, Record<string, Record<string, unknown>>>;
```

- [ ] **Step 2b: Run the parity test to confirm the new namespace is balanced**

```bash
pnpm --filter @multica/mobile test
```

Expected: `PASS`.

- [ ] **Step 3: Migrate `settings.tsx`**

Open `apps/mobile/app/(app)/[workspace]/more/settings.tsx`.

Add the import (after the `cn` import):

```ts
import { useTranslation } from "react-i18next";
import { useLocale, type LanguagePreference } from "@/lib/i18n/use-locale";
```

Delete the hardcoded `THEME_OPTIONS` constant (lines 35-39) — it moves
inline into the component body in the next step so its labels can use
`t()`.

Inside `export default function SettingsPage()`, right after the
existing `const { preference, setPreference, colorScheme } =
useColorScheme();` line, add:

```ts
  const { t } = useTranslation("settings");
  const { t: tCommon } = useTranslation("common");
  const { preference: langPreference, setPreference: setLangPreference } =
    useLocale();

  const themeOptions: Array<{ value: ThemePreference; label: string }> = [
    { value: "light", label: t("appearance.theme.light") },
    { value: "dark", label: t("appearance.theme.dark") },
    { value: "system", label: t("appearance.theme.system") },
  ];

  const languageOptions: Array<{ value: LanguagePreference; label: string }> = [
    { value: "en", label: t("language.options.english") },
    { value: "zh-Hans", label: t("language.options.chinese") },
    { value: "system", label: t("language.options.system") },
  ];
```

Replace the `onSignOut` function body's hardcoded strings:

```ts
  const onSignOut = () => {
    Alert.alert(
      tCommon("sign_out"),
      t("sign_out_message"),
      [
        { text: tCommon("cancel"), style: "cancel" },
        {
          text: tCommon("sign_out"),
          style: "destructive",
          onPress: async () => {
            await clearWorkspace();
            await logout();
          },
        },
      ],
    );
  };
```

Replace the JSX section titles and labels. Change:

```tsx
      <SectionGroup title="Account">
```
to:
```tsx
      <SectionGroup title={t("account.title")}>
```

Change the `NavRow` for the account avatar row — the `Avatar` `alt` prop:
```tsx
            <Avatar alt={user?.name ?? "User avatar"} className="size-10">
```
to:
```tsx
            <Avatar alt={user?.name ?? t("account.user_avatar_alt")} className="size-10">
```

Change the notifications `NavRow`:
```tsx
        <NavRow
          onPress={goNotifications}
          chevronColor={mutedFg}
          title="Notifications"
          subtitle="Inbox and system alerts"
        />
```
to:
```tsx
        <NavRow
          onPress={goNotifications}
          chevronColor={mutedFg}
          title={t("account.notifications_row.title")}
          subtitle={t("account.notifications_row.subtitle")}
        />
```

Change:
```tsx
      <SectionGroup title="Workspaces">
```
to:
```tsx
      <SectionGroup title={t("workspaces.title")}>
```

Change the workspaces error text:
```tsx
            <Text className="text-sm text-destructive">
              Failed to load workspaces
            </Text>
```
to:
```tsx
            <Text className="text-sm text-destructive">
              {t("workspaces.error")}
            </Text>
```

Change:
```tsx
      <SectionGroup title="Appearance">
```
to:
```tsx
      <SectionGroup title={t("appearance.title")}>
```

Change the theme `RadioGroup`'s `.map` source from `THEME_OPTIONS` to
`themeOptions` (the local variable defined above):

```tsx
          {themeOptions.map((opt, idx) => {
            const isLast = idx === themeOptions.length - 1;
```

Immediately after the closing `</SectionGroup>` of the Appearance
section (before the sign-out `<View className="pt-2">` block), add a
new language section:

```tsx
      <SectionGroup title={t("language.title")}>
        <RadioGroup
          value={langPreference}
          onValueChange={(v) => setLangPreference(v as LanguagePreference)}
          className="gap-0"
        >
          {languageOptions.map((opt, idx) => {
            const isLast = idx === languageOptions.length - 1;
            return (
              <View key={opt.value}>
                <Pressable
                  onPress={() => setLangPreference(opt.value)}
                  className="flex-row items-center px-4 py-3.5 active:bg-secondary gap-3"
                >
                  <RadioGroupItem value={opt.value} />
                  <Text className="flex-1 text-base font-medium text-foreground">
                    {opt.label}
                  </Text>
                </Pressable>
                {!isLast ? <Separator /> : null}
              </View>
            );
          })}
        </RadioGroup>
      </SectionGroup>

```

Finally, change the sign-out button text:
```tsx
        <Button variant="destructive" onPress={onSignOut}>
          <Text>Sign out</Text>
        </Button>
```
to:
```tsx
        <Button variant="destructive" onPress={onSignOut}>
          <Text>{tCommon("sign_out")}</Text>
        </Button>
```

- [ ] **Step 4: Verify typecheck and lint**

```bash
pnpm --filter @multica/mobile typecheck
pnpm --filter @multica/mobile lint
```

Expected: both `PASS`. Typecheck will catch a stale `THEME_OPTIONS`
reference if the Step 3 deletion/replacement was incomplete.

- [ ] **Step 5: Commit**

```bash
git add apps/mobile/locales apps/mobile/app/"(app)"/"[workspace]"/more/settings.tsx
git commit -m "feat(mobile): translate settings screen, add language switcher

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

---

### Task 3: Migrate `profile.tsx`

**Files:**
- Modify: `apps/mobile/locales/en/settings.json` (add `profile` section)
- Modify: `apps/mobile/locales/zh-Hans/settings.json` (add `profile` section)
- Modify: `apps/mobile/app/(app)/[workspace]/more/settings/profile.tsx`

**Interfaces:**
- Consumes: `useTranslation("settings")` pattern established in Task 2.
- Produces: `profile.*` keys under the existing `settings` namespace.

- [ ] **Step 1: Add the `profile` section to both settings.json files**

Edit `apps/mobile/locales/en/settings.json` — add a `"profile"` key as a
new top-level sibling of `"account"` / `"workspaces"` / `"appearance"` /
`"language"` / `"sign_out_message"` (add a trailing comma after
`"sign_out_message": "..."`  and insert this block):

```json
  "profile": {
    "your_avatar_alt": "Your avatar",
    "tap_to_change_photo": "Tap to change photo",
    "name_label": "Name",
    "name_placeholder": "Your name",
    "email_label": "Email",
    "email_hint": "Email is set at sign-up and can't be changed here.",
    "saving": "Saving…",
    "avatar_actions": {
      "take_photo": "Take Photo",
      "choose_library": "Choose from Library",
      "remove_photo": "Remove Photo"
    },
    "camera_permission": {
      "title": "Permission needed",
      "message": "Camera access is required to take a photo."
    },
    "image_too_large": {
      "title": "Image too large",
      "message": "Pick an image under 5 MB."
    },
    "upload_failed": {
      "title": "Upload failed",
      "message": "Could not upload avatar."
    },
    "remove_failed": {
      "title": "Remove failed",
      "message": "Could not remove avatar."
    },
    "save_failed": {
      "title": "Save failed",
      "message": "Could not update profile."
    }
  }
```

Edit `apps/mobile/locales/zh-Hans/settings.json` — same position, add:

```json
  "profile": {
    "your_avatar_alt": "你的头像",
    "tap_to_change_photo": "点击更换头像",
    "name_label": "姓名",
    "name_placeholder": "你的姓名",
    "email_label": "邮箱",
    "email_hint": "邮箱在注册时设置，此处无法修改。",
    "saving": "保存中…",
    "avatar_actions": {
      "take_photo": "拍照",
      "choose_library": "从相册选择",
      "remove_photo": "移除头像"
    },
    "camera_permission": {
      "title": "需要权限",
      "message": "拍照需要访问相机的权限。"
    },
    "image_too_large": {
      "title": "图片过大",
      "message": "请选择小于 5 MB 的图片。"
    },
    "upload_failed": {
      "title": "上传失败",
      "message": "头像上传失败。"
    },
    "remove_failed": {
      "title": "移除失败",
      "message": "头像移除失败。"
    },
    "save_failed": {
      "title": "保存失败",
      "message": "个人资料更新失败。"
    }
  }
```

- [ ] **Step 2: Run the parity test**

```bash
pnpm --filter @multica/mobile test
```

Expected: `PASS`.

- [ ] **Step 3: Migrate `profile.tsx`**

Open `apps/mobile/app/(app)/[workspace]/more/settings/profile.tsx`.

Add the import (after the `Text`/`Button`/etc. UI imports, before the
data imports):

```ts
import { useTranslation } from "react-i18next";
```

Inside `export default function ProfileSettingsScreen()`, right after
`const { showActionSheetWithOptions } = useActionSheet();`, add:

```ts
  const { t } = useTranslation("settings");
  const { t: tCommon } = useTranslation("common");
```

Change `handleAvatarPick`'s options array:
```ts
    const options = ["Take Photo", "Choose from Library", "Remove Photo", "Cancel"];
```
to:
```ts
    const options = [
      t("profile.avatar_actions.take_photo"),
      t("profile.avatar_actions.choose_library"),
      t("profile.avatar_actions.remove_photo"),
      tCommon("cancel"),
    ];
```

Change `pickFromCamera`'s alert:
```ts
      Alert.alert("Permission needed", "Camera access is required to take a photo.");
```
to:
```ts
      Alert.alert(
        t("profile.camera_permission.title"),
        t("profile.camera_permission.message"),
      );
```

Change `uploadAvatar`'s two alerts:
```ts
      Alert.alert("Image too large", "Pick an image under 5 MB.");
```
to:
```ts
      Alert.alert(
        t("profile.image_too_large.title"),
        t("profile.image_too_large.message"),
      );
```

and:
```ts
      Alert.alert(
        "Upload failed",
        err instanceof Error ? err.message : "Could not upload avatar.",
      );
```
to:
```ts
      Alert.alert(
        t("profile.upload_failed.title"),
        err instanceof Error ? err.message : t("profile.upload_failed.message"),
      );
```

Change `removeAvatar`'s alert:
```ts
      Alert.alert(
        "Remove failed",
        err instanceof Error ? err.message : "Could not remove avatar.",
      );
```
to:
```ts
      Alert.alert(
        t("profile.remove_failed.title"),
        err instanceof Error ? err.message : t("profile.remove_failed.message"),
      );
```

Change `handleSave`'s alert:
```ts
      Alert.alert(
        "Save failed",
        err instanceof Error ? err.message : "Could not update profile.",
      );
```
to:
```ts
      Alert.alert(
        t("profile.save_failed.title"),
        err instanceof Error ? err.message : t("profile.save_failed.message"),
      );
```

Change the avatar alt text:
```tsx
          <Avatar alt={user?.name ?? "Your avatar"} className="size-24">
```
to:
```tsx
          <Avatar alt={user?.name ?? t("profile.your_avatar_alt")} className="size-24">
```

Change the "Tap to change photo" text:
```tsx
          <Text className="text-xs text-muted-foreground">
            Tap to change photo
          </Text>
```
to:
```tsx
          <Text className="text-xs text-muted-foreground">
            {t("profile.tap_to_change_photo")}
          </Text>
```

Change the Name field:
```tsx
          <Text className="text-xs text-muted-foreground mb-1.5">Name</Text>
          <TextField
            value={name}
            onChangeText={setName}
            placeholder="Your name"
```
to:
```tsx
          <Text className="text-xs text-muted-foreground mb-1.5">{t("profile.name_label")}</Text>
          <TextField
            value={name}
            onChangeText={setName}
            placeholder={t("profile.name_placeholder")}
```

Change the Email field label and hint:
```tsx
          <Text className="text-xs text-muted-foreground mb-1.5">Email</Text>
```
to:
```tsx
          <Text className="text-xs text-muted-foreground mb-1.5">{t("profile.email_label")}</Text>
```

and:
```tsx
          <Text className="text-xs text-muted-foreground mt-1.5">
            Email is set at sign-up and can&apos;t be changed here.
          </Text>
```
to:
```tsx
          <Text className="text-xs text-muted-foreground mt-1.5">
            {t("profile.email_hint")}
          </Text>
```

Change the Save button:
```tsx
      <Button onPress={handleSave} disabled={!dirty || saving}>
        <Text>{saving ? "Saving…" : "Save"}</Text>
      </Button>
```
to:
```tsx
      <Button onPress={handleSave} disabled={!dirty || saving}>
        <Text>{saving ? t("profile.saving") : tCommon("save")}</Text>
      </Button>
```

- [ ] **Step 4: Verify typecheck and lint**

```bash
pnpm --filter @multica/mobile typecheck
pnpm --filter @multica/mobile lint
```

Expected: both `PASS`.

- [ ] **Step 5: Commit**

```bash
git add apps/mobile/locales "apps/mobile/app/(app)/[workspace]/more/settings/profile.tsx"
git commit -m "feat(mobile): translate profile screen

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

---

### Task 4: Migrate `notifications.tsx`

**Files:**
- Modify: `apps/mobile/locales/en/settings.json` (add `notifications` section)
- Modify: `apps/mobile/locales/zh-Hans/settings.json` (add `notifications` section)
- Modify: `apps/mobile/app/(app)/[workspace]/more/settings/notifications.tsx`

**Interfaces:**
- Consumes: `useTranslation("settings")` pattern established in Task 2.
- Produces: `notifications.*` keys under the `settings` namespace.

- [ ] **Step 1: Add the `notifications` section to both settings.json files**

Edit `apps/mobile/locales/en/settings.json` — add as a new top-level
sibling after `"profile"` (add a trailing comma after the `"profile"`
block's closing `}`):

```json
  "notifications": {
    "error": "Failed to load notification preferences.",
    "inbox_section": {
      "title": "Inbox notifications",
      "description": "Which events show up in your inbox."
    },
    "groups": {
      "assignments": {
        "label": "Assignments",
        "description": "When you're assigned an issue or removed as assignee."
      },
      "status_changes": {
        "label": "Status changes",
        "description": "When an issue's status changes."
      },
      "comments": {
        "label": "Comments",
        "description": "New comments on issues you're subscribed to."
      },
      "updates": {
        "label": "Issue updates",
        "description": "Edits to title, description, labels, priority, or due date."
      },
      "agent_activity": {
        "label": "Agent activity",
        "description": "When an agent picks up, runs, or completes a task."
      }
    },
    "system_section": {
      "title": "System",
      "description": "Multica-wide announcements and important account events."
    },
    "system_notifications": {
      "label": "System notifications",
      "description": "Account changes, security alerts, product updates."
    }
  }
```

Edit `apps/mobile/locales/zh-Hans/settings.json` — same position, add:

```json
  "notifications": {
    "error": "加载通知设置失败。",
    "inbox_section": {
      "title": "收件箱通知",
      "description": "哪些事件会显示在你的收件箱中。"
    },
    "groups": {
      "assignments": {
        "label": "分配",
        "description": "当你被分配为 issue 负责人，或被移除负责人身份时。"
      },
      "status_changes": {
        "label": "状态变更",
        "description": "当 issue 的状态发生变化时。"
      },
      "comments": {
        "label": "评论",
        "description": "你订阅的 issue 有新评论时。"
      },
      "updates": {
        "label": "issue 更新",
        "description": "标题、描述、标签、优先级或截止日期的修改。"
      },
      "agent_activity": {
        "label": "智能体活动",
        "description": "当智能体认领、运行或完成一个 task 时。"
      }
    },
    "system_section": {
      "title": "系统",
      "description": "Multica 范围内的公告和重要的账号事件。"
    },
    "system_notifications": {
      "label": "系统通知",
      "description": "账号变更、安全提醒、产品更新。"
    }
  }
```

(`issue` / `task` stay lowercase English per the entity mixed rule in
`conventions.mdx` — these are UI toggle labels/descriptions, not doc
prose, so they follow the "UI strings, state names, code references →
lowercase English" row.)

- [ ] **Step 2: Run the parity test**

```bash
pnpm --filter @multica/mobile test
```

Expected: `PASS`.

- [ ] **Step 3: Migrate `notifications.tsx`**

Open `apps/mobile/app/(app)/[workspace]/more/settings/notifications.tsx`.

Delete the file header comment's now-inaccurate line — change:

```ts
/**
 * Notification preferences subscreen. 5 inbox groups + system_notifications
 * toggle, each backed by an optimistic PUT /api/notification-preferences.
 *
 * Copy mirrors packages/views/settings/components/notifications-tab.tsx but
 * hardcoded English (mobile has no i18n infra yet). The group labels MUST
 * stay in sync with web — they describe the same server-side semantics,
 * and divergent labels would violate behavioral parity (apps/mobile/CLAUDE.md).
 */
```

to:

```ts
/**
 * Notification preferences subscreen. 5 inbox groups + system_notifications
 * toggle, each backed by an optimistic PUT /api/notification-preferences.
 *
 * Copy mirrors packages/views/settings/components/notifications-tab.tsx in
 * meaning (translated via apps/mobile/locales/*.json, not shared code). The
 * group labels MUST stay in sync with web — they describe the same
 * server-side semantics, and divergent labels would violate behavioral
 * parity (apps/mobile/CLAUDE.md).
 */
```

Add the import (after the `react-native` import):

```ts
import { useTranslation } from "react-i18next";
```

Delete the hardcoded `INBOX_GROUPS` constant (lines 23-53) — it moves
inline into the component body so labels/descriptions can use `t()`.

Inside `export default function NotificationsSettingsScreen()`, right
after `const mutation = useUpdateNotificationPreferences();`, add:

```ts
  const { t } = useTranslation("settings");

  const inboxGroups: Array<{
    key: Exclude<NotificationGroupKey, "system_notifications">;
    label: string;
    description: string;
  }> = [
    {
      key: "assignments",
      label: t("notifications.groups.assignments.label"),
      description: t("notifications.groups.assignments.description"),
    },
    {
      key: "status_changes",
      label: t("notifications.groups.status_changes.label"),
      description: t("notifications.groups.status_changes.description"),
    },
    {
      key: "comments",
      label: t("notifications.groups.comments.label"),
      description: t("notifications.groups.comments.description"),
    },
    {
      key: "updates",
      label: t("notifications.groups.updates.label"),
      description: t("notifications.groups.updates.description"),
    },
    {
      key: "agent_activity",
      label: t("notifications.groups.agent_activity.label"),
      description: t("notifications.groups.agent_activity.description"),
    },
  ];
```

Change the error-state text:
```tsx
        <Text className="text-sm text-destructive text-center">
          Failed to load notification preferences.
        </Text>
```
to:
```tsx
        <Text className="text-sm text-destructive text-center">
          {t("notifications.error")}
        </Text>
```

Change the inbox `Section` and its `.map` source from `INBOX_GROUPS` to
`inboxGroups`:
```tsx
      <Section
        title="Inbox notifications"
        description="Which events show up in your inbox."
      >
        {INBOX_GROUPS.map((group, idx) => {
          const enabled = preferences[group.key] !== "muted";
          const isLast = idx === INBOX_GROUPS.length - 1;
```
to:
```tsx
      <Section
        title={t("notifications.inbox_section.title")}
        description={t("notifications.inbox_section.description")}
      >
        {inboxGroups.map((group, idx) => {
          const enabled = preferences[group.key] !== "muted";
          const isLast = idx === inboxGroups.length - 1;
```

Change the System `Section` and its row:
```tsx
      <Section
        title="System"
        description="Multica-wide announcements and important account events."
      >
        <View className="flex-row items-center px-4 py-3 gap-3">
          <View className="flex-1">
            <Text className="text-base font-medium text-foreground">
              System notifications
            </Text>
            <Text className="text-xs text-muted-foreground mt-0.5">
              Account changes, security alerts, product updates.
            </Text>
          </View>
```
to:
```tsx
      <Section
        title={t("notifications.system_section.title")}
        description={t("notifications.system_section.description")}
      >
        <View className="flex-row items-center px-4 py-3 gap-3">
          <View className="flex-1">
            <Text className="text-base font-medium text-foreground">
              {t("notifications.system_notifications.label")}
            </Text>
            <Text className="text-xs text-muted-foreground mt-0.5">
              {t("notifications.system_notifications.description")}
            </Text>
          </View>
```

- [ ] **Step 4: Verify typecheck and lint**

```bash
pnpm --filter @multica/mobile typecheck
pnpm --filter @multica/mobile lint
```

Expected: both `PASS`. Typecheck will catch a stale `INBOX_GROUPS`
reference if the Step 3 deletion/replacement was incomplete.

- [ ] **Step 5: Commit**

```bash
git add apps/mobile/locales "apps/mobile/app/(app)/[workspace]/more/settings/notifications.tsx"
git commit -m "feat(mobile): translate notifications screen

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
```

---

### Task 5: Full verification

**Files:** none (verification only).

**Interfaces:**
- Consumes: everything from Tasks 1-4.

- [ ] **Step 1: Run the full check suite**

```bash
pnpm --filter @multica/mobile typecheck
pnpm --filter @multica/mobile lint
pnpm --filter @multica/mobile test
```

Expected: all three `PASS`.

- [ ] **Step 2: Manual bilingual pass**

On a running build (device or Metro-connected simulator):

1. Open Settings. Confirm every visible string (Account/Workspaces/
   Appearance/Language section titles, theme options, sign-out button)
   renders in the language matching the device's current locale.
2. Switch the new Language radio to 简体中文 (Chinese). Confirm the
   entire Settings screen — including the Notifications and Profile
   subscreens when navigated into — re-renders in Chinese immediately,
   with no app restart required.
3. On the Profile subscreen, tap the avatar to open the action sheet.
   Confirm "Take Photo" / "Choose from Library" / "Remove Photo" /
   "Cancel" show in the current language.
4. Switch back to English. Confirm everything reverts.
5. Force-quit and relaunch the app. Confirm the last-selected language
   (not the device default) is what loads — this is the
   `expo-secure-store` persistence from Task 1 taking effect.

- [ ] **Step 3: Confirm no stale hardcoded strings remain in the 3 pilot files**

```bash
grep -n '"[A-Z][a-z].*"' \
  "apps/mobile/app/(app)/[workspace]/more/settings.tsx" \
  "apps/mobile/app/(app)/[workspace]/more/settings/profile.tsx" \
  "apps/mobile/app/(app)/[workspace]/more/settings/notifications.tsx"
```

Expected: no matches that are user-facing copy (className strings and
non-UI string literals like `"light"` / `"dark"` are fine and will
still appear — the goal is confirming no leftover `Alert.alert("...")`,
JSX text, or label props with literal English/Chinese sentences).

If this step required any fixes, commit them with a `fix(mobile):`
prefix describing exactly what was missed.
