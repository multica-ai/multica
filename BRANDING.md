# BRANDING.md — Hira fork handbook

This fork Vietnamizes + rebrands Multica as "Hira" using a 3-layer principle so that
`git merge upstream/main` stays low-conflict. Full plan:
docs/superpowers/plans/2026-06-13-viet-hoa-multica.md

## 3-layer principle

- **Layer A — fork-owned new files** (zero conflict): `packages/views/locales/vi/**`,
  `packages/ui/styles/brand.css`, `docs/*.vi.md`, Hira logo/favicon assets.
- **Layer B — append-only edits to upstream files** (very low conflict): registering
  `vi` in `types.ts`, `auth.go`, `locales/index.ts`, the `Record<SupportedLocale>` maps,
  one `@import brand.css` line per app.
- **Layer C — content edits to upstream files** (low-medium conflict): metadata in
  `layout.tsx`, `email.go`, electron-builder. EVERY such edit MUST be logged in the
  Touch-point Registry below.

**Rule:** every time you edit a file that upstream owns, add a row to the registry in
the SAME commit.

## Touch-point Registry

| File | Change | Conflict policy |
|---|---|---|
<!-- tasks append rows here as they touch upstream files -->
| packages/core/i18n/types.ts | +"vi" in SupportedLocale + SUPPORTED_LOCALES (2 lines) | Re-apply 2 lines on conflict |
| packages/core/i18n/pick-locale.test.ts | +1 it() block for vi | Keep both sides (additive) |
| server/internal/handler/auth.go | +"vi" in supportedLanguages | Re-apply 1 line |
| server/internal/handler/user_language_test.go | +1 test for vi | Keep both sides |
| packages/views/locales/index.ts | +25 vi imports + vi block in RESOURCES | Re-apply block on conflict |

| packages/views/locales/{en,zh-Hans,ko,ja}/settings.json | +"vietnamese" language label key | Re-apply 1 key per file |
| packages/views/settings/components/preferences-tab.tsx | +1 vi option in languageOptions | Re-apply 1 line |
| apps/web/app/layout.tsx | +vi in HTML_LANG | Re-apply 1 line (see also Task 6/12) |
| apps/desktop/src/renderer/src/App.tsx | +vi in HTML_LANG | Re-apply 1 line |
| packages/views/onboarding/templates/index.ts | +vi→en content fallback | Re-apply 1 line |
| apps/web/lib/use-cases-i18n.ts | +vi UseCaseText block | Re-apply block |

## Upstream sync playbook
<!-- filled in by the final task -->

## Forbidden (lessons from the prior app-hira fork)
- Do NOT rename @multica/* packages, the `multica` CLI, the Go module, env vars, DB
  names, or the `multica-locale` cookie. Surface rebrand only.
- Do NOT edit `packages/ui/styles/tokens.css` or `base.css` — all overrides go in `brand.css`.
- Do NOT edit strings in `locales/{en,zh-Hans,ko,ja}` (except registering the `vietnamese`
  language label key).
- Do NOT rewrite upstream pages/components just to restyle — override via tokens/CSS first.
- Keep `DEFAULT_LOCALE = "en"` (Vietnamese users auto-match `vi` via Accept-Language).
