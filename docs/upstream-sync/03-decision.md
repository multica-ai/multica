# Step 3 — Decision

Konkret faseplan baseret på `01-audit.md` og `02-assessment.md`. Hver fase har præcis scope, eval-gate og rollback-strategi.

## Forudsætninger

Før Phase 0 starter:

1. **Stakeholder-beslutninger:**
   - [ ] Inbox-replacement: bekræft at upstream's inbox-arbejde ikke skal flyde til os automatisk
   - [ ] Docs: bekræft at vi forlader docs-merge og altid tager upstream's version
   - [ ] Cerebro-namespace: bekræft `cerebro-*` som package-prefix og `internal/cerebro/` som server-path

2. **Tekniske forudsætninger:**
   - Eval-suite-baseline kørt og snapshots gemt (se `04-evals.md` Phase 0)
   - `chore/upstream-sync-analysis` branch eksisterer som arbejds-base
   - Worktree på `../firtal-cerebro-upstream-sync` aktiv

## Phase 0 — Foundation (engangs, ~250k tokens)

**Scope:**
- Opret tom cerebro-zone struktur (mapper + README'er)
- Tilføj lint-regel: PR der modificerer upstream-fil uden `CEREBRO-PATCH:` markør fejler
- Skriv `scripts/upstream-sync.sh` der auto-resolver L0
- Skriv `scripts/validate-cerebro-patches.sh` der grep'er alle markører og verificerer
- Tilføj `docs/cerebro-patches.md` (initialt tom registry)
- Opdater `CLAUDE.md` med additive-first-regel
- Eval-suite Phase 0: baseline-capture (se `04-evals.md`)

**Done-criteria:**
- [ ] `scripts/upstream-sync.sh --dry-run upstream/main` kører grønt
- [ ] `scripts/validate-cerebro-patches.sh` rapporterer 0 markører (intet er flyttet endnu)
- [ ] Eval-baseline er gemt i `e2e/baselines/pre-refactor/`
- [ ] CLAUDE.md har "## Cerebro extension discipline" sektion
- [ ] PR mod main lander grønt

**Rollback:** revert PR. Phase 0 er additivt — ingen eksisterende kode rørt.

## Phase 1 — L1 ekstraktion: rene additive features (~700k tokens)

**Scope:** Flyt 13 filer (eksklusiv inbox-page) til cerebro-zonen.

Rækkefølge (mindst risiko først):

1. **cerebro-test** — flyt `apps/web/test/helpers.tsx` cerebro-bits, `daemon_test.go` cerebro-tests, `claude_test.go` cerebro-tests, `tasks-tab.test.tsx`, `client.test.ts`. Risiko: lavest — kun tests rammer.
2. **cerebro-users** — flyt `packages/views/members/index.ts` til `packages/cerebro-users/`. Members-feature.
3. **cerebro-notifications** — flyt `notifications-tab.tsx` (ny fil) + listeners + handlers. Tab registreres via context-pattern.
4. **cerebro-mcp** — flyt MCP install guide-additions.
5. **cerebro-realtime** — flyt event-handlers + 1-line registration.
6. **cerebro-runtime/sandbox/profile/cli** — server-side ekstrakter fra `daemon/config.go`, `cli/client.go`, `notification_listeners.go`.
7. **cerebro-api** — eksponér sub-client `cerebroApi.*` mountet i `client.ts` (3-line patch).

**Done-criteria pr punkt:**
- [ ] Filer flyttet til ny sti, gamle stier fjernet
- [ ] Imports opdateret
- [ ] `pnpm typecheck` grøn
- [ ] `make test` grøn
- [ ] Eval Phase 1-gate: alle baseline-scenarier passerer (se `04-evals.md`)
- [ ] Manuel browser-test af berørt feature (se eval-checklist)

**Rollback pr punkt:** hver flytning er sin egen PR. Revert er sikker.

**Konflikt-flade efter Phase 1:** 107 → 93 filer. Men endnu ingen sync mulig før alle faser kører.

## Phase 2 — L1 inbox-replacement (~250k tokens, isoleret)

**Scope:** Flyt vores inbox-page til `packages/cerebro-inbox/` og route-shadow så cerebro's version vises i stedet for upstream's.

**Stakeholder-bekræftelse skal foreligge** før denne fase starter (per Phase 0 forudsætning).

**Done-criteria:**
- [ ] `packages/cerebro-inbox/inbox-page.tsx` indeholder vores 1198-linje implementering
- [ ] Web-route `/inbox` peger på cerebro-version
- [ ] Desktop-route ligeså
- [ ] Eval: alle inbox-baseline-scenarier passerer (folders, archive, multi-select, mark-read)
- [ ] Visual regression check viser identisk DOM/screenshot

**Rollback:** route peger igen på upstream's inbox-page. Vores cerebro-inbox forbliver i pakke men ubrugt.

## Phase 3 — L2 composition wrappers (~900k tokens)

**Scope:** Konvertér 12 filer (15 - 3 der flyttes til L3 per assessment) til wrapper-pattern.

Rækkefølge (mindst koblet først):

1. `comment-card.tsx` → `cerebro-comments/comment-card.tsx` (helper-extraction + wrap)
2. `runtime-detail.tsx` → `cerebro-runtime/runtime-detail.tsx` (sandbox-toggle slot)
3. `project-detail.tsx`, `project-picker.tsx` → `cerebro-access/*`
4. `chat-input.tsx`, `chat-message-list.tsx` → `cerebro-mcp/*`
5. `issue-detail.tsx`, `agent-live-card.tsx`, `list-row.tsx`, `reply-input.tsx` → `cerebro-issues/*`
6. `editor/readonly-content.tsx` → `cerebro-ui/readonly-content.tsx` (CSS override + wrap)
7. `tasks-tab.tsx` → `cerebro-users/tasks-tab-wrap.tsx`
8. `settings-page.tsx` → tab-registration via context

**Done-criteria pr wrapper:**
- [ ] Wrapper-komponent eksisterer i cerebro-pakke
- [ ] Upstream-fil restaureret til upstream-state (vores diff = 0)
- [ ] Routes/imports opdateret til at bruge wrapper
- [ ] `pnpm typecheck` grøn
- [ ] Eval: features der bruger denne komponent passerer baseline
- [ ] Visual regression check identisk

**Eskaleringskriterium:** hvis en wrapper viser sig at koste >150 linjer eller dublere mere end 30% af upstream-komponentens logik → drop wrapping for den fil og flyt til L3 i stedet.

**Rollback pr wrapper:** restaurér upstream-fil til vores forrige state, fjern wrapper-import.

## Phase 4 — L3 patch-markering (~336k tokens)

**Scope:** For 42 filer: tilføj `CEREBRO-PATCH(<navn>):` markør foran hver inline-modifikation. Dokumentér i `docs/cerebro-patches.md`.

**Format:**
```go
// CEREBRO-PATCH(project-access-fields): expose access on response
Color    *string `json:"color"`
RepoURL  *string `json:"repo_url"`
Access   string  `json:"access"`
```

**Done-criteria:**
- [ ] Alle 42 filer har markører
- [ ] `docs/cerebro-patches.md` har én entry pr unik patch-navn med:
  - Filsti
  - Hvad patchen gør
  - Hvorfor det ikke kan være additivt eller composition
- [ ] `scripts/validate-cerebro-patches.sh` rapporterer alle markører fundet
- [ ] Total kodelinjer i patches ≤200

**Rollback:** Ingen — markører er kommentarer, kan altid fjernes/redigeres.

## Phase 5 — Sync-validation (~500k tokens)

**Scope:** Kør første rigtige upstream/main merge på en ren sync-branch og mål reel reduceret konfliktflade.

**Steps:**
1. Opret `chore/upstream-sync-validation` branch fra main (efter Phase 1-4 er landet)
2. `git fetch upstream && git merge upstream/main`
3. Kør `scripts/upstream-sync.sh --auto-resolve`
4. Kør `scripts/validate-cerebro-patches.sh`
5. Resolve resterende konflikter manuelt
6. Kør komplet eval-suite mod den merged branch
7. Sammenlign mod baseline

**Done-criteria:**
- [ ] Konfliktflade <15 filer (vs 201 oprindeligt)
- [ ] Konflikt-linjer total <50 linjer
- [ ] Alle eval-scenarier passerer
- [ ] Visual regression: 0 forskelle på cerebro-features
- [ ] Visual regression: forventede forskelle på upstream-features (de er opdaterede)
- [ ] `make check` grøn
- [ ] Manuel smoke-test i browser bekræfter cerebro-features virker

**Rollback:** abort merge. Vi har lært noget om hvor planen er svag og itererer i Phase 6.

## Phase 6 — Iteration (open-ended)

**Scope:** baseret på Phase 5-data:
- Om L2-wrappers viste sig fragile → konvertér problemfiler til L3
- Om L3-patches dublerer → identificér gentagende mønstre og lav L1-extension
- Om eval-suiten missede regressioner → udvid suite

## Samlet timeline + cost

| Fase | Tokens | Status-condition |
|---|---|---|
| 0 — Foundation | 250k | CI grøn |
| 1 — L1 ekstraktion | 700k | 13 features flyttet, eval grøn |
| 2 — Inbox-replacement | 250k | Inbox kører på cerebro-route |
| 3 — L2 wrappers | 900k | 12 wrappers, hver eval-gated |
| 4 — L3 patches | 336k | 42 markører + registry |
| 5 — Sync-validation | 500k | Real merge med <15 konflikter |
| Buffer (eval-fix, regressioner) | 400k | |
| **Total** | **~3,3M tokens** | |

## Eskaleringskriterier (hvornår vi stopper og revurderer)

- Phase 1 viser at en "additiv" feature faktisk har skjulte koblinger → revurder L1-listen
- Phase 3 første 5 wrappers tager hver >150k tokens → wrap er for dyr; flyt mere til L3
- Phase 5 viser konfliktflade >30 filer → strategi virker ikke som forventet; ny analyse
- Eval finder regressioner i 3+ uafhængige steder → behavioral preservation er for svær; pause refactor

## Done-condition for hele initiativet

Refactor er færdig når:
- [ ] Phase 5 kørt mod et rigtigt nyt upstream/main: konfliktflade <15 filer, manuel resolution <2 timer
- [ ] `make check` grøn
- [ ] Eval-suite grøn (cerebro-features uændrede, upstream-features opdaterede som forventet)
- [ ] `docs/cerebro-patches.md` har komplet registry
- [ ] CLAUDE.md har discipline-regel
- [ ] Lint-regel håndhæver disciplin

Næste step: `04-evals.md`.

## Management summary

**Hvad:** Vi flytter alle cerebro-specifikke ændringer ud af multica-upstream-filerne og ind i en separat `cerebro-*` zone, så vi kan trække upstream-opdateringer ind regelmæssigt uden konflikt-helvede.

**Hvorfor:** Upstream multica har 306 commits ud over os — masser af værdi vi vil have. Naive merge giver 201 konflikter. Med refactor reduceres det til <15 filer pr sync.

**Hvad det koster:** ~3,3M tokens engangs LLM-arbejde fordelt over 6 faser, hver med klar test-gate. Cirka svarende til 12-15 fokuserede AI-assisterede arbejdsdage.

**Hvad det sparer:** Hver upstream-sync går fra ~1,8M til ~250k tokens — 7× billigere. Investeringen tjener sig ind efter 2-3 sync; med kvartalsvis cadence er det tilbagebetalt på ~6 måneder.

**Hvad er risikoen:** Hovedrisikoen er at refactor introducerer adfærdsregressioner i kerneflows. Mitigeres med eval-suite (se `04-evals.md`) der kører før og efter hver fase og sammenligner snapshots.

**Beslutning der skal træffes nu:** OK at commit'e til alle 6 faser, eller skal vi køre Phase 0 + 1 først som proof-of-concept og revurder?
