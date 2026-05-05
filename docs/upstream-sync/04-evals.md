# Step 4 — Evals

Eval-strategi for at sikre at refactoren ikke ændrer adfærd eller udseende. Hver fase i `03-decision.md` har en eval-gate der skal passere før vi går videre.

## Princip

> "Refactor er fuldført når brugeren ikke kan se forskel — i adfærd, udseende eller respons-tid."

Det betyder vi skal kunne **bevise** preservation, ikke bare påstå det. Beviset består af:

1. **Baseline-snapshot** før refactor (current `main`)
2. **Identisk scenario-suite** der kører efter hver fase
3. **Auto-sammenligning** af adfærd + visuel + API-respons
4. **Manuel smoke-test** af kritiske flows i browser

## Eksisterende test-base

Vi starter ikke fra nul:

| Suite | Filer | Linjer | Dækning |
|---|---|---|---|
| Playwright E2E | `e2e/*.spec.ts` × 7 | 759 | auth, navigation, issues, comments, notifications, projects-access, settings |
| Vitest (views) | `packages/views/**/*.test.tsx` | mange | komponent-niveau |
| Vitest (core) | `packages/core/**/*.test.ts` | mange | hooks, queries, stores |
| Go tests | `server/internal/**/*_test.go` | mange | handlers, services |

**Hvad der mangler for refactor-safe coverage:**
- Inbox (folders, archive, multi-select, mark-read)
- Members admin (role toggles, member detail, enforcement)
- MCP install guide
- Sandbox runtime UI
- Sidebar mobile-auto-close
- Settings tabs (Notifications-tab specifically)
- Comment-card mobile padding/sweep
- Visual regression coverage på alle cerebro-touched sider

## Phase 0 — Baseline capture

**Formål:** fastfryse "sådan ser/virker det nu" før vi rører noget.

### 0.1 Udvid E2E-coverage til alle cerebro-features

Tilføj specs der dækker hver cerebro-feature, så de kan re-køres efter hver refactor-fase:

| Ny spec | Scope | Estimerede tests |
|---|---|---|
| `e2e/inbox.spec.ts` | Folders, archive, multi-select, mark-read, real-time updates | ~12 tests |
| `e2e/members.spec.ts` | Members-list, member-detail, role toggle, enforcement toggle, remove member | ~10 tests |
| `e2e/sandbox.spec.ts` | Per-runtime sandbox toggle, status-indicator, opt-out | ~5 tests |
| `e2e/mcp.spec.ts` | MCP install guide flow, onboarding hook trigger | ~4 tests |
| `e2e/notifications-tab.spec.ts` | Per-channel toggles, route-choice, auto-subscribe rules | ~8 tests |
| `e2e/sidebar-mobile.spec.ts` | Mobile auto-close on navigation, pin-icons by type | ~5 tests |
| `e2e/comments-mobile.spec.ts` | Comment-card mobile widths, send-button, FAB hidden | ~6 tests |
| `e2e/profile.spec.ts` | Profile persistence, runtime injection | ~4 tests |
| `e2e/projects-extras.spec.ts` | Color picker, RepoURL field, MCP auto-detect | ~5 tests |
| `e2e/issues-private.spec.ts` | `IsPrivate` field, search-filter, redaction | ~6 tests |

**Total ny E2E-dækning:** ~65 tests, estimeret 800-1000 linjer specs.

### 0.2 Tilføj visual regression suite

Playwright understøtter screenshot-comparison nativt. Tilføj `e2e/visual.spec.ts` der tager screenshots af kritiske sider:

```typescript
test("inbox page visual baseline", async ({ page }) => {
  await page.goto("/inbox");
  await expect(page).toHaveScreenshot("inbox-list.png", {
    maxDiffPixelRatio: 0.01,
  });
});
```

