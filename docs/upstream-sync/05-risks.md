# Step 5 — Risk Register

Eksplicit risiko-register med sandsynlighed × impact og konkret mitigation pr risiko. Reviewes ved hvert fase-skifte.

## Skala

**Sandsynlighed:**
- **Lav** — usandsynligt baseret på discovery
- **Medium** — vurderet 1-50% chance
- **Høj** — sandsynligt at ske mindst én gang under projektet

**Impact:**
- **Lav** — kan håndteres uden at standse projektet
- **Medium** — kræver re-arbejde af én fase
- **Høj** — kræver fundamental revurdering af strategi

## Tekniske risici

### R1 — TypeScript module augmentation virker ikke for upstream-typer

**Sandsynlighed:** Lav (modul-augmentation er stabil og bredt brugt)
**Impact:** Høj (rammer hele L1 type-extension-strategien)
**Triggers:** Upstream skifter fra `interface` til `type` for typer vi augmenter
**Mitigation:**
- Phase -1 verifikation P1 fanger dette FØR commitment
- Fallback hvis P1 fejler: brug separate cerebro-typer (`CerebroProject extends Project`) i stedet for augmentation
**Detection:** `pnpm typecheck` rød

### R2 — Path-alias-shadowing virker ikke ensartet på tværs af bundlers

**Sandsynlighed:** Medium (Next.js + Turbopack + Electron-vite har hver sin config)
**Impact:** Medium (rammer L2 wrapper-strategien)
**Triggers:** Forskellige bundlers fortolker tsconfig paths forskelligt
**Mitigation:**
- Phase -1 P2 verificerer i begge apps før Phase 3
- Fallback: explicit re-export fra cerebro-pakke + import fra cerebro-sti i app-koden
**Detection:** Build fejler, eller komponent fra forkert pakke rendres

### R3 — Migrations konflikter alligevel ved merge

**Sandsynlighed:** Lav (per F1-discovery: tracking via fuld filnavn, ikke nummer)
**Impact:** Medium (kræver manuel migrations-håndtering)
**Triggers:** Upstream tilføjer migration der **ændrer** en kolonne vi har tilføjet
**Mitigation:**
- Idempotente migrationer (per F8) gør re-kør sikker
- Cerebro-migrationer i `9NNN_*` namespace + separat mappe
- Pre-merge: kør `make migrate-up` på frisk DB som eval-gate
**Detection:** Migration fejler eller giver inkonsistent schema

### R4 — Wrap-pattern bryder ved upstream API-change

**Sandsynlighed:** Medium (sker mindst én gang om året)
**Impact:** Medium (kræver re-design af de berørte wrappers)
**Triggers:** Upstream ændrer required props, fjerner exports, omdøber komponenter
**Mitigation:**
- Compile-fail fanges af `make check` ved merge-tid (ikke runtime)
- L2 wrappers er begrænset til 14 filer → reparation er afgrænset
- Eskalér til L3 hvis wrap er for fragil
**Detection:** TypeScript-fejl efter merge

### R5 — Bundle-size vokser >10%

**Sandsynlighed:** Lav (composition tilføjer minimal kode)
**Impact:** Medium (perf-regression for slutbruger)
**Triggers:** Wrappers dublerer logik; cerebro-pakker har egne dependencies
**Mitigation:**
- Phase 0 baseline + per-fase check af `pnpm build` size
- Tree-shaking verifikation
- Budget: max 5% vækst på initial-load JS
**Detection:** Bundle-size-test rapporterer overskridelse

### R6 — Sqlc multi-source virker ikke

**Sandsynlighed:** Lav (sqlc.yaml understøtter multiple input)
**Impact:** Medium (rammer cerebro-server-strategien)
**Triggers:** sqlc opfører sig anderledes end forventet med to mapper
**Mitigation:**
- Phase -1 P3 verificerer
- Fallback: behold cerebro-queries i `server/pkg/db/queries/cerebro_*.sql` (samme mappe, prefiks-filtreret)
**Detection:** `make sqlc` fejler eller producerer forkert kode

## Process-risici

### R7 — CEREBRO-PATCH markører glemmes

**Sandsynlighed:** Høj (mennesker glemmer)
**Impact:** Lav (fanges senere ved næste sync)
**Triggers:** Hurtig PR uden code-review-tid
**Mitigation:**
- Lint-regel håndhæver fra Phase 0 — PR der bryder reglen merges ikke
- Pre-commit-hook fanger lokalt
- Code-review checklist
**Detection:** Lint-jobbet rødt

### R8 — Refactor kolliderer med igangværende feature-arbejde

**Sandsynlighed:** Høj (5+ åbne branches per F5-discovery)
**Impact:** Medium (re-baseline arbejde)
**Triggers:** Open PR landes mid-refactor uden cerebro-disciplin
**Mitigation:**
- Disciplin-bekendtgørelse Phase 0 før Phase 1 starter
- PR-template kræver cerebro-zone for nye features
- Lint-regel + pre-commit-hook
- Freeze-vindue for ikke-cerebro PR'er under Phase 1-3 (afhængig af team-størrelse)
**Detection:** Re-merge konflikt mod refactor-branch

