# Step 3 — Decision

Konkret faseplan baseret på `00-discovery.md`, `01-audit.md` og `02-assessment.md`. Hver fase har præcis scope, eval-gate, go/no-go-kriterium og rollback-strategi.

## Forudsætninger

Før Phase -1 starter:

- [x] Stakeholder-bekræftelser:
  - Cerebro-namespace bekræftet (præfiks `cerebro-*`)
  - Docs = upstream's dokumentation (vi tager theirs ved hver sync)
  - Inbox-replacement → konverteret til feature-flag-pattern (default-on)
- [ ] Tekniske forudsætninger:
  - `chore/upstream-sync-analysis` branch eksisterer som arbejds-base
  - Worktree på `../firtal-cerebro-upstream-sync` aktiv
  - Untrackede filer i main-checkout besluttet (commit/slet/ignore)

## Phase -1 — Preflight (~400k tokens, 1-2 dage)

**Formål:** Verificér ALLE tekniske antagelser med små engangs-eksperimenter FØR vi committer 3M+ tokens af refactor.

### Verifikationspunkter

| # | Antagelse | Eksperiment | Pass-kriterium |
|---|---|---|---|
| P1 | TypeScript module augmentation virker for upstream-typer | Augment `Project` type i `packages/cerebro-types/augment.ts`, importér + brug det udvidede felt fra `apps/web/` | `pnpm typecheck` grøn med extended type tilgængelig udenfor cerebro-pakke |
| P2 | Path-alias-shadowing virker i Next.js + Electron-vite | Skift `@multica/views/issues/comment-card` til at peges på `@cerebro/comments/comment-card` i tsconfig + bundler-config, byg begge apps | Begge apps bygger; ingen runtime-fejl; vores komponent rendres |
| P3 | Sqlc accepterer multi-source queries | Add `server/internal/cerebro/queries/test_query.sql`, opdater sqlc.yaml, kør `make sqlc` | Generated kode produceret korrekt fra begge query-mapper |
| P4 | Migrations fra to mapper virker | Læg `cerebro_test_001.sql` i `server/migrations/cerebro/`, opdater `cmd/migrate/main.go` til at scanne begge, kør `make migrate-up` | Begge migrationer kører i deterministisk rækkefølge |
| P5 | Playwright visual regression er stabil | Tag baseline af 3 sider (inbox, projects, settings), kør 5x, sammenlign | <0.1% pixel diff over 5 runs |
| P6 | Lint-regel kan håndhæve CEREBRO-PATCH markører | Implementér eslint-rule + go-vet-check der fejler på upstream-fil-modifikation uden markør, lav test-PR der bryder reglen | Lint-tjek fejler korrekt; PR med markør passerer |
| P7 | Wrapping af `comment-card.tsx` virker (single-file proof) | Lav fuld L2-wrapper for én komponent end-to-end, eval-suite grøn | E2E-tests for comments grønne; visual diff = 0 |
| P8 | Feature flag-mønster virker i begge apps | Implementér `cerebro_inbox_enabled` flag med Settings-tab + persistens, verificér routing skifter | Begge apps respekterer flag; toggle virker uden reload |

### Phase -1 done-criteria

- [ ] Alle 8 verifikationspunkter passerer
- [ ] Hvert eksperiment dokumenteret i `docs/upstream-sync/preflight/<P-N>.md` med:
  - Hvad vi gjorde
  - Resultat (pass/fail + eventuelle gotchas)
  - Hvad det betyder for hovedplanen
- [ ] Beslutningsdokument `docs/upstream-sync/preflight/SUMMARY.md` med GO/HOLD/NO-GO

### Phase -1 go/no-go

| Resultat | Action |
|---|---|
| **GO** — alle 8 punkter passerer | Committer videre til Phase 0 |
| **HOLD** — 1-2 punkter fejler med kendt mitigation | Implementér mitigation, re-test, dokumentér ændring i 02-assessment.md |
| **NO-GO** — 3+ punkter fejler eller fundamental antagelse brudt | Stop. Revurder strategi. Skriv `02-assessment-revision.md` med alternativer |

