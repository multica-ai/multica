# BRANDING.md — Hira fork handbook

This fork Vietnamizes + rebrands Multica as "Hira" using a 3-layer principle so that
`git merge upstream/main` stays low-conflict. Full plan:
docs/superpowers/plans/2026-06-13-viet-hoa-multica.md

> **AI agents / new contributors start here.** This is the authoritative fork doc.
> [`AGENTS.md`](AGENTS.md) and [`CLAUDE.md`](CLAUDE.md) carry a short banner pointing
> back to this file. To pull upstream features, run `scripts/sync-upstream.sh`
> (see *Upstream sync playbook* below) — don't merge upstream by hand unless you
> have to.

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

Every upstream-owned file this fork edits. New fork-owned files (`locales/vi/**`,
`brand.css`, `README.vi.md`, `docs/brand/**`, the plan) are not listed — they never conflict.

| File | Change | Conflict policy |
|---|---|---|
| packages/core/i18n/types.ts | +"vi" in SupportedLocale + SUPPORTED_LOCALES (2 lines); DEFAULT_LOCALE stays "en" | Re-apply 2 lines |
| packages/core/i18n/pick-locale.test.ts | +1 it() block for vi | Keep both sides (additive) |
| server/internal/handler/auth.go | +"vi" in supportedLanguages | Re-apply 1 line |
| server/internal/handler/user_language_test.go | +1 test for vi | Keep both sides |
| packages/views/locales/index.ts | +25 vi imports + vi block in RESOURCES | Re-apply block |
| packages/views/locales/{en,zh-Hans,ko,ja}/settings.json | +"vietnamese" language label key | Re-apply 1 key per file |
| packages/views/settings/components/preferences-tab.tsx | +1 vi option in languageOptions | Re-apply 1 line |
| apps/web/app/layout.tsx | +vi in HTML_LANG; +vietnamese font subsets; Hira metadata (title/description/siteName/metadataBase=app.hira.vn/locale=vi_VN; twitter block removed) | Take upstream, re-apply 3 regions |
| apps/desktop/src/renderer/src/App.tsx | +vi in HTML_LANG | Re-apply 1 line |
| packages/views/onboarding/templates/index.ts | +vi→en content fallback | Re-apply 1 line |
| apps/web/lib/use-cases-i18n.ts | +vi UseCaseText block | Re-apply block |
| apps/web/features/landing/i18n/types.ts | +vi in localeLabels + locales array | Re-apply (required for typecheck) |
| apps/web/app/globals.css | +@import brand.css (after base.css) | Re-apply 1 line |
| apps/desktop/src/renderer/src/globals.css | +@import brand.css (after base.css) | Re-apply 1 line |
| packages/ui/package.json | +"./styles/brand.css" in exports (required by desktop's package-path import) | Re-apply 1 line |
| scripts/local-env.sh | derive+export NEXT_PUBLIC_API_URL/NEXT_PUBLIC_WS_URL so `make dev` proxies /api to the backend, not itself (general dev-tooling bugfix; good upstream candidate) | Keep ours; drop if upstream fixes it |
| apps/web/public/favicon.svg | Hira "h." mark | merge=ours (auto) |
| server/internal/service/email.go | sender noreply@hira.vn; VI verification + invitation subjects/bodies; appURL app.hira.vn; CTA indigo | Take upstream, re-apply 6 strings |
| server/internal/service/email_test.go | invitation subject expectation → VI/Hira | Re-apply 1 assertion |
| README.md | +Tiếng Việt link in language nav; +Vietnamese "Bản fork cá nhân — Hira" notice block after the header | Take upstream, re-apply the 2 fork additions (top of file) |
| AGENTS.md | Fork-notice banner prepended above upstream content (golden rules + pointers) | Keep our banner, take upstream body below it |
| CLAUDE.md | Fork-notice blockquote inserted after the intro line (golden rules + pointers) | Keep our blockquote, take upstream body |

> **In-app brand glyph (MulticaIcon) intentionally NOT swapped.** It is a monochrome
> `currentColor` clip-path used as a loading spinner (animate-spin/pulse) and themed glyph
> across 11 call sites; replacing it with the colored "h." mark would break spinners and
> `text-white`/`text-foreground` contexts. A dedicated monochrome Hira glyph is a separate
> design task. The colored "h." mark lives in `favicon.svg` (tab/PWA/OS icon).

> **Emails are Vietnamese-only by design.** `email.go` sends verification + invitation
> emails in Vietnamese to every recipient regardless of their `language` preference. This is
> intentional for the Vietnamese-market deployment (upstream was hardcoded English-only and
> equally not locale-aware). If this fork ever needs multi-language emails, make `email.go`
> select the template by the recipient's locale — that is a new feature, not a sync concern.

> **Desktop OS-packaging rebrand DEFERRED** (web-only deployment). The desktop renderer
> already gets Vietnamese + the indigo palette via the rows above. When shipping a desktop
> build, rebrand `apps/desktop/{package.json, electron-builder.yml, src/main/index.ts}`
> (productName/appId `vn.hira.desktop`/protocol `hira`/`publish` → fork releases/`app.setName`)
> and regenerate `build/icon.{png,icns,ico}` + `resources/icon.png` from the Hira mark. See
> plan Task 14. The icon files are already `merge=ours` in `.gitattributes`.

## Upstream sync playbook

**Preferred: the helper script.** It does everything below safely — adds the
`upstream` remote if missing, self-configures the `merge=ours` driver, merges into
an *isolated* `sync/upstream-<timestamp>` branch (never touching `main`), and runs
the three safety nets. It never pushes.

```bash
scripts/sync-upstream.sh            # fetch + merge into a sync branch + run safety nets
scripts/sync-upstream.sh --full     # also run the full `make check` (Go + E2E)
# On conflicts it stops on the sync branch with the file list; resolve each per the
# "Conflict policy" column above, then: git add -A && git merge --continue
# When green: git switch main && git merge --no-ff sync/upstream-<ts> && git push
```

**Manual fallback** (if you must merge by hand):

```bash
git fetch upstream
git switch -c sync/upstream-manual main
git merge upstream/main                            # merge=ours protects brand assets (configure once: git config merge.ours.driver true)
# Conflicts? open the Touch-point Registry above and resolve each file per its "Conflict policy" column.
pnpm install                                       # lockfile / catalog may have changed
pnpm typecheck                                     # SAFETY NET 1 — see below
pnpm test                                          # SAFETY NET 2 — see below
cd server && go build ./... && go test ./...       # server still compiles / passes
make check                                         # full pipeline (typecheck + unit + Go + E2E) before push
# then: git switch main && git merge --no-ff sync/upstream-manual && git push
```

### Three automated safety nets after a merge
1. **`pnpm typecheck`** — any new `Record<SupportedLocale, …>` map upstream adds will fail
   to compile until it has a `vi` entry. The compiler enumerates every wiring site for you.
2. **`packages/views/locales/parity.test.ts`** — fails (and lists the keys) whenever upstream
   adds an `en` key/namespace that `vi` doesn't have yet. To resolve:
   - New namespace: `cp packages/views/locales/en/<ns>.json packages/views/locales/vi/`,
     register it in `locales/index.ts` (import + RESOURCES), then translate.
   - New keys in an existing namespace: the merge updates `en` (and `ja`/`ko`/`zh-Hans`,
     which upstream owns) but does NOT touch `vi/<ns>.json` (fork-owned), so `vi` falls
     behind. Copy the new keys from `en/<ns>.json` into `vi/<ns>.json` and translate them.
     Keep `{{placeholders}}` and both plural forms. The failing parity test prints the exact
     missing keys. (Verified live: an upstream merge added 19 `agents.json` keys to `en`,
     and parity flagged all 19 as missing from `vi`.)
3. **`.gitattributes` `merge=ours`** — brand assets (favicon, desktop icons, logos) are never
   overwritten by upstream. Requires `git config merge.ours.driver true` once per clone.

### Why this stays mergeable
The fork only *adds* a locale and *overlays* brand tokens; it never renames technical
identifiers or edits upstream's `en/zh-Hans/ko/ja` strings or `tokens.css`. The diff against
upstream is ~20 small touch-points (all listed above) plus fork-owned new files. Re-applying
20 small hunks is minutes of work; the safety nets make missed translations impossible to
ship silently.

## Forbidden (lessons from the prior app-hira fork)
- Do NOT rename @multica/* packages, the `multica` CLI, the Go module, env vars, DB
  names, or the `multica-locale` cookie. Surface rebrand only.
- Do NOT edit `packages/ui/styles/tokens.css` or `base.css` — all overrides go in `brand.css`.
- Do NOT edit strings in `locales/{en,zh-Hans,ko,ja}` (except registering the `vietnamese`
  language label key).
- Do NOT rewrite upstream pages/components just to restyle — override via tokens/CSS first.
- Keep `DEFAULT_LOCALE = "en"` (Vietnamese users auto-match `vi` via Accept-Language).