### R9 — Phase 5 viser konfliktflade ikke reduceret nok

**Sandsynlighed:** Medium (vi har ikke gjort dette før)
**Impact:** Høj (al refactor-investering ineffektiv)
**Triggers:** L2/L3-fordeling forkert estimeret; nye konfliktklasser opstår
**Mitigation:**
- Phase 5 har eksplicit go/no-go-kriterium
- Phase 6 (iteration) er planlagt — ikke en bug men en feature
- Reclassification-protokol (per 03-decision.md)
**Detection:** Phase 5 målinger >15 filer eller >50 linjer

### R10 — Eval-suite er ikke dækkende nok

**Sandsynlighed:** Medium (vi tilføjer 65 nye tests; kan ikke garantere 100% coverage)
**Impact:** Medium (regression slipper igennem til prod)
**Triggers:** Edge-case adfærd ikke testet; mobil-specifikke regressioner; race conditions
**Mitigation:**
- Visual regression fanger DOM-ændringer
- API contract tests fanger felter
- Manuel smoke-test efter hver fase
- Rollback-strategi pr fase
**Detection:** User report eller monitoring efter deploy

## Strategiske risici

### R11 — Inbox feature flag introducerer state-bug

**Sandsynlighed:** Medium (toggle midt i session kan skabe stale state)
**Impact:** Medium (degraderet UX, ikke datatab)
**Triggers:** WebSocket connection forventer én inbox-format men UI er anden
**Mitigation:**
- E2E test toggle-flow eksplicit
- Default-on i prod = brugere ser cerebro-version som standard
- Toggle kræver page-reload (dokumenteret i flag-tab)
**Detection:** Console errors eller broken UI ved toggle

### R12 — Sandbox-tests fejler på Linux CI

**Sandsynlighed:** Sikker (sandbox er macOS-specifik per F9)
**Impact:** Lav (kun CI rød; mitigation kendt)
**Triggers:** Tests kører platform-uafhængigt
**Mitigation:**
- Eksplicit `test.skip(process.platform !== 'darwin')`
- Eller separat macOS-only CI-job
**Detection:** CI rødt på Linux runners

### R13 — Type-extensions bryder ved upstream type-rename

**Sandsynlighed:** Medium
**Impact:** Medium
**Triggers:** Upstream omdøber `Project` → `ProjectV2`
**Mitigation:**
- Compile-fail fanges af `make check`
- Manuel re-augmentation = 5-10 minutters arbejde
**Detection:** TypeScript-fejl efter merge

### R14 — Refactor tager længere tid end estimeret

**Sandsynlighed:** Høj (estimater er altid for optimistiske)
**Impact:** Medium (delay; ikke fundamentalt problem)
**Triggers:** Komplekse wrappers; uforudsete edge cases
**Mitigation:**
- Hver fase har sin egen eval-gate; vi opdager forsinkelse tidligt
- Buffer i token-budget (400k)
- Faserne kan pausses og genoptages — det er ikke et big-bang
**Detection:** Token-forbrug overstiger estimat med 50%+

### R15 — Cerebro-features divergerer fra upstream's tilsvarende features

**Sandsynlighed:** Høj (det er hele pointen ved at have cerebro-zone)
**Impact:** Lav-Medium (ikke et problem, men skal vedligeholdes bevidst)
**Triggers:** Upstream forbedrer sin notifications → vores cerebro-notifications står stille
**Mitigation:**
- Periodisk gennemgang af upstream-changelog for relevante forbedringer
- Cherry-pick interessante upstream-commits ind i cerebro-zonen manuelt
- Acceptér at cerebro = anderledes, ikke nødvendigvis bedre eller værre
**Detection:** User feedback om manglende features

## Risk heat-map

```
          Lav impact     Medium impact     Høj impact
Høj       R7, R15        R8, R14
Medium                   R4, R10, R11      R9
Lav       R12            R3, R5, R6, R13   R1, R2
```

**Største bekymring:** R9 (Phase 5 fejler) — høj impact, medium sandsynlighed. Mitigation = Phase -1 preflight reducerer sandsynlighed; eksplicit go/no-go reducerer impact.

**Mindre bekymring:** R7 og R15 er sikre men selv-håndterende — lint-regel + bevidst vedligeholdelse.

## Continuous monitoring

Re-vurder dette register ved hvert fase-skifte:

1. Var en risiko der materialiserede? Opdater status.
2. Opdagede vi nye risici? Tilføj.
3. Er en planlagt mitigation utilstrækkelig? Eskaler.

Risiko-registeret er et levende dokument.

## Acceptance

Stakeholder-godkendelse af risikoprofilen kræves før Phase -1 starter:

- [ ] Alle risici læst og forstået
- [ ] Mitigation-strategier accepteret
- [ ] Tolerance for R9 (Phase 5 fejler) bekræftet — vi er villige til at investere ~3M tokens før vi ved om strategien holder
- [ ] Tolerance for R8 (refactor-team-konflikt) — vi accepterer freeze-vindue eller koordinationsoverhead