### Phase -1 rollback

Phase -1 er rent eksperimentelt — alt arbejde foregår i en throwaway-branch (`preflight/exploration`). Intet committes til main. Rollback = `git branch -D preflight/exploration`.

## Phase 0 — Foundation (~300k tokens, 1 dag)

**Forudsætning:** Phase -1 = GO.

### 0.1 Team-koordinering (FØR alt andet)

Disciplin-bekendtgørelse til team:
- Refactor-vinduet annonceres i interne kanaler
- Eksisterende åbne PR'er (per F5): `feat/access-combined-flow-and-keylock`, `fix/claude-empty-output-diagnostics-JEH-405`, `fix/project-access-response-field` osv → land FØR Phase 1, ELLER vent til efter Phase 5
- Alle nye feature-PRs i refactor-vinduet skal lande i `cerebro-*` zone (lint-håndhævet)

### 0.2 Cerebro-zone-skelet

Opret tomme mapper + READMEs:
```
packages/cerebro-feature-flags/  (NY — grundlag for feature flags)
packages/cerebro-access/
packages/cerebro-users/
packages/cerebro-artifacts/      (struktur klar; Phase 1 flytter eksisterende)
packages/cerebro-attachments/    (struktur klar; Phase 1 flytter eksisterende)
packages/cerebro-notifications/  (struktur klar; Phase 1 flytter eksisterende)
packages/cerebro-inbox/
packages/cerebro-runtime/
packages/cerebro-mcp/
packages/cerebro-types/
packages/cerebro-test/
packages/cerebro-ui/             (CSS-overrides + wrapped UI components)

server/internal/cerebro/
    access/
    users/
    artifacts/
    attachments/
    notifications/
    sandbox/
    profile/
    mcp/
    feature_flags/

server/migrations/cerebro/       (cerebro-specifikke migrationer fremover)
```

Hver mappe har en `README.md` der forklarer:
- Hvad denne pakke ejer
- Hvad der MÅ landes her
- Hvad der IKKE må landes her
- Hvordan upstream-import-niveauet ser ud

### 0.3 Feature flag-system

Bygges som første artefakt:

**Frontend:**
- `packages/cerebro-feature-flags/store.ts` — Zustand store, persisterer til localStorage + server
- `packages/cerebro-feature-flags/registry.ts` — central definition af alle cerebro-flags med default-værdier
- `packages/cerebro-feature-flags/settings-tab.tsx` — Settings-tab UI med toggles (registreres via `extraAccountTabs` prop på SettingsPage — F3-discovery)
- Initial flags: `cerebro_inbox`, `cerebro_access_control`, `cerebro_members_admin`, `cerebro_sandbox_ui`, `cerebro_mcp_guide` (alle default-on)

**Backend:**
- Ny migration `9010_cerebro_feature_flags.sql` med tabel `cerebro_feature_flags(workspace_id, user_id, flag_key, enabled)`
- Endpoint `/api/cerebro/feature-flags` (GET/PUT)
- Server udleder default = on hvis ikke override eksisterer

**App-roden:**
- `apps/web/app/[workspaceSlug]/(dashboard)/layout.tsx` mounter Cerebro feature flag provider
- Desktop ditto
- Hver feature wraps i `<FeatureFlag flag="cerebro_inbox">...<Else>...</FeatureFlag>` mønster

### 0.4 Lint-regel (CEREBRO-PATCH håndhævelse)

- ESLint-regel: enhver ændring i fil under `packages/views/`, `packages/core/` (eksklusiv cerebro-* pakker) skal have `// CEREBRO-PATCH(<navn>):` markør i diff
- Go-vet-check: ditto for upstream-paths under `server/`
- Pre-commit-hook installeres for at fange dette tidligt
- CI-job blokerer PR der bryder reglen
- Allowlist: discipline kan disables med `CEREBRO-ALLOW-NO-PATCH:` kommentar (kræver code review)

