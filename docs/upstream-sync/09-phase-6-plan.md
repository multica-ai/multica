# Phase 6 — Plan: flyt 126 fork-filer til cerebro-zoner

**Status:** Ikke startet.
**Forudsætning:** Ingen. Phase 6 sker PÅ samme sidegren som Phase 1-5 (chore/upstream-sync-analysis). Intet skal til main før hele featuren er færdig + testet + bekræftet.

**Land-rækkefølge (eksplicit):**
1. Phase 6 — flyt de 126 filer
2. Test merge mod upstream — konflikt-tal skal være acceptabelt
3. Manuel funktionstest af apps (web + desktop)
4. Bruger bekræfter at alt virker
5. **Først derefter** land til main

## Hvorfor Phase 6 overhovedet?

Phase 5 (real merge mod upstream) producerede 201 konflikt-filer vs <15 mål. Hovedårsagen: vores fork har ~126 helt nye filer som ligger i mapper der "tilhører" upstream (`packages/views/`, `packages/core/`, `server/internal/handler/`, `server/cmd/multica/`). De konflikter ikke i streng forstand (upstream har ikke tilsvarende filer), men de tæller med i vores diff og forurener disciplin-checks.

Når de er flyttet til cerebro-zoner (`packages/cerebro-*/`, `server/internal/cerebro/*/`), forsvinder de fra konflikt-listen. Forventet: konflikter falder fra 201 til ~154 filer — stadig højt, men nu er det **kun ægte ændringer** i upstream-filer.

## Hvad skal flyttes?

126 net-new fork-filer som per chunk 11-subagentens analyse ligger forkert. Den eksakte liste skal regenereres ved Phase 6-start sådan her:

```bash
# Find filer der findes i fork men ikke i upstream
git diff --name-only --diff-filter=A upstream/main...HEAD \
  | grep -vE '^(packages/cerebro-|server/internal/cerebro/|server/migrations/cerebro/|docs/|scripts/cerebro|\.github/.*upstream-zone)' \
  | grep -E '^(packages/(views|core|ui)|server/(internal/handler|cmd|pkg))/'
```

Eksempler på områder vi ved skal flyttes (fra subagent-analyse):
- artifact-feature filer i `packages/views/artifacts/` og `packages/core/artifacts/` — meget er allerede flyttet i chunk 4, men der er rester
- profile-feature filer
- MCP install guide UI
- inbox-folder logik
- work-sessions
- project-access UI komponenter (project-access-tab, RestrictedLock osv. — pure cerebro)
- budget enforcement
- user-profile customizations
- runtime-setup tokens

## Approach

Mekanisk arbejde, samme mønster som chunk 4 renames. Per fil:

1. `git mv <upstream-path>/<file> packages/cerebro-<feature>/<file>` (history bevaret)
2. Opdatér `cerebro-<feature>/package.json` exports + deps
3. Find consumere via `grep -rn "<old-import-path>" apps/ packages/`
4. Opdatér imports
5. Verificér typecheck

Gruppér i logiske batches (alle artifact-rester sammen, alle profile sammen, osv.). Brug 3-4 parallelle subagenter per chunk, samme protokol som chunk 4.

## Estimat

- 5-8 iterationer
- ~1.5-2M tokens (vi har ~3.3M tilbage af de 7M budget)
- Wall-clock: 4-6 timer med parallelle subagenter

## Eval-kriterier

Per chunk:
- `pnpm typecheck` grøn (jeg ved at imports skal opdateres samtidig — manglende imports fanges af typecheck)
- `pnpm test` grøn
- `make test` grøn (Go side)
- `scripts/per-session-eval.sh` grøn

Phase 6 done-criterium:
- `git merge upstream/main` på en ny test-branch giver **<50 konflikt-filer**
- Det er målet — hvis vi rammer <50, er strategien tilbage på sporet (originalplanens <15 var urealistisk givet hvor meget forken har divergeret)

## Risici

- **Imports er spredte**: en fil kan importeres fra mange steder (apps/web, apps/desktop, packages/views, packages/core). Subagenter kan misse nogle. Mitigeres ved typecheck efter hver flytning.
- **Cykliske afhængigheder**: hvis cerebro-feature A flyttes og bruger cerebro-feature B, og B flyttes senere, kan der opstå midlertidige cykler. Subagenter skal arbejde "leaves first" — flyt mest-isolerede filer først.
- **Audit-fejl**: chunk 4 + chunk 6 fandt at audit-dokumentet (01-audit.md) var unøjagtigt flere steder. Phase 6's første skridt skal være at GENERATE den faktiske liste, ikke stole på audit-tabellen.

## Bonus-rensning (lav under Phase 6 hvis tid tillader)

Mens vi alligevel rører i koden:
- `apps/web/test/helpers.tsx` har **nul consumere** — død fil. Slet.
- `packages/views/agents/components/tabs/tasks-tab.test.tsx` er upstream-ejet (kun 1-tegns ellipsis-tweak fra fork). Revertér den ene tegnsændring.
- Cli/client.go (audit-fejlklassifikation): er ikke cobra-kommandoer, det er en HTTP-klient. Marker er allerede tilføjet — opdatér audit hvis vi reviewer.
- `daemon_test.go` orphan-task-test og `claude_test.go` ring-buffer-tests: depender på private fixtures, kan ikke ekstraheres. Markers er allerede der. Behold som-er.

## Når Phase 6 er kørt

- Forventet konfliktflade ved næste upstream-sync: ~50-150 filer
- Sync-cadence-mål skal re-baselines: original plan sagde <15 filer/kvartal, realistisk er nu 50-100
- Følg disciplin: nye cerebro-features skal ALTID lande i cerebro-zoner. Lint-guard rejicerer upstream-zone-modifikationer uden CEREBRO-PATCH markører.

## Hvis du kommer til denne fil i en ny session

1. Læs også `docs/upstream-sync/COMPLETION-REPORT.md` for at se hvor Phase 1-5 endte
2. Læs `docs/upstream-sync/SESSION-STATE.md` for status (skal være `COMPLETED` efter Phase 5)
3. Kør den filsøge-kommando under "Hvad skal flyttes?" for at få den aktuelle liste (filer kan have ændret sig siden 2026-05-05)
4. Start ny branch: `git checkout main && git checkout -b chore/upstream-sync-phase-6`
5. Brug samme /loop-mønster som Phase 1-5 hvis bruger vil have autonom kørsel
