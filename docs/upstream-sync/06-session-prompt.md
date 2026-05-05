# Step 6 — Kickoff prompt for fresh session

Brug denne prompt til at starte en ny Claude-session der kører hele upstream-sync-refactor i mål.

## Sådan bruger du den

1. Start en ny Claude Code-session i `/Users/hvejsel/firtal-repos/firtal-cerebro` (main checkout)
2. Indsæt prompten nedenfor som første besked
3. Sessionen vil arbejde sig igennem alle faser med eksplicit go/no-go ved hver gate
4. Hvis du får brug for at pause: `Ctrl+C` på det aktuelle skridt; sessionen kan genoptages med en simpel "fortsæt"

---

## START PROMPT (copy fra her og ned)

```
Du skal eksekvere upstream-sync-refactor for firtal-cerebro repo'et.
Hele planen ligger i `docs/upstream-sync/` (worktree: `../firtal-cerebro-upstream-sync`).
Læs dem alle i denne rækkefølge inden du starter:

1. `docs/upstream-sync/00-discovery.md` — what we know about the codebase
2. `docs/upstream-sync/01-audit.md` — file-by-file classification (L0-L3)
3. `docs/upstream-sync/02-assessment.md` — risk og feasibility-analyse
4. `docs/upstream-sync/03-decision.md` — fasen-plan (Phase -1 til 6)
5. `docs/upstream-sync/04-evals.md` — eval-strategi pr fase
6. `docs/upstream-sync/05-risks.md` — risk register

KONTEKST DU SKAL VIDE:
- Det er en hard fork af multica-ai/multica der ikke kan påvirke upstream
- Vi har 120 commits divergens; upstream har 306 commits divergens
- Naive merge giver 201 konflikter — vi vil reducere til <15
- Cerebro-namespace er bekræftet (præfiks `cerebro-*`)
- Docs (multica's dokumentation) tager vi altid upstream's version af
- Inbox bliver feature-flag-toggled (default cerebro), ikke direkte erstatning
- Worktree til arbejdet: `../firtal-cerebro-upstream-sync` (chore/upstream-sync-analysis branch)

DIN OPGAVE:
Eksekver Phase -1 (Preflight) → Phase 0 → Phase 1 → Phase 2 → Phase 3 → Phase 4 → Phase 5
i nævnte rækkefølge. Ved hver fase-grænse stopper du, præsenterer go/no-go-data,
og venter på min eksplicitte "GO" før du fortsætter.

REGLER:
1. Læs ALTID hele fase-beskrivelsen i 03-decision.md før du starter en fase
2. Hver phase har eksplicit done-criteria — verificér ALLE før du markerer fasen færdig
3. Eval-gates fra 04-evals.md skal passere før phase-overgang
4. Hvis en risiko fra 05-risks.md materialiserer sig, eskalér til mig — beslut ikke selv at deviere fra planen
5. Alle ændringer landes som atomic commits med konventionelle commit-beskeder
6. Hver fase landes som én eller flere PRs til main (ikke direkte commits)
7. Pre-commit hooks må IKKE skippes (--no-verify forbudt)
8. Hvis du finder fejl i planen mens du eksekverer, opdater dokumenterne og notér ændringen
9. Brug TaskCreate til at tracke faser og sub-tasks; opdater status undervejs
10. Brug worktree (`../firtal-cerebro-upstream-sync`) til alt eksperimentelt arbejde;
    main-checkouten skal forblive ren

START MED:
- Verificér plan-dokumenterne eksisterer og er læsbare
- Tjek at vi er på den rigtige branch (`chore/upstream-sync-analysis` i worktree)
- Tjek at upstream remote stadig peger på multica-ai/multica
- Bekræft alle forudsætninger fra 03-decision.md "Forudsætninger"-sektion
- Vis mig en kort statusrapport (max 10 linjer) før du starter Phase -1

KOMMUNIKATION:
- Dansk i alle stakeholder-svar
- Per fase: kort statusrapport (under 200 linjer markdown)
- Per gate: tabel med målinger + go/no-go-anbefaling
- Kode-output, error-traces og diff-output: paste direkte (ikke gentage tool-output)
- Repository-navn som sidste linje i hvert svar: `firtal-cerebro`

EVAL-DRIVEN VERIFICATION:
For hver fase:
1. Tag eval-baseline FØR ændringer (hvis ikke allerede gjort)
2. Lav ændringerne
3. Kør eval-suite
4. Sammenlign mod baseline
5. Hvis eval grøn → marker fase færdig + go/no-go-rapport
6. Hvis eval rød → identificér årsag, fix, re-kør evals

NÅR DU SIDDER FAST:
- Hvis du tvivler på en beslutning → spørg, deviere ikke fra planen
- Hvis en risiko fra 05-risks.md materialiserer → eskalér med specifik reference
- Hvis token-budget overskrides med 50%+ → pause + rapportér
- Hvis 3+ evals fejler i samme fase → stop + rapportér

START NU. Læs alle 6 plan-filer, vis statusrapport, og afvent min GO før Phase -1.
```

