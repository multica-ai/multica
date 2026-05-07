# Step 7 — Session schedule

Reference for hvilke chunks der laves i hvilken rækkefølge. Bruges af både human-paced execution (én session pr chunk) og autonom loop (flere chunks pr session, paused ved boundaries).

## Chunk-skema

| # | Fase | Scope | Tokens | Auto-progress? | Decision-point efter |
|---|---|---|---|---|---|
| 1 | Phase -1 | Alle 8 preflight-eksperimenter (parallelle subagenter) | 400k | NEJ — pause for user GO | Strategy validated |
| 2 | Phase 0a | Cerebro-zone-skelet + READMEs + lint-regel + sync-script | 200k | JA hvis evals grøn | |
| 3 | Phase 0b | Feature flag-system + Settings-tab + eval-baselines + CLAUDE.md | 300k | JA hvis evals grøn | Foundation done |
| 4 | Phase 1a | Rename de 7 isolerede pakker | 200k | JA hvis evals grøn | |
| 5 | Phase 1b | Flyt cerebro-test + cerebro-users + cerebro-notifications | 250k | JA hvis evals grøn | |
| 6 | Phase 1c | Flyt cerebro-mcp + cerebro-realtime + cerebro-runtime + cerebro-api | 300k | JA hvis evals grøn | Phase 1 done |
| 7 | Phase 2 | Inbox feature flag end-to-end | 250k | **NEJ** — user pause før start | Inbox toggle works |
| 8 | Phase 3a | Wrappers: comment-card + runtime-detail + project-detail + project-picker | 250k | JA hvis evals grøn | |
| 9 | Phase 3b | Wrappers: chat-input + chat-message-list + issue-detail + agent-live-card | 250k | JA hvis evals grøn | |
| 10 | Phase 3c | Wrappers: list-row + reply-input + readonly-content + tasks-tab + agents-page | 300k | JA hvis evals grøn | Phase 3 done |
| 11 | Phase 4 | Tilføj 42 CEREBRO-PATCH markører + dokumentér registry | 336k | JA hvis evals grøn | Markers done |
| 12 | Phase 5 | Real upstream/main merge + mål konfliktflade | 500k | **NEJ** — user pause før start | **Final GO/NO-GO** |
| 13 | Phase 6 (kontingent) | Iteration efter Phase 5 | varies | JA hvis Phase 5 HOLD | |

**Total: ~3,5M tokens, breakeven efter 2-3 sync.**

## Per-chunk brief

Hvert chunk har et selvstændigt brief som autonomous loop læser:

### Chunk 1 — Phase -1 Preflight

**Scope:** Eksekvér alle 8 verifikationspunkter (P1-P8) fra `03-decision.md`.

**Strategy:** Dispatch 8 parallelle subagenter (én pr verifikation), aggregér rapporter til `docs/upstream-sync/preflight/SUMMARY.md`.

**Done når:**
- 8 rapporter eksisterer i `docs/upstream-sync/preflight/<P-N>.md`
- SUMMARY.md har GO/HOLD/NO-GO recommendation
- SESSION-STATE.md opdateret med phase_minus_1.go_no_go

**Auto-progression:**
- GO → kan auto-fortsætte til Chunk 2 hvis user-pause flag ikke sat
- For nu: user-pause er sat for Phase -1 → STOP og afvent user

### Chunk 2 — Phase 0a Foundation (skelet + lint + scripts)

**Scope:**
- Opret cerebro-zone mappe-skelet med READMEs
- Implementér lint-regel (eslint custom rule + go vet check)
- Skriv `scripts/upstream-sync.sh`
- Skriv `scripts/validate-cerebro-patches.sh`
- Skriv `scripts/per-session-eval.sh`

**Done når:**
- Mapper eksisterer
- Lint-regel fanger en test-violation
- Scripts kan køres med --help
- `pnpm typecheck` grøn

**Auto-progression:** GO → fortsæt til Chunk 3.

### Chunk 3 — Phase 0b Foundation (feature flags + evals)

**Scope:**
- Implementér feature flag-system (`packages/cerebro-feature-flags/`)
- Tilføj Settings-tab via `extraAccountTabs` prop
- Capture eval-baselines (visual + API contract + performance)
- Opdatér CLAUDE.md med disciplin-sektion
- Land Foundation som PR til main

**Done når:**
- Feature flag toggleable in UI
- Baselines capture'd i `e2e/__snapshots__/`
- CLAUDE.md opdateret
- PR oprettet med foundation

**Auto-progression:** GO → fortsæt til Chunk 4 (Phase 1).

### Chunk 4 — Phase 1a Rename isolated packages