### 0.5 Sync-script

`scripts/upstream-sync.sh`:
1. Opret `chore/upstream-sync-$(date +%Y-W%V)` branch fra main
2. `git fetch upstream`
3. `git merge upstream/main`
4. **Auto-resolve:**
   - `git checkout --theirs apps/docs/ apps/web/app/docs/ apps/web/content/ '*.md' SELF_HOSTING* CONTRIBUTING*`
   - `git checkout --theirs server/pkg/db/generated/`
   - `make sqlc` (regenerér)
   - `pnpm install` (regenerér lockfile)
5. **Validate cerebro-patches:**
   - `scripts/validate-cerebro-patches.sh` — grep alle `CEREBRO-PATCH:` markører, verificér de stadig findes
6. **Rapport:**
   - Print konfliktfile-antal og total konflikt-linjer
   - Print cerebro-patches der konflikter (skal manuelt resolves)
   - Forventet output for sundt sync: <15 filer, <50 linjer

### 0.6 Eval-baseline

Per `04-evals.md` Phase 0:
- Skriv 10 nye E2E-specs (~65 tests)
- Capture visual regression baselines
- Capture API contract baselines
- Capture performance budgets
- CI-integration

### 0.7 CLAUDE.md-update

Tilføj sektion:

```markdown
## Cerebro extension discipline

This fork keeps cerebro-specific code separate from upstream multica:

1. **All new cerebro features land in `packages/cerebro-*/` or `server/internal/cerebro/*`.**
   Never modify upstream-files unless the modification is irreducible.

2. **When you must modify an upstream file**, mark the change with
   `// CEREBRO-PATCH(<descriptive-name>):` and document it in
   `docs/cerebro-patches.md`. Each patch must be ≤5 lines.

3. **New migrations use `9NNN_cerebro_*.sql` namespace.** Cerebro
   migrations live in `server/migrations/cerebro/`.

4. **Feature flag every cerebro feature** via `packages/cerebro-feature-flags`
   so the user can disable cerebro behaviors without redeploy.

The lint-regel in `.eslintrc` and `scripts/lint-go.sh` enforce these rules.
```

### Phase 0 done-criteria

- [ ] Team-bekendtgørelse sendt og bekræftet
- [ ] Cerebro-zone-skelet committed (alle mapper + READMEs)
- [ ] Feature flag-system fungerer (Settings-tab + 5 default-on flags)
- [ ] Lint-regel håndhæver CEREBRO-PATCH markører
- [ ] `scripts/upstream-sync.sh` kører med dry-run mod upstream/main
- [ ] Eval-baseline gemt i `e2e/__snapshots__/baseline/`
- [ ] CLAUDE.md har "Cerebro extension discipline" sektion
- [ ] PR mod main lander grønt

### Phase 0 go/no-go

| Resultat | Action |
|---|---|
| **GO** — alle done-criteria passerer | Committer videre til Phase 1 |
| **HOLD** — feature flag eller lint-regel virker ikke som forventet | Fix før vi går videre |
| **NO-GO** — fundamentalt sammenbrud | Tilbage til Phase -1 revurdering |

## Phase 1 — L1 ekstraktion (~700k tokens, 3-5 dage)

**Forudsætning:** Phase 0 = GO.

### 1.1 Rename eksisterende isolerede pakker (per F2-discovery)

7 pakker er allerede vores; rename til cerebro-prefiks:
- `packages/core/artifacts/` + `packages/views/artifacts/` → `packages/cerebro-artifacts/{core,views}/`
- `packages/core/attachments/` + `packages/views/attachments/` → `packages/cerebro-attachments/{core,views}/`
- `packages/core/notifications/` + `packages/views/notifications/` → `packages/cerebro-notifications/{core,views}/`
- `packages/views/members/` → `packages/cerebro-users/`

Trivielt arbejde: rename mapper, opdater package.json, opdater alle imports.

### 1.2 Flyt ny-fil L1-content

13 filer (eksklusiv inbox-page som håndteres i Phase 2):
- Test-additions → `packages/cerebro-test/`
- `notifications-tab.tsx` → `packages/cerebro-notifications/views/`
- Server-side cerebro-handlers → `server/internal/cerebro/`

Rækkefølge (mindst risiko først):
1. cerebro-test (kun tests rammer ved fejl)
2. cerebro-users-rename (mest selvstændigt)
3. cerebro-notifications-rename + flyt
4. cerebro-mcp
5. cerebro-realtime
6. cerebro-runtime + sandbox + profile
7. cerebro-api sub-client

### Phase 1 done-criteria pr feature

- [ ] Filer flyttet/renamed
- [ ] Imports opdateret (incl. tsconfig, package.json, sqlc.yaml hvis relevant)
- [ ] Gamle stier fjernet (ingen "compatibility shims")
- [ ] `pnpm typecheck` grøn
- [ ] `make test` grøn
- [ ] `make check` grøn
- [ ] **Eval-gate Phase 1:** alle baseline-scenarier passerer; visual regression: 0 unintended diff
- [ ] Manuel browser-smoke: feature virker som før

### Phase 1 go/no-go

| Resultat | Action |
|---|---|
| **GO** — alle 13 features + 7 renames lander grønt | Committer videre til Phase 2 |
| **HOLD** — én feature fejler eval-gate | Rul den enkelte feature tilbage, fix, re-prøv |
| **NO-GO** — 3+ features fejler | Stop. Revurder L1-listen. Måske skal nogle filer flyttes til L2/L3. |

### Phase 1 rollback

Hver feature er sin egen PR. Revert er sikker.

## Phase 2 — Inbox feature flag (~250k tokens, 1-2 dage)

**Forudsætning:** Phase 1 = GO.

**Scope:** Vores inbox-page placeres i `packages/cerebro-inbox/`. Routes vælger mellem upstream's inbox og cerebro's via feature flag (default cerebro).

### Implementering

```typescript
// apps/web/app/[workspaceSlug]/(dashboard)/inbox/page.tsx
import { useFeatureFlag } from "@cerebro/feature-flags";
import { InboxPage as UpstreamInbox } from "@multica/views/inbox";
import { CerebroInboxPage } from "@cerebro/inbox";

export default function Page() {
  const useCerebro = useFeatureFlag("cerebro_inbox");
  return useCerebro ? <CerebroInboxPage /> : <UpstreamInbox />;
}
```

Desktop-route ditto. Selve inbox-implementeringen flyttes 1:1 fra `packages/views/inbox/components/inbox-page.tsx` (vores 1198-linje version) til `packages/cerebro-inbox/inbox-page.tsx`. Upstream's inbox-page restaureres til upstream-state.

### Phase 2 done-criteria

- [ ] `packages/cerebro-inbox/` indeholder vores inbox + alle dependencies
- [ ] Web + desktop routes vælger via feature flag
- [ ] Feature flag default = on
- [ ] Settings-tab viser flag som toggle
- [ ] Toggle off → upstream's inbox vises korrekt
- [ ] Toggle on → cerebro's inbox vises korrekt (alle vores features intakte)
- [ ] **Eval-gate Phase 2:** alle 12 inbox-tests grønne; visual regression identisk; performance ≤baseline; WS real-time virker

### Phase 2 go/no-go

| Resultat | Action |
|---|---|
| **GO** — flag virker, begge varianter renders | Committer videre til Phase 3 |
| **HOLD** — feature toggle introducerer state-bug | Fix og re-prøv |
| **NO-GO** — fundamental konflikt mellem upstream og cerebro inbox-state | Inbox forbliver inline-modificeret (L3); revurder strategi |

## Phase 3 — L2 composition wrappers (~900k tokens, 5-7 dage)

**Forudsætning:** Phase 2 = GO.

**Scope:** 14 filer (fra 15, da settings-page er flyttet til L1) konverteres til wrapper-pattern.

Rækkefølge (samme som original 03-decision):
1. comment-card → cerebro-comments/
2. runtime-detail → cerebro-runtime/
3. project-detail, project-picker → cerebro-access/
4. chat-input, chat-message-list → cerebro-mcp/
5. issue-detail, agent-live-card, list-row, reply-input → cerebro-issues/
6. editor/readonly-content → cerebro-ui/
7. tasks-tab, agents-page → cerebro-users/

### Eskaleringskriterium

Hvis en wrapper koster >150 linjer eller dublerer >30% af upstream-komponentens logik → drop wrapping for den fil og flyt til L3.

Trigger: efter wrapper #5, gennemgå wrap-economics. Hvis 3+ wrappers har eskaleret til L3 → revurder for de resterende 9.

### Phase 3 done-criteria pr wrapper

- [ ] Wrapper-komponent eksisterer i cerebro-pakke
- [ ] Upstream-fil restaureret til upstream-state (vores diff = 0)
- [ ] Routes/imports opdateret til at bruge wrapper
- [ ] `pnpm typecheck` grøn
- [ ] **Eval-gate Phase 3:** specifik feature-spec grøn; visual regression: 0 unintended diff på sider der bruger komponenten

### Phase 3 go/no-go

| Resultat | Action |
|---|---|
| **GO** — alle 14 wrappers fungerer | Committer videre til Phase 4 |
| **HOLD** — 3-5 wrappers eskalerer til L3 | Acceptér; opdater statistik; gå videre |
| **NO-GO** — >5 wrappers fejler eller wrap-overhead introducerer perf-regression | Stop. Konvertér resterende til L3. Phase 4 håndterer det. |

## Phase 4 — L3 patch-markering (~336k tokens, 2-3 dage)

**Forudsætning:** Phase 3 = GO eller NO-GO med L3-konvertering.

**Scope:** 42 filer (eller flere hvis Phase 3 eskalerede) får `CEREBRO-PATCH(<navn>):` markører + dokumenteres i `docs/cerebro-patches.md`.

Lint-reglen er allerede aktiveret i Phase 0 — Phase 4 er hvor vi systematisk markerer alle eksisterende patches.

### Phase 4 done-criteria

- [ ] Alle markører tilføjet
- [ ] `docs/cerebro-patches.md` har én entry pr unik patch-navn
- [ ] `scripts/validate-cerebro-patches.sh` rapporterer alle markører fundet
- [ ] Total kodelinjer i patches dokumenteret (mål ≤200, eskaler hvis >300)

### Phase 4 go/no-go

| Resultat | Action |
|---|---|
| **GO** — alle markører + registry komplette | Committer videre til Phase 5 |
| **HOLD** — registry mangler enkelte entries | Fyld manglende ind |
| **NO-GO** — total patch-linjer >300 | Strategi virker ikke — for meget inline-modifikation. Tilbage til Phase 3 for genanalyse |

## Phase 5 — Sync-validation (~500k tokens, 1-2 dage)

**Forudsætning:** Phase 4 = GO. Hele refactoren landet i main.

**Scope:** Kør første rigtige `git merge upstream/main` og mål reel reduceret konfliktflade.

```
git checkout main && git pull
git checkout -b chore/upstream-sync-validation
git fetch upstream
git merge upstream/main || true
scripts/upstream-sync.sh --resolve
scripts/validate-cerebro-patches.sh
make check
pnpm exec playwright test
```

### Phase 5 go/no-go (det endegyldige test)

| Måling | GO | HOLD | NO-GO |
|---|---|---|---|
| Konfliktfiler | <15 | 15-30 | >30 |
| Konflikt-linjer total | <50 | 50-150 | >150 |
| Eval-suite | Alle grønne | 1-2 fejl | 3+ fejl |
| Visual regression cerebro-features | 0 diff | 0 diff | Diff |
| `make check` | Grøn | Med advarsler | Fejl |

**GO:** Land merge til main. Refactor er færdig. Etablér rutine: månedlig sync.
**HOLD:** Phase 6 fix specifikke problemer; re-kør Phase 5.
**NO-GO:** Phase 6 systematisk genanalyse; muligvis tilbage til Phase 3 med justeret L2/L3-fordeling.

## Phase 6 — Iteration (open-ended)

**Scope:** Baseret på Phase 5-data:
- Hvis L2-wrappers viste sig fragile → konvertér problemfiler til L3
- Hvis L3-patches dublerer → identificér gentagende mønstre og lav L1-extension
- Hvis eval-suiten missede regressioner → udvid suite

## Reclassification-protokol (gælder alle faser)

Hvis en fil under Phase X opdages at høre i lag Y:

1. Stop arbejdet på den fil
2. Luk eksisterende sub-task som "reclassified"
3. Opret ny task i den korrekte fase
4. Opdater `01-audit.md` med ny klassifikation + rationale
5. Opdater `02-assessment.md` hvis fordeling ændres signifikant
6. Genoptag arbejdet i den korrekte fase

## Samlet timeline + cost

| Fase | Tokens | Status-condition |
|---|---|---|
| -1 — Preflight | 400k | Alle 8 antagelser verificeret |
| 0 — Foundation | 300k | CI grøn, feature flag virker, lint håndhæver |
| 1 — L1 ekstraktion | 700k | 13 features flyttet + 7 renames, eval grøn |
| 2 — Inbox feature flag | 250k | Begge varianter virker, toggle persisterer |
| 3 — L2 wrappers | 900k | 14 wrappers, hver eval-gated |
| 4 — L3 patches | 336k | 42 markører + registry |
| 5 — Sync-validation | 500k | Real merge med <15 konflikter |
| Eval-suite (fra 04-evals.md) | 600k | Phase 0 baseline + per-fase gates |
| Buffer (eval-fix, regressioner) | 400k | |
| **Total** | **~4,4M tokens** | |

## Done-condition for hele initiativet

- [ ] Phase 5 kørt mod et rigtigt nyt upstream/main: konfliktflade <15 filer, manuel resolution <2 timer
- [ ] `make check` grøn
- [ ] Eval-suite grøn (cerebro-features uændrede, upstream-features opdaterede som forventet)
- [ ] `docs/cerebro-patches.md` har komplet registry
- [ ] CLAUDE.md har discipline-regel
- [ ] Lint-regel håndhæver disciplin
- [ ] Feature flag-system styrer alle cerebro-features
- [ ] Månedlig sync-rutine etableret
- [ ] Team-disciplin: nye PRs lander automatisk i cerebro-zone

## Management summary

**Hvad:** Vi flytter alle cerebro-specifikke ændringer ud af upstream-multica-filerne og ind i en separat `cerebro-*` zone, beskyttet af feature flags og håndhævet af lint-regler. Dermed kan vi pull'e upstream-opdateringer regelmæssigt uden konflikt-helvede.

**Hvorfor:** Upstream multica har 306 commits ud over os — masser af værdi vi vil have. Naive merge giver 201 konflikter. Med refactor reduceres det til <15 filer pr sync.

**Hvad det koster:** ~4,4M tokens (engangs) fordelt over 7 faser, hver med klar test-gate og eksplicit go/no-go-kriterium. Cirka svarende til 18-22 fokuserede AI-assisterede arbejdsdage.

**Hvad det sparer:** Hver upstream-sync går fra ~1,8M til ~250k tokens — 7× billigere. Investeringen tjener sig ind efter 2-3 sync; med kvartalsvis cadence er det tilbagebetalt på ~6 måneder.

**Hvad er risikoen:** Se `05-risks.md` for komplet register. Hovedrisikoen er at refactor introducerer adfærdsregressioner; mitigeres med eval-suite (`04-evals.md`) der kører før og efter hver fase. Hver fase har eksplicit go/no-go så vi opdager problemer tidligt.

**Beslutning der skal træffes:** OK at starte Phase -1 (preflight)? Det er det billigste skridt der validerer fundamentalt om planen virker.