---

## Hvad sessionen forventes at producere

Per fase:

**Phase -1 output:**
- 8 verifikationsrapporter i `docs/upstream-sync/preflight/<P-N>.md`
- Sammenfattende `docs/upstream-sync/preflight/SUMMARY.md`
- Beslutningsanmodning: GO/HOLD/NO-GO

**Phase 0 output:**
- Cerebro-zone-skelet committed til main (gennem PR)
- Feature flag-system implementeret + Settings-tab
- Lint-regel aktiveret
- `scripts/upstream-sync.sh` + `scripts/validate-cerebro-patches.sh`
- CLAUDE.md opdateret
- Eval-baselines gemt
- Beslutningsanmodning: GO/HOLD/NO-GO

**Phase 1 output:**
- 13 features flyttet til cerebro-zone (PR pr feature)
- 7 pakker renamed til cerebro-prefix (PR pr pakke)
- Eval-suite grøn efter hver flytning
- Beslutningsanmodning: GO/HOLD/NO-GO

**Phase 2 output:**
- Inbox feature flag-pattern fungerer
- Begge varianter (cerebro/upstream) renderable
- Eval-suite for inbox grøn
- Beslutningsanmodning: GO/HOLD/NO-GO

**Phase 3 output:**
- 14 wrappers implementeret (eskaleret hvor nødvendigt)
- Hver wrapper med eval-bevis
- Beslutningsanmodning: GO/HOLD/NO-GO

**Phase 4 output:**
- 42+ CEREBRO-PATCH markører tilføjet
- `docs/cerebro-patches.md` registry komplet
- Beslutningsanmodning: GO/HOLD/NO-GO

**Phase 5 output:**
- Real upstream/main merge kørt
- Konfliktmålinger rapporteret
- Eval-suite grøn på merged branch
- Endelig GO/HOLD/NO-GO til at lande merge

## Pause/resume-protokol

Hvis sessionen skal stoppes midt i en fase:

1. Sessionen committer alt urørt arbejde til arbejds-branch
2. Sessionen opdaterer task-list med præcis status
3. For at genoptage: ny session med prompten "Fortsæt upstream-sync hvor du slap. Læs status i `docs/upstream-sync/SESSION-STATE.md`"

Sessionen skal vedligeholde `docs/upstream-sync/SESSION-STATE.md` med:
- Aktuel fase
- Sub-task i gang
- Næste planlagte skridt
- Blockers eller eskaleringer

## Validation før session starter

Før du indsætter prompten, verificér:

```bash
cd /Users/hvejsel/firtal-repos/firtal-cerebro
ls docs/upstream-sync/  # skal vise 00-06
git branch -a | grep chore/upstream-sync-analysis
git remote -v | grep upstream
```

Forventet output:
- Alle 7 plan-filer (00-06) eksisterer
- Branch `chore/upstream-sync-analysis` eksisterer (i worktree)
- Remote `upstream` peger på `https://github.com/multica-ai/multica.git`

Hvis noget mangler — fix det FØR du starter sessionen.

## Forventet samlet runtime

- Phase -1: 1-2 dage AI-assisteret arbejde (~400k tokens)
- Phase 0: 1 dag (~300k tokens)
- Phase 1: 3-5 dage (~700k tokens)
- Phase 2: 1-2 dage (~250k tokens)
- Phase 3: 5-7 dage (~900k tokens)
- Phase 4: 2-3 dage (~336k tokens)
- Phase 5: 1-2 dage (~500k tokens)
- Buffer + evals: ~1M tokens

**Total realistisk:** 3-4 ugers fokuseret AI-assisteret arbejde fordelt over kalendertid (kan strækkes hvis pauser).

## Beslutninger der allerede er truffet (sessionen skal ikke spørge om disse)

- Cerebro-namespace bekræftet (`cerebro-*` prefiks)
- Docs = multica's dokumentation, tag altid upstream's version
- Inbox via feature flag (default-on cerebro)
- Cerebro-migrationer i `9NNN_*` namespace, separat mappe
- Lint-regel håndhæver CEREBRO-PATCH disciplin fra Phase 0
- Feature flag-system bruger eksisterende `extraAccountTabs` prop på SettingsPage
- Allerede-isolerede pakker (artifacts, attachments, notifications, members) renames i Phase 1

## Beslutninger sessionen SKAL spørge om

- Phase -1 NO-GO (3+ antagelser fejler) → revurdér strategi
- Phase 5 NO-GO (>30 konfliktfiler eller systemiske evals fejler) → tilbage til Phase 3
- Total token-forbrug overstiger 6M (50% over budget) → stakeholder-input
- Materialiseret risiko fra 05-risks.md med Høj-impact → stakeholder-input

Alt andet kan sessionen tage selv inden for planens rammer.