Sider der får visual baseline:
- `/inbox` (list + folder-view + multi-select state)
- `/projects` (list + restricted dot + color)
- `/projects/<id>` (detail + access-toggle + members panel)
- `/members` (list + member-detail)
- `/settings` (alle tabs, fokus på Notifications-tab)
- `/issues` (list-row mobil + comment-card mobil)
- `/issues/<id>` (issue-detail + agent-live-card)
- `/runtimes/<id>` (sandbox-toggle)
- Sidebar (mobile collapsed + expanded)

Konfiguration: `playwright.config.ts` får `expect.toHaveScreenshot` defaults med 0.01 pixel diff threshold.

### 0.3 API contract baseline

Capture API-respons-shapes der ikke må ændre form:

```typescript
// e2e/api-contracts.spec.ts
test("ProjectResponse contains cerebro fields", async () => {
  const project = await api.createProject("contract-test");
  expect(project).toMatchObject({
    color: expect.any(String),
    repo_url: expect.toBeOneOf([null, expect.any(String)]),
    access: expect.stringMatching(/^(workspace|restricted)$/),
  });
});
```

Contract-tests for alle cerebro-felter på upstream-typer:
- `ProjectResponse.{color, repo_url, access}`
- `IssueResponse.is_private`
- `MemberWithUser` med usage + role
- Notifications response shape
- Inbox folder shape
- Sandbox config shape
- WorkSession i MCP responses

### 0.4 Performance baseline

Mål responstider for kritiske endpoints, så vi kan se om refactor introducerer regression:

```typescript
test("inbox list renders within budget", async ({ page }) => {
  const start = Date.now();
  await page.goto("/inbox");
  await page.waitForSelector('[data-testid="inbox-item"]');
  expect(Date.now() - start).toBeLessThan(2000);
});
```

Budgets capture'es for: inbox load, members list, project list, issue detail open.

### Done-criteria for Phase 0

- [ ] Alle 10 nye E2E-specs skrevet og passerer mod nuværende `main`
- [ ] Visual baselines genereret (`e2e/__snapshots__/`)
- [ ] API contract baseline kører grøn
- [ ] Performance budgets dokumenteret i `e2e/budgets.json`
- [ ] Komplet suite kører på <10 min lokalt
- [ ] CI integration tilføjet (kører på hver PR)

**Estimeret cost:** ~600k tokens (specs + baselines + CI-wiring).

## Per-fase eval-gates

### Phase 1 gate — L1 ekstraktion

Efter hver feature-flytning (cerebro-test, cerebro-users, cerebro-notifications, etc):

- [ ] Komplet E2E-suite grøn
- [ ] Visual regression: 0 unintended pixel-diffs
- [ ] API contracts uændrede
- [ ] Performance budgets holdt (±10%)
- [ ] Manuel browser-smoke (5 min): logg ind, åbn berørt feature, verificér det ser/virker som før

**Gate-fail-respons:** rul den enkelte ekstraktion tilbage, undersøg, prøv igen. Phase 1 er per-feature så blast radius er lille.

### Phase 2 gate — Inbox-replacement

Strengere gate fordi denne fase er stor:

- [ ] Hele `e2e/inbox.spec.ts` grøn (12 tests)
- [ ] Visual: identiske screenshots på alle inbox-states (list, folder, multi-select, archive, mark-read)
- [ ] WebSocket real-time updates virker (manuel test: åbn to browsere, marker som læst i den ene, verificér opdatering i den anden)
- [ ] Performance: inbox-load <2s, scroll smooth ved 1000+ items
- [ ] Ingen Console-errors i 10 minutters interaktion

### Phase 3 gate — L2 wrappers

Per wrapper:

- [ ] Specifik feature-spec grøn
- [ ] Visual regression på sider der bruger wrapped komponent: 0 pixel-diff
- [ ] Komponent-niveau Vitest grøn
- [ ] Hot-reload virker korrekt (dev server)

**Eskalering:** hvis 3+ wrappers fejler visual-check → wrap-strategien er ikke holdbar; revurder via tilbage til 02-assessment.

### Phase 4 gate — L3 markering

Lavere gate (markører er kommentarer, ingen funktionalitet rørt):

