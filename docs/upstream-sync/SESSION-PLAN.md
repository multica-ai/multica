# Session-plan: upstream-merge test

**Worktree:** `/Users/hvejsel/firtal-repos/firtal-cerebro-merge-test`
**Branch:** `test/upstream-merge-2026-W19`
**Udgangspunkt:** `chore/upstream-sync-analysis @ d106fb61`
**Hovedrepo (ikke rør):** `/Users/hvejsel/firtal-repos/firtal-cerebro` (på `main`)

## Hvorfor en separat worktree?

Production rammes først hvis nogen `git push origin main`. Denne worktree er
isoleret fra hovedrepoet på filsystem-niveau, har sin egen database, og sin
egen branch. Hvis merge-testen smadrer alt: `git worktree remove` fjerner
hele oplevelsen og hovedrepoet er urørt.

## Situations-rapport (2026-05-06)

To grene har bevæget sig siden vores arbejde startede:

**`origin/main` (firtal-cerebro)** — ~30 nye commits siden vores branch
divergerede. De vigtige overlappende features:

| Ny feature på main | Konflikter med vores |
|---|---|
| `feat(access): project-level access control (Phases 1-8)` + `combined restrict+pick flow + red keylock indicator` | 🔴 `cerebro-access`-pakken — vi har egen implementation |
| `feat(notifications): channel-first datamodel + resolvers (P0)` | 🔴 `cerebro-notifications` + cerebro-listeners-patches |
| `feat(channels): kind-conditional issue-detail + /channels/{id} route` | 🟡 `comment-card-cerebro` + `issue-detail-cerebro` patches |
| `feat(server): channels — API handlers + access + tests` | 🟡 server-side cerebro-access |
| `Skill ownership, versioning, change requests + forks (JEH-216)` | 🟡 `cerebro-skills` hvis nogen patches |
| PWA + Web Push notifications (#59, #62, #63, #71, #76, #86) | 🟢 nye filer, lav risiko |
| Mobile sweeps på sidebar/inbox/issue-breadcrumb | 🟡 shared views vi har patches på |

**`upstream/main` (multica)** — ~50 nye commits. Notable:

- `feat(server): redis-backed runtime liveness with DB fallback`
- `feat(chat): support deleting chat sessions` + History panel
- `fix(daemon)` family: heartbeat, session resume, isolated polls
- `fix(execenv)`: refuse to write `.gc_meta.json` when issue_id empty
- `feat(cli): --assignee-id / --to-id / --user-id targeting`
- `fix(views): split desktop/mobile sidebar state in project-detail`

## Konsekvens

Vi har **TO merge-jobs**, ikke ét:

1. **Sync vores branch op til `origin/main`** — løs konflikter mellem
   vores cerebro-arbejde og firtal-cerebro's nye features.
2. **Merge `upstream/main` ind** — løs cerebro-vs-multica konflikter.

Vores `scripts/upstream-sync-resolve.sh` hjælper KUN med job 2.

## Plan for denne session

### Fase 1: Sync med origin/main (forventet 2-4 timers arbejde)

1. **Inspect og rapporter overlap** — for hver af de røde rækker ovenfor,
   diff vores arbejde mod origin/main's version. Skriv resultat til
   `docs/upstream-sync/origin-sync-overlap.md`.

2. **Beslutnings-punkt**: for hver overlappende feature, vælg én af:
   - **Take origin's version + drop vores** (hvis origin's er bedre/identisk)
   - **Take ours + drop origin's** (hvis vi har domain-specific cerebro-logic)
   - **Merge by hand** (begge har værdi)

3. **Merge origin/main ind i vores branch**: `git merge origin/main`
   - Forventet: konflikt-filer i overlappende områder
   - Ingen auto-resolve script for dette job — alt manuelt

4. **Verifikation efter origin-merge**:
   - `make check` grøn
   - Browser-smoke test af de overlappende features (project access,
     notifications, issue-detail, mobile views)
   - Hvis noget breaker: stop og rapporter

### Fase 2: Upstream/main merge (forventet 2-3 timer)

5. **Auto-resolve pass 1**: `bash scripts/upstream-sync-resolve.sh --apply`
   - Forventet: ~107 konflikter løst (docs/i18n purge + DD)

6. **Manuel resolve af de 4 trivielle filer**:
   - `server/pkg/db/queries/agent.sql` + `issue.sql`
   - `packages/core/package.json` + `packages/views/package.json`

7. **Auto-resolve pass 2**: `--apply` igen
   - Forventet: ~7 yderligere løst (sqlc-genererede + pnpm-lock)

