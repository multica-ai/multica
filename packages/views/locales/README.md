# Adding a locale to Multica

The i18n architecture is designed so adding a language is a registration
exercise plus content — no plumbing changes required (with one exception
noted below for RTL locales). This document is the checklist.

Existing locales: `en` (default), `zh-Hans` (Simplified Chinese), `he`
(Hebrew, RTL).

## 1. Register the locale code

Add the new code to **`packages/core/i18n/types.ts`**:

- Extend the `SupportedLocale` union: `"en" | "zh-Hans" | "he" | "<new>"`.
- Append to `SUPPORTED_LOCALES`.
- Add a `HTML_LANG[<new>] = "<bcp47>"` entry. Use the region-tagged BCP-47
  form when there's a conventional one (e.g. `zh-Hans` → `"zh-CN"`); plain
  ISO 639 otherwise (e.g. `he` → `"he"`, `ja` → `"ja"`).

That single file is the source of truth — `pickLocale`, `matchLocale`, the
parity test, the request negotiator, and the resource registry all consume
these constants.

## 2. Generate translation files

Run the AI seed script (requires `ANTHROPIC_API_KEY`):

```bash
pnpm seed-locale --source en --target <new>
```

This calls Claude Opus once per namespace, validates that every translated
JSON has the **exact** key set of the source (fails loudly if not), and
writes 24 files under `packages/views/locales/<new>/`. The output is a
starting point — review every namespace (especially `auth`, `settings`,
`onboarding`, `common`) before merging. The script preserves ICU
placeholders, Trans tags, markdown, entity IDs (`MUL-123`), and brand
wordmarks (Multica, GitHub, etc.).

To translate manually instead, copy `packages/views/locales/en/` to
`packages/views/locales/<new>/` and translate each value while keeping the
key tree identical. The parity test (`parity.test.ts`) will fail until
every key has a counterpart.

## 3. Register the resources

Edit **`packages/views/locales/index.ts`**:

- Add 24 `import <new><Namespace> from "./<new>/<file>.json"` lines.
- Add a `<new>: { ... }` entry to `RESOURCES`.

## 4. Add the locale to the settings switcher

Edit **`packages/views/settings/components/preferences-tab.tsx`**:

- Append `{ value: "<new>", label: t(($) => $.preferences.language.<new>) }`
  to `languageOptions`.

Edit each `settings.json` to add the new label key under
`preferences.language.<new>`:

- `en/settings.json` — English name (e.g. `"Japanese"`)
- `zh-Hans/settings.json` — Chinese name (e.g. `"日语"`)
- `<new>/settings.json` — Native name (e.g. `"日本語"`)

## 5. Update the server allowlist

Edit **`server/internal/handler/auth.go`** — add the new code to
`supportedLanguages`:

```go
var supportedLanguages = map[string]struct{}{
    "en":      {},
    "zh-Hans": {},
    "he":      {},
    "<new>":   {},
}
```

Mirror the test in `server/internal/handler/user_language_test.go` (copy
`TestUpdateMeAcceptsHebrew` as a template).

The server rejects any value not in this map with 400 `unsupported
language`, so missing this step breaks cross-device sync silently — the
cookie persists locally but PATCH `/api/me` fails.

## 6. (RTL only) Mark the locale as right-to-left

If the new locale is RTL (Arabic `ar`, Persian `fa`, Urdu `ur`, …), edit
**`packages/core/i18n/direction.ts`**:

```ts
export const RTL_LOCALES: ReadonlySet<SupportedLocale> = new Set([
  "he",
  "<new>",
]);
```

This drives `<html dir>` on web (via `apps/web/app/layout.tsx`) and on
desktop (via `apps/desktop/src/renderer/src/App.tsx` + the inline
bootstrap in `index.html` that prevents the first-paint LTR flash). It
also flips the workspace sidebar to the trailing edge via the `side` prop
in `packages/views/layout/app-sidebar.tsx`.

If the new locale is LTR, no changes here — the default direction handles
it.

## 7. (Optional) Fonts

If the new script needs a specific system font for fallback rendering,
append it to **both**:

- `apps/web/app/layout.tsx` — the `fallback` array of the `Inter()` call.
- `apps/desktop/src/renderer/src/globals.css` — the `--font-sans` CSS
  variable.

These must stay in sync. Inter only loads the Latin subset, so non-Latin
characters fall through this chain per-character.

If the script is part of the Latin family or has decent OS-default
support, this step can be skipped.

## 8. Run the verification

```bash
pnpm typecheck            # union widening will surface missing switch arms
pnpm test                 # parity test enforces full key coverage
make test                 # Go side: user_language_test must include the new code
```

The parity test (`packages/views/locales/parity.test.ts`) iterates every
non-`en` locale automatically — no edits needed.

Manual smoke (web on the dev port, default 3000 or override
`FRONTEND_PORT`):

1. Set `multica-locale` cookie to the new code and reload.
2. Verify `<html lang="..." dir="ltr|rtl">` in DevTools.
3. Visit `/login`, the inbox, the issues list, an issue detail, and
   settings. Visual sweep: padding/margins look intentional, popovers
   align correctly, entity codes like `MUL-123` stay LTR within RTL text
   (Unicode bidi handles this automatically — no `<bdi>` wrappers
   required unless a regression is found).

## Files NOT in scope for the core add

These are explicit follow-ups, tracked per-locale:

- `apps/docs/content/docs/**/*.<lang>.mdx` — translated docs pages.
- `apps/docs/content/docs/developers/conventions.<lang>.mdx` — translation
  glossary / voice guide for this locale.
- `packages/views/onboarding/utils/starter-content-content-<lang>.ts` —
  translated onboarding starter project.
- `README.<lang>.md` — translated top-level README.

Each can land in a separate PR; none of them block locale support.

## Reference

- Existing zh-Hans wiring: search the repo for `"zh-Hans"` — every spot
  you need to mirror is one line per locale.
- Existing Hebrew (RTL) wiring: the same plus `RTL_LOCALES`,
  `app-sidebar.tsx`'s `sidebarSide`, and the desktop `index.html` inline
  bootstrap script that sets `dir="rtl"` from `localStorage` before any
  stylesheet evaluates.
