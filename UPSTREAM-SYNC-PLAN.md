# Upstream Sync — Plan og målte tal

**Worktree:** `../firtal-cerebro-upstream-sync`
**Branch:** `chore/upstream-sync-analysis`
**Upstream:** `https://github.com/multica-ai/multica.git`
**Mergebase:** `4ce3e5dd fix(auth): hand off session to Desktop when web is already logged in (#1364)`

## Målt skadebillede (fra reel test-merge)

| Metrik | Tal |
|---|---|
| Vores commits siden mergebase | 120 |
| Upstream commits siden mergebase | 306 |
| Filer ændret begge steder med konflikt | **201** |
| - heraf docs/MD-content | 94 |
| - heraf kode | 107 |

## Konflikt-buckets (kode, 107 filer)

### Bucket A — Auto/mechanical (18 filer)
Regenererbare eller config-niveau. Resolves med scripts, ikke per-fil-vurdering.
- `pnpm-lock.yaml`, `package.json` × 2 → `pnpm install` regenererer
- `server/pkg/db/generated/*.sql.go` × 6 → `make sqlc` regenererer
- `server/pkg/db/queries/*.sql` × 2 → manuel merge → så `make sqlc`
- `.gitignore`, `.env.example`, `Makefile`, `docker-compose.selfhost.yml`, `.github/workflows/ci.yml`, `scripts/install.sh`, `scripts/init-worktree-env.sh`

### Bucket B — Docs-arkitekturkonflikt (17 kodefiler + 94 content-filer = 111)
**Én beslutning** (vores commit `f28c9fdb refactor(docs): merge apps/docs into apps/web at /docs route`) skaber ~55% af alle konflikter. Upstream har holdt `apps/docs/` separat og fortsat udviklet det.

**Tre valg:**
1. **Behold vores merge** — accepter at hele upstream-`apps/docs/`-arbejdet skal manuelt portes hver gang (~111 filer pr sync)
2. **Rul vores merge tilbage** — gå tilbage til `apps/docs/` separat. Engangs-cost, derefter ~0 docs-konflikter pr sync
3. **PR vores docs-merge upstream** — gratis fremover hvis de accepterer, men usikkert udfald

### Bucket C — Feature-integration (71 filer, det reelle arbejde)

| Område | Filer | Vores feature | Strategi |
|---|---|---|---|
| `server/internal/handler/` | 10 | access, members, enforcement, MCP-ændringer | Ekstrahér til `server/internal/firtal/` + 1-line hooks |
| `packages/views/issues/` | 8 | mobile, comment-card, agent-live-card mods | Wrap eller PR upstream — flere er pure UX-fixes |
| `server/internal/daemon/` | 6 | sandbox, profile, MCP | Isolér til `daemon/firtal_*.go` filer |
| `server/pkg/agent/` | 5 | claude.go, copilot, cursor, gemini mods | Få linjer pr fil — tag konflikter manuelt |
| `server/cmd/server/` | 4 | router, listeners — feature-wiring | 1-line hook per route i `firtal_routes.go` |
| `packages/core/types/` | 4 | type-extensions | Ekstrahér til `packages/core/firtal-types/` |
| `packages/views/runtimes/` | 3 | runtime-detail, list, utils | Wrap-komponenter |
| `packages/views/projects/` | 3 | access control kerne | **Allerede dybt koblet** — kræver mest refactor |
| `packages/views/chat/` | 3 | chat-window, input, message-list | UX-mods — kandidat til upstream-PR |
| `packages/views/agents/` | 3 | tasks-tab + tests | Wrap eller upstream-PR |
| Diverse single files | 22 | inbox, sidebar, settings, etc | Per-fil vurdering |

**Top-10 mest modificerede konfliktfiler** (vores commit-dybde):
```
20  server/pkg/db/generated/models.go        (regenereres)
20  packages/core/api/client.ts
19  server/cmd/server/router.go
13  packages/views/inbox/components/inbox-page.tsx
11  server/internal/handler/daemon.go
11  packages/views/settings/components/settings-page.tsx
11  packages/views/issues/components/issue-detail.tsx
10  apps/desktop/src/renderer/src/routes.tsx
 9  packages/views/layout/app-sidebar.tsx
 9  packages/core/types/index.ts
```

Disse er de **arkitekturelle hot-spots**. Hvis de ikke isoleres, betaler vi prisen ved hver upstream-merge.

---

## Token-cost-vurdering (LLM-driven konfliktløsning)

Antagelser: typisk konflikt-fil = 2-5k input til at læse begge sider + markers, 0,5-3k output til rewrite. Refactor-til-additivt = 30-80k pr fil (læs, design, dual-rewrite, test).

### Engangs-cost ved naiv merge nu (alle konflikter løses inline, ingen refactor)

| Bucket | Filer | Tokens/fil | Sub-total |
|---|---|---|---|
| A — Auto/mechanical | 18 | ~0 (scripts) | ~0 |
| B — Docs (kode) | 17 | 8k | 136k |
| B — Docs (content) | 94 | 2k | 188k |
| C — Feature (let, ≤3 vores commits) | ~35 | 10k | 350k |
| C — Feature (medium, 4-9 commits) | ~25 | 20k | 500k |
| C — Feature (tung, ≥10 commits) | ~11 | 50k | 550k |
| Tests + desktop + misc | 7 | 15k | 105k |
| **Engangs-merge total** | | | **~1,8M tokens** |