8. **Manuel resolve af resterende ~80 filer**:
   - Disse er de ægte cerebro-vs-upstream content-konflikter
   - For hver: åbn fil, vurder cerebro-patch vs upstream-evolution,
     beslut behold/tag-deres/merge
   - Brug `// CEREBRO-PATCH(...)` markers som breadcrumbs

### Fase 3: Verifikation (forventet 1-2 timer)

9. **`make check` skal være grøn** (typecheck + tests + cerebro-patches)
10. **Browser smoke-test** af alle cerebro-features:
    - Settings → Cerebro features (toggle persist)
    - Settings → Agent Profile (save/load)
    - Project Access (Open ↔ Restricted)
    - Workspace kill-switch
    - Inbox folders
    - Notification routing
    - Comment-card (cerebro patches)
    - Issue-detail (cerebro extras)
11. **Production-build sanity** (ikke kørt før):
    - `pnpm build` (web)
    - `pnpm --filter @multica/desktop build` (electron)
12. **E2E test** (skipped i tidligere eval):
    - `pnpm exec playwright test`

### Fase 4: Land-beslutning

13. **Hvis alle 1-12 er grøn**: åbn PR fra `test/upstream-merge-2026-W19`
    til `main`. Brug squash-merge eller merge-commit efter brugerens
    præference.

14. **Hvis noget fejler**: rapporter præcist hvilken fase + hvilken fil +
    hvilken fejltype. Bruger beslutter om vi forsøger fix eller revertere
    branchen.

## Fixer der allerede ligger på `chore/upstream-sync-analysis`

Disse er medbragt fra base-branchen — du behøver ikke gøre noget for at
få dem:

- **RSC-fix** på `packages/cerebro-feature-flags/index.tsx` — fixer
  `/settings`-siden der var brudt på main før dette
- **Auto-resolve script** `scripts/upstream-sync-resolve.sh` (107 + 7
  auto-resolved konflikter)
- **Master-code dev-helper** (`MULTICA_DEV_MASTER_CODE` env-var)
- **Test-fix** på `auth_master_code_test.go` (env-bleed)
- **CLAUDE.md-update** med browser smoke-test target rule
- **Phase 6 file relocations** (37 filer i 5 nye cerebro-pakker)

## Sikkerhed

**Hvad må du gøre i denne worktree:**
- Alt: merge, slet filer, rebase, force-checkout, `make reset-db` osv.

**Hvad du IKKE må:**
- `git push` til `origin/main` (kun via PR efter eksplicit grønt lys fra
  brugeren)
- `git push --force` til ANY remote-branch
- Modificere ANY fil i `/Users/hvejsel/firtal-repos/firtal-cerebro` (det
  er hovedrepoet på main)
- Slette eller modificere `/Users/hvejsel/firtal-repos/firtal-cerebro-upstream-sync`
  worktree (det er stadig live for evt. follow-up)

## Hvis ALT går galt

```bash
# Fra hovedrepoet
cd /Users/hvejsel/firtal-repos/firtal-cerebro
git worktree remove --force ../firtal-cerebro-merge-test
git branch -D test/upstream-merge-2026-W19

# Slet ny database
docker exec -it $(docker ps -q --filter ancestor=pgvector/pgvector:pg17) \
  psql -U multica -c "DROP DATABASE IF EXISTS <new-worktree-db-name>;"
```

Hovedrepo + upstream-sync-worktree + production = stadig 100 % intakt.

## Kontekst-filer at læse FØRST i ny session

For at forstå hvad der allerede er gjort og hvorfor:

1. `docs/upstream-sync/COMPLETION-REPORT.md` — Phase 1-5 outcome
2. `docs/upstream-sync/SESSION-STATE.md` — full state log
3. `docs/upstream-sync/auto-resolve-test-20260506.md` — script verification
4. `CLAUDE.md` "Cerebro Extension Discipline" sektion (regler 1-4)
5. **DENNE FIL** (du læser den nu)

## Start-kommando til ny session

```bash
cd /Users/hvejsel/firtal-repos/firtal-cerebro-merge-test
claude
```

Første prompt til Claude:

> Læs `docs/upstream-sync/SESSION-PLAN.md` og start med fase 1. Vi er
> i merge-test-worktreen. Hovedrepoet på `main` må ikke røres. Kør
> Fase 1 trin 1 først (inspect og rapporter overlap mellem vores
> arbejde og origin/main's nye features) og stop der så jeg kan
> bekræfte beslutningerne i trin 2 før vi merger.

## Estimeret total tid

- Fase 1 (origin/main sync): 2-4 timer
- Fase 2 (upstream merge): 2-3 timer
- Fase 3 (verifikation): 1-2 timer
- Fase 4 (land): 30 min hvis grønt, ellers længere

**Total: 5-9 timer aktivt arbejde.** Man kan opdele over flere sessioner.