- [ ] `make typecheck` + `make test` grøn (sanity)
- [ ] `scripts/validate-cerebro-patches.sh` rapporterer alle 42 markører fundet
- [ ] `docs/cerebro-patches.md` har komplet registry
- [ ] Total kodelinjer i patches ≤200

### Phase 5 gate — Sync-validation

**Den endegyldige test:** kan vi merge upstream/main rent?

- [ ] Konfliktflade <15 filer (måles efter auto-resolve)
- [ ] Konflikt-linjer total <50
- [ ] Komplet eval-suite grøn på den merged branch
- [ ] Visual regression: cerebro-features har 0 diff; upstream-features har forventede diffs (manuel review)
- [ ] API contracts: cerebro-fields stadig til stede + alle nye upstream-fields til stede
- [ ] Manuel 30-min smoke-test af alle cerebro-features
- [ ] `make check` grøn

## Rollback-strategi

Hvis en eval-gate fejler:

1. **Identificér scope:** hvilken specifik test/screenshot/contract fejler
2. **Bisect:** seneste commit der introducerede failure
3. **Revert eller fix:** afhængigt af om det er en arkitektur-fejl eller en implementations-bug
4. **Re-run gate:** verificér grøn før vi går videre

Hver fase committer kun til en feature-branch. Merge til main kræver gate-passage.

## Værktøjer

- **Playwright** (allerede installeret) — E2E + visual regression
- **`@playwright/test` snapshots** — pixel-diff comparison
- **Vitest** (allerede installeret) — unit + integration
- **`go test`** — backend
- Ingen nye værktøjer kræves; vi udvider eksisterende suites

## Manuel smoke-test-checklist

Hver fase ender med en 5-30 min manuel test i browser. Checklist:

```
[ ] Login fungerer
[ ] Workspace-skift virker
[ ] Sidebar viser korrekt indhold (mobile + desktop)
[ ] Inbox loader, folders fungerer, archive fungerer
[ ] Issues-list scroller smooth, mobile-paddings korrekte
[ ] Issue-detail åbner, agent-live-card rendrer, comment-card mobil-OK
[ ] Projects-list viser color + restricted-dot
[ ] Project-detail åbner, access-toggle fungerer, members-panel vises
[ ] Members-side åbner, role-toggle persisterer, member-detail virker
[ ] Settings → Notifications-tab viser alle toggles
[ ] Runtimes-detail viser sandbox-toggle
[ ] MCP install guide vises i onboarding
[ ] WebSocket real-time updates fungerer
[ ] Ingen Console-errors
```

## Token-cost-budget for evals

| Aktivitet | Tokens |
|---|---|
| Skriv 10 nye E2E-specs | 350k |
| Visual regression setup + baselines | 100k |
| API contract tests | 80k |
| Performance baseline | 30k |
| CI integration | 40k |
| **Phase 0 total** | **600k** |
| Per-fase gate-runs (×6 faser, automated) | ~0 (kun CI-tid) |
| Eval-driven fix af regressioner | ~400k buffer |
| **Total inkl buffer** | **~1M tokens** |

Indeholdt i 03-decision.md's samlede 3,3M token-budget under "Buffer".

## Done-condition

Eval-arbejdet er færdigt når:
- [ ] Phase 0 baselines er capture'd og committed
- [ ] CI kører hele suiten på hver PR
- [ ] Hver fase i 03-decision.md har sit specifikke gate dokumenteret
- [ ] Alle gates har passeret deres respektive faser
- [ ] Phase 5 viser <15 konfliktfiler ved real upstream-merge

## Næste step

Hvis 01-04 er accepteret: start på Phase 0 — opret cerebro-zone-skelet + skriv de 10 nye E2E-specs.

Beslutning der skal træffes:
- Skal vi starte med Phase 0 nu, eller vil du først reviewe 01-04?
- Bekræfter du inbox-replacement (Phase 2)?
- Bekræfter du docs-take-theirs strategien?