### Refactor-til-additivt (Trin 5 fra forrige plan)

| Aktivitet | Filer | Tokens/fil | Sub-total |
|---|---|---|---|
| Ekstrahér access control til `firtal-access` | ~12 | 60k | 720k |
| Ekstrahér members/enforcement til `firtal-users` | ~8 | 50k | 400k |
| Ekstrahér MCP/profile/sandbox til `firtal-runtime` | ~10 | 50k | 500k |
| Ekstrahér types til `firtal-types` | ~4 | 30k | 120k |
| Server-hooks-design (`server/internal/firtal/`) | ~15 | 60k | 900k |
| Backout eller isolér docs-merge | ~17 | 30k | 510k |
| Tests + verification | ~10 | 40k | 400k |
| **Refactor total** | ~76 | | **~3,5M tokens** |

### Cost pr fremtidig upstream-sync

| Strategi | Tokens pr sync | Antal sync til breakeven |
|---|---|---|
| Naiv (gør intet, merge hver gang) | ~1,5-2M | — |
| Med refactor (kun 1-line-hooks tilbage) | ~150-300k | ~2-3 sync |
| Hybrid (kun bucket B løses, behold C som inline) | ~800k-1,2M | ~5-7 sync |

**Konklusion på tokens:** refactor-investeringen (~3,5M) tjener sig ind efter **2-3 upstream-merges**. Givet at upstream lavede 306 commits siden vores base — dvs ~50/måned — er vi i refactor-betaler-sig-ind-territorium efter første kvartal.

---

## Anbefalet sekvens

### Fase 0 — Setup (engangs, ~50k tokens)
- [x] Tilføj upstream-remote
- [x] Branch + worktree til analyse
- [x] Mål konfliktflade
- [ ] Tilføj `multica-ai/multica` til `.git/config` på `main`-checkout også
- [ ] Tilføj `additive-first`-konvention til `CLAUDE.md`

### Fase 1 — Lav skaden mindre før første rigtige merge (~3,5M tokens)

**1a. Beslut docs-arkitektur** (~510k tokens hvis vi backer ud)
Vores `apps/docs → apps/web/docs` merge er den enkeltbeslutning der koster mest. Vurdér: backout, behold, eller PR upstream. Min anbefaling: backout — det er en kosmetisk forbedring der koster ~111 filer pr sync.

**1b. Ekstrahér access control til `packages/firtal-access/` + `server/internal/firtal/access/`** (~720k + 400k = 1,1M tokens)
Den dybest koblede feature. Største ROI.

**1c. Ekstrahér members/enforcement til `packages/firtal-users/` + `server/internal/firtal/users/`** (~400k tokens)
Allerede ret selvstændig — billig at flytte.

**1d. Server-hooks-skelet** (~900k tokens)
Etablér `server/internal/firtal/` med klare hook-points i upstream-handlers, så fremtidig kobling sker via 1-linjes-kald.

**1e. Type-ekstrakt** (~120k tokens)
Flyt vores type-tilføjelser til `packages/firtal-types/` med extension-interfaces.

### Fase 2 — Første rigtige merge (~300-500k tokens efter Fase 1)
Når bucket B er løst og bucket C reduceret til hooks, er hver upstream-merge under 500k tokens.

### Fase 3 — Disciplin (løbende, gratis)
- Ugentlig `git fetch upstream && git merge upstream/main` på `chore/upstream-sync-YYYY-WW`
- PR-template kræver: nye filer i `firtal-*/` eller `internal/firtal/` medmindre eksplicit begrundet
- Schema-migrationer i `server/migrations/firtal/` med separat seriel nummerering

---

## Done-condition

Plan er gennemført når:
1. `git merge upstream/main` på en frisk branch giver **<10 konflikter** der alle er trivielle (1-3 linjers hook-kald)
2. Refactor-arbejdet er landet i `main` via PRs uden adfærdsregression (e2e + Go tests grønne)
3. `CLAUDE.md` har additive-first-reglen
4. Første rigtige sync er kørt igennem inden for 500k tokens

---

## Management summary (dansk)

**Hvad:** Vi forbereder firtal-cerebro så vi kan trække upstream-Multica-ændringer ind regelmæssigt uden hver gang at bruge dage på konfliktløsning.

**Hvorfor:** Vi har 120 commits ud over upstream, upstream har 306 commits ud over os, og en naiv merge giver 201 konflikter. Det vil kun blive værre over tid.

**Hvad det koster (engangs):** ~3,5 millioner tokens i LLM-arbejde (svarer til ~10-15 dages dedikeret AI-assisteret refactor) til at flytte vores ændringer til selvstændige `firtal-*/` mapper og isolere upstream-touch til 1-linjes hooks.

**Hvad det sparer:** Hver fremtidig upstream-merge går fra ~1,8M tokens til ~300k tokens — en 6× reduktion. Investeringen tjener sig ind efter 2-3 merges.

**Den ene store beslutning:** Vores docs-merge (`apps/docs` → `apps/web/docs`) skaber over halvdelen af konflikterne. Anbefaling: rul den tilbage. Det er en kosmetisk forbedring vi betaler dyrt for.

**Næste skridt:** Vælg om vi går efter den fulde refactor-vej eller den hybride. Hvis ja, starter vi med docs-backout og access-control-ekstraktion.
