# Mobile i18n Foundation — Design

Date: 2026-07-07
Status: Approved (infra + pilot; full-app translation sweep is separate follow-up work)

## Context

`apps/mobile` currently has zero i18n infrastructure — every string across
~50+ screens/components is hardcoded English. This is a known, documented
gap: `app/(app)/[workspace]/more/settings/notifications.tsx:6` explicitly
comments "hardcoded English (mobile has no i18n infra yet)".

Web/desktop already have a mature system: `i18next` + `react-i18next`,
4 languages (`en`/`zh-Hans`/`ko`/`ja`), namespaced JSON files per feature
in `packages/views/locales/`, and a mandatory glossary/voice guide at
`apps/docs/content/docs/developers/conventions.mdx`. Per the root
`CLAUDE.md`, mobile is independent and owns its own i18n — it does not
import web's `packages/views/locales/` or `packages/core/i18n/` code, but
Chinese copy must still follow the same glossary/voice rules.

## Goal

Stand up mobile's own i18n runtime (library, locale detection, persisted
override, namespace file structure) and prove it end-to-end on one pilot
cluster: the Settings screens (`settings.tsx`, `profile.tsx`,
`notifications.tsx`). English and Simplified Chinese (`zh-Hans`) only.

## Non-goals

- Translating the other ~47 screens/components — separate follow-up work,
  picked up incrementally per screen.
- Korean (`ko`) / Japanese (`ja`) — web's other two languages. Adding a
  language later is "drop in a new JSON file per namespace," not an
  architecture change, so deferring them costs nothing structural.
- Any change to web/desktop's i18n code — mobile's runtime is fully
  separate per the root `CLAUDE.md` "Mobile is independent" rule.

## Dependencies

Per `apps/mobile/CLAUDE.md` Lesson #1 (check `dist-tags`, don't hardcode
from memory — verified today):

- `expo-localization` — installed via `pnpm exec expo install
  expo-localization`, which resolves to the SDK 55-aligned `55.0.16` (not
  npm `latest`, which is `57.0.0` and outpaces this project's Expo SDK).
- `i18next@26.3.4` (npm `latest`, not Expo-managed — pin explicitly).
- `react-i18next@17.0.8` (npm `latest`). Peer deps verified: requires
  `i18next >= 26.2.0` (satisfied by `26.3.4`) and `react >= 16.8.0`
  (satisfied by this project's React 19.1) — no conflicts.

## Architecture

### File structure

```
apps/mobile/
  lib/i18n/
    index.ts        # i18next.init() — registers resources, fallbackLng: "en"
    use-locale.ts    # persisted preference, mirrors use-color-scheme.ts
  locales/
    en/
      common.json    # cross-screen generic words: Save/Cancel/Delete/Sign out...
      settings.json  # Settings-cluster-specific copy
    zh-Hans/
      common.json
      settings.json
    parity.test.ts   # key-parity guard, mirrors packages/views/locales/parity.test.ts
```

`lib/i18n/index.ts` is initialized once at module load from
`app/_layout.tsx`, the same way `prewarmHighlighter()` already is.

### Locale detection and persistence

Mirrors the existing theme-preference pattern in
`apps/mobile/lib/use-color-scheme.ts` exactly:

- First install: `expo-localization` reads the device locale. A `zh-*`
  device locale selects `zh-Hans`; anything else selects `en`. This is a
  silent default, not a first-run prompt — matches standard iOS/Android
  app behavior.
- `use-locale.ts` exposes `{ locale, preference, setPreference }`.
  `preference` is `"en" | "zh-Hans" | "system"` (`"system"` follows the
  device-detected value from the point above). Persisted to
  `expo-secure-store` under key `language-preference`, semantically
  parallel to `theme-preference`.
- `setPreference(p)` calls `i18next.changeLanguage(p)`. `react-i18next`'s
  `useTranslation()` subscribes to language changes automatically — no
  manual force-rerender needed.

### Settings screen changes

`settings.tsx` gets a new language `RadioGroup` (English / 简体中文 / 跟随系统)
placed immediately next to the existing theme `RadioGroup`, reusing the
same section container styling. All three pilot files
(`settings.tsx`, `profile.tsx`, `notifications.tsx`) replace their
hardcoded strings with `useTranslation()`'s `t("namespace.key")`.
`notifications.tsx`'s "hardcoded English (mobile has no i18n infra yet)"
header comment is deleted as part of this — it's no longer true.

### Copy and terminology

Chinese copy follows `apps/docs/content/docs/developers/conventions.mdx`
strictly: entities (`issue`/`skill`/`task`) stay lowercase English;
concepts (Workspace → 工作区, Settings → 设置, etc.) are fully translated;
full-width punctuation; terms not in the glossary follow the precedent in
`apps/docs/content/docs/*.zh.mdx`. The Settings pilot is low-risk here —
it's almost entirely generic UI words already covered by the glossary's
"Translate fully — generic UI words" table, not entity terminology.

### Testing

- `parity.test.ts`: asserts every key in `en/*.json` has a `zh-Hans`
  counterpart and vice versa (normalizing `_one`/`_other` plural suffixes
  the same way `packages/views/locales/parity.test.ts` does), so a future
  PR can't add an English key without its Chinese translation.
- The three pilot files have no existing test coverage (same situation as
  the `ActionSheetIOS` migration in the Android foundation plan). This
  round doesn't add new screen-level tests; verification is typecheck +
  manually toggling the language switcher and confirming both languages
  render correctly on the Settings cluster.

### Tech-stack baseline update

`apps/mobile/CLAUDE.md` "Tech-stack baseline" gets three new lines:
`i18next`, `react-i18next`, `expo-localization`.

## Verification

1. `pnpm --filter @multica/mobile typecheck`
2. `pnpm --filter @multica/mobile lint`
3. `pnpm --filter @multica/mobile test` (includes the new `parity.test.ts`)
4. Manual: launch the app, open Settings, switch the language radio
   between English / 简体中文 / 跟随系统, confirm `settings.tsx`,
   `profile.tsx`, and `notifications.tsx` all render the correct
   language and that the choice survives an app restart (persisted via
   `expo-secure-store`).

## Follow-up (separate work, not this plan)

- Translate the remaining ~47 mobile screens/components incrementally,
  namespace-by-namespace, following the same pattern established here.
- Add `ko` / `ja` namespace JSON files once a translator/reviewer for
  those languages is available — no code changes needed, just new JSON
  files plus registering them in `lib/i18n/index.ts`'s `resources` map
  and the `SUPPORTED_LOCALES`-equivalent list mobile defines.