**Scope:** Per `01-audit.md` rename:
- `packages/core/artifacts/` → `packages/cerebro-artifacts/core/`
- `packages/core/attachments/` → `packages/cerebro-attachments/core/`
- `packages/core/notifications/` → `packages/cerebro-notifications/core/`
- `packages/views/artifacts/` → `packages/cerebro-artifacts/views/`
- `packages/views/attachments/` → `packages/cerebro-attachments/views/`
- `packages/views/notifications/` → `packages/cerebro-notifications/views/`
- `packages/views/members/` → `packages/cerebro-users/`

**Done når:**
- Alle 7 mapper renamed
- Alle imports opdateret
- `pnpm typecheck` + `make test` + `make check` grøn

**Auto-progression:** GO → Chunk 5.

### Chunk 5 — Phase 1b Move features (test + users + notifications)

**Scope:** Flyt L1-content til de cerebro-pakker der eksisterer fra Chunk 4. Subagenter parallelt.

**Done når:** 3 features migreret, evals grønne.

**Auto-progression:** GO → Chunk 6.

### Chunk 6 — Phase 1c Move features (mcp + realtime + runtime + api)

**Scope:** Resterende L1-content. Subagenter parallelt.

**Done når:** 4 features migreret, alle Phase 1 evals grønne.

**Auto-progression:** GO til Chunk 7? **NEJ** — Phase 2 er user-pause.

### Chunk 7 — Phase 2 Inbox feature flag

**Scope:** Vores 1198-linje inbox-implementering flyttes til `packages/cerebro-inbox/`. Routes vælger via `cerebro_inbox` flag.

**Done når:**
- `packages/cerebro-inbox/inbox-page.tsx` indeholder vores version
- Web + desktop routes vælger via flag
- Default = on
- E2E inbox-tests grønne på BEGGE varianter

**Auto-progression:** GO → Chunk 8.

### Chunks 8-10 — Phase 3 Wrappers

**Scope:** 13 wrappers, fordelt 4+4+5. Subagenter parallelt inden for hver chunk.

**Eskaleringskriterium:** Hvis 3+ wrappers koster >150 linjer eller dublerer >30% af upstream-logik → drop wrapping for resterende, flyt til L3.

**Auto-progression efter Chunk 10:** GO → Chunk 11.

### Chunk 11 — Phase 4 Mark patches

**Scope:** 42 filer får `CEREBRO-PATCH(<navn>):` markører. Dokumentér i `docs/cerebro-patches.md`. Subagenter parallelt (6 agents × 7 filer hver).

**Done når:** Alle markører + registry komplet, validate-script rapporterer alle fundet.

**Auto-progression:** GO til Chunk 12? **NEJ** — Phase 5 er user-pause.

### Chunk 12 — Phase 5 Sync validation

**Scope:** Real upstream/main merge på chore/upstream-sync-validation branch.

**Done når:** Merge complete med <15 konfliktfiler, eval-suite grøn på merged branch.

**Output:** GO/HOLD/NO-GO til at lande merge til main. **Bruger lander selv** — autonomous loop committer ikke til main uden eksplicit instruks.

## User-pause-punkter

Autonomous loop SKAL stoppe og afvente bruger på disse 3 punkter:

1. **Efter Chunk 1 (Phase -1)** — strategy validation, kan ikke auto-decide
2. **Før Chunk 7 (Phase 2)** — inbox replacement er signifikant arkitekturbeslutning
3. **Før Chunk 12 (Phase 5)** — real merge er stor irreversibel handling

På alle andre punkter må loopet auto-progress hvis evals er grønne.

## Eval-gates pr chunk

Hvert chunk SKAL passere gate før status sættes til "completed":

```bash
# L1 Smoke (efter hvert commit i chunk):
pnpm typecheck
pnpm exec vitest run <changed-pkg>

# L2 Feature (efter hver task i chunk):
pnpm exec playwright test e2e/<feature>.spec.ts

# L3 Gate (chunk-end):
scripts/per-session-eval.sh <chunk-id>
# Outputs pass/fail table
# All pass → chunk completed
# Any fail → retry up to 3x, then escalate
```

## Recovery hvis chunk fejler

```yaml
on_chunk_failure:
  attempt_1: retry_with_revert
  attempt_2: retry_with_revert
  attempt_3:
    update_state: status=PAUSED, pause_reason=eval_failure_3x
    write_diagnostic: docs/upstream-sync/failures/<timestamp>.md
    stop_loop: true
```

## Hvordan loop'et bruger dette skema

Autonomous-loop læser `SESSION-STATE.md` for at finde aktuel chunk, læser den specifikke chunk-brief her, eksekverer, opdaterer state, beslutter om auto-progress eller pause.
