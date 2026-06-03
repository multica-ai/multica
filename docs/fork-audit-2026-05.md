# Fork-audit 2026-05 — firtal-cerebro vs upstream

**Issue:** [FIR-2173](mention://issue/119675f2-71cb-4c0d-9b58-edc783d47d72)
**Mål:** Permanent reduktion af fork-overflade. Hver in-place ændring i en upstream-fil bliver til konfliktrisiko ved næste merge. Vi vil flytte, slette eller upstream'e så meget som muligt, så fremtidige merges bliver lette i sig selv — ikke kun via bot ([FIR-2169](mention://issue/a4c50337-ddbb-4725-8dfb-02efece952f3)) og catch-up ([FIR-2171](mention://issue/0aee1c0e-0780-4c43-898b-dc140a70b80e)).

**Måling:** 682 commits foran `upstream/main` (35 bagud). 2.363 filer er rørt totalt — 594 modificerede upstream-filer, 1.332 helt nye filer i fork-zoner, 427 slettede.

**Status:** Audit + prioriteret backlog. Selve eksekvering (delete/PR/refactor) sker i follow-up issues efter Jespers go/no-go.

> **Hvor hænger denne audit sammen med tidligere arbejde?**
> - `docs/upstream-sync/01-audit.md` + `02-assessment.md` + `03-decision.md` har allerede defineret refactor-strategien (cerebro-zoner, L0/L1/L2/L3-lag). Den plan er **godkendt**, men ikke fuldt eksekveret. Denne audit gentager ikke planen — den prioriterer og udvider med tre nye vinkler:
>   1. **Upstream-PR-kandidater** (var ikke i 01-audit's scope — den fokuserede på "hvordan merger vi", ikke "hvad kan vi give tilbage")
>   2. **Slet-kandidater** (revert'ed / superseded / dead code der stadig bærer fork-vægt)
>   3. **Top-10 højeste-leverage** med konkret konflikt-reduktion ved næste merge
> - `docs/upstream-sync/origin-sync-overlap.md` (W19) er vores friskeste merge-konflikt-virkelighed — den vægter top-10 nedenfor.

---

## 1. Feature-bucket-oversigt (33 buckets dækker alle 682 commits)

Commits er grupperet efter feature, ikke kronologi. Hver bucket har én anbefaling. Tabellen er sorteret med højeste-leverage øverst (mest fork-overflade-reduktion ved næste merge).

| # | Bucket | Commits ca. | Status i koden | Anbefaling | Begrundelse |
|---|---|---|---|---|---|
| 1 | **API client extensions** (`packages/core/api/client.ts`) | ~30 | In-place +1.516 linjer på upstream-fil | **REFAKTOR** | Største enkelt-fil-konflikt. Skal splittes i `packages/cerebro-api-client/extensions.ts` med module-augmentation. |
| 2 | **Inbox-feature-set** (folders, archive, multi-select, reminders, group-by, mobile) | ~50 | Delvist flyttet til `packages/cerebro-inbox/`, men `inbox-page.tsx` og `inbox-list-item.tsx` er stadig in-place | **REFAKTOR (færdiggør)** | Phase 1 i `03-decision.md`. Vi ejer 100% af inbox UX — replace upstream-fil med en route-shadow. Fjerner +1.202 og +281 konfliktlinjer. |
| 3 | **Issue detail UI extensions** (agent-live-card, comment-threading, reference-filter, metadata-sidebar) | ~30 | In-place +980/-626 på `issue-detail.tsx` + 6 søsterfiler | **REFAKTOR** | Wrap-pattern (P7 i preflight). Allerede god kandidat fordi vi har stort surface (537 add-only på `agent-live-card.tsx` er rent vores). |
| 4 | **Cerebro-server-handlers** (`server/internal/handler/daemon.go`, `runtime.go`, `issue.go`, `workspace.go`, `agent.go`) | ~40 | In-place +1.800 linjer på upstream-handlers | **REFAKTOR** | Pak til `server/internal/cerebro/<feature>/handler.go` + 1-linje routing-patch (L2-mønster fra 02-assessment). |
| 5 | **Artifact / documents** (file manager, MCP-tools, viewer, folders, attachments) | ~30 | Allerede 100% i `packages/cerebro-artifacts/` + `server/internal/cerebro/artifacts/` | **BEHOLD** | Veldefineret, isoleret, ingen upstream-overlap. |
| 6 | **Workflows** (cerebro/workflows) | ~12 | `packages/cerebro-workflows/` | **BEHOLD** | Firtal-specifik orchestration. |
| 7 | **Groups** (agent groups) | ~12 | `packages/cerebro-groups/` | **BEHOLD** | Firtal-specifik. |
| 8 | **Channels** (channel-first routing) | ~14 | Delvis cerebro, men upstream skrev sit eget channels-system (origin-sync-overlap §2) | **BEHOLD + ALIGN** | Vores er færdig; upstream's er anderledes. Beslutning på næste merge: behold vores eller vedtag upstream's. |
| 9 | **Notifications-rewrite** | ~15 | Vi flyttede `packages/views/settings/.../notifications-tab.tsx` til cerebro; upstream skrev derefter en stor rewrite (origin-sync-overlap §1) | **SLET (vores) + take upstream** | Origin's 355 nye linjer er produkt-evolutionen. Vores L1-flytning var mekanisk. Take theirs, re-flyt efter merge. |
| 10 | **References** (issue references UI) | ~10 | `packages/cerebro-references/` | **BEHOLD** | Firtal-specifik. Modent. |
| 11 | **Access / Users / Permissions** (Phase 5 unified access, capability-register) | ~25 | `packages/cerebro-access/`, `packages/cerebro-permissions/`, `packages/cerebro-agent-capabilities/` | **BEHOLD** | Cerebro-produkt-defining. |
| 12 | **Profile** (user/agent-profile schema + runtime injection) | ~8 | `packages/cerebro-profile/` | **BEHOLD** | Cerebro-specifik. |
| 13 | **Approvals** (approval inbox for needs_approval verdicts) | ~6 | `packages/cerebro-approvals/` | **BEHOLD** | Cerebro-specifik. |
| 14 | **MCP-server + agent-tools** (Claude Code integration, list_comments, attachments) | ~12 | `packages/cerebro-mcp/` + `packages/cerebro-runtime-tools/` | **PR til upstream (delvist)** | MCP-server-skeleton er generel; firtal-specifikke tools beholdes. |
| 15 | **Cloud runtime launcher** (FIR-2152) | ~6 | In-place på `packages/views/runtimes/` (+335/-545) + `server/internal/handler/runtime.go` | **REFAKTOR + PR (delvist)** | Selve cloud-runtime-koncept kan upstream'es. UI-shell skal flyttes til `packages/cerebro-runtime/views/`. |
| 16 | **Runtime sandbox-policy enforcement** | ~5 | In-place på `server/internal/daemon/daemon.go` + execenv | **REFAKTOR** | Flyt til `server/internal/cerebro/sandbox/` med policy-hook. |
| 17 | **Skill ownership / versioning / forks** (JEH-216) | ~8 | In-place på `packages/views/skills/components/skill-detail-page.tsx` (+243) | **PR til upstream** | Generel value. Per origin-sync-overlap §7 er konflikten "grøn". Kandidat til at sende tilbage. |
| 18 | **Editor extensions** (prose-cap, readonly-content, image-view, attachments-render) | ~15 | In-place på `packages/views/editor/` (+261, +161) | **PR til upstream** | Editor-bug-fixes og generel UX. Kandidat. |
| 19 | **Mobile sweeps** (inbox push routing, swipe archive, task view, sidebar fixes) | ~20 | In-place på `apps/mobile/` (omfattende) | **PR til upstream (gradvist)** | Bug-fixes der gælder alle Multica-installs. Per origin-sync-overlap §5 er der allerede overlap. |
| 20 | **Autopilots** (webhook trigger, model override, dialog) | ~8 | In-place på `server/internal/service/autopilot.go` (+163) + `packages/views/autopilots/components/autopilot-dialog.tsx` | **REFAKTOR + PR (delvist)** | Model-override er generel. Webhook-trigger-UI er Firtal-orienteret men ren value. |
| 21 | **Chat session UI** (pinning, FAB hide, sessions tabs) | ~14 | Delvis in-place på `packages/views/chat/` (-283 på chat-window) | **REFAKTOR** | Wrap eller flyt til `packages/cerebro-chat/`. |
| 22 | **Sidebar customizations** (squads, selected agent URL, dupe nav, mobile close, project-color) | ~13 | In-place +445 på `app-sidebar.tsx` | **REFAKTOR** | Komponer via cerebro-ui slot-pattern. |
| 23 | **Settings tab additions** (workspace-tab, settings-page, notifications-tab routing) | ~10 | In-place +404 på `workspace-tab.tsx`, +198 på `settings-page.tsx` | **REFAKTOR** | Settings-page er klassisk slot-pattern. Lavt risiko at wrap. |
| 24 | **Projects extensions** (color, repo URL, MCP auto-detect, nested projects JEH-672) | ~8 | In-place på `projects-page.tsx`, `project-detail.tsx` | **PR til upstream (delvist) + REFAKTOR** | Color-picker og repo-URL er generel UX. Nested-projects er Firtal-domain. |
| 25 | **Onboarding v3 slides** | ~3 | `packages/views/onboarding/` | **BEHOLD** | Firtal-branded content. |
| 26 | **Dictation streaming** (JEH-729) | ~5 | `packages/cerebro-dictation/` | **BEHOLD** | Firtal-specifik. |
| 27 | **Credentials / Firtal Gateway / Infisical secrets** | ~10 | `packages/cerebro-credentials/` | **BEHOLD** | Firtal-specifik infra. |
| 28 | **Persona SDK** | ~4 | `packages/cerebro-persona-sdk/` | **BEHOLD** | Firtal-domain (helsebixen). |
| 29 | **Tasks-feature** (kolonnevælger, søgning, 24h default) | ~5 | `packages/cerebro-tasks/` | **PR til upstream (delvist)** | Tasks-grid-UX er generel; firtal-default-værdier holdes. |
| 30 | **Deploy / CI / build-system** (LaunchAgent, deploy.sh, goreleaser, friday-release, smoke-test) | ~25 | `.deploy/`, `.github/workflows/`, top-level scripts | **BEHOLD** | Vores deployment-måde — irrelevant for upstream. |
| 31 | **CLI extensions** | ~7 | `server/cmd/multica/` + `server/internal/cli/` | **REFAKTOR + PR (delvist)** | Notifications-list CLI er generel; artifact-folder-CLI er vores. |
| 32 | **Revert / cleanup / dead-code** (NotificationBell, ChatFab, MCPServersCard, folders-feature, dublet-fixes, role-toggles) | ~15 | Allerede revertet/fjernet — men reverts står som commits i loggen | **SLET fra historik (rebase-squash)** | Lav-værdi. Reverts gør git-blame støjende men ændrer ikke konfliktfladen. Kan vente. |
| 33 | **Docs (upstream-sync, eval-reports, CLAUDE.md, DEPLOY.md)** | ~20 | `docs/upstream-sync/` | **BEHOLD** | Vores driftsviden. Upstream må gerne se det. |

---

## 2. Top-10 højeste-leverage actions

Sorteret efter **konflikt-reduktion ved næste upstream-merge** (med vægtning fra `origin-sync-overlap.md`).

| Rang | Action | Konflikt-reduktion (estimat) | Eksekverings-cost | Risiko | Follow-up issue |
|---|---|---|---|---|---|
| **1** | Refaktor `packages/views/inbox/components/inbox-page.tsx` → `packages/cerebro-inbox/inbox-page.tsx` med route-shadow (replacement) | Fjerner #1 konflikt-fil (+1.202/-171). Garanterer 0 konflikter på inbox-UI for evigt. | Stor — ~3-5 dage. Phase 1.1 i 03-decision. | Medium — upstream's inbox-bug-fixes kommer ikke automatisk; cherry-pick manuelt. | TBD |
| **2** | Refaktor `packages/core/api/client.ts` → splittet i `packages/cerebro-api-client/extensions.ts` via module-augmentation (P1-mønster) | Fjerner #2 konflikt-fil (+1.516/-188). | Medium — ~2 dage. Berører hver pakke der importerer `apiClient`. | Lav — TS-typer fanger det meste. | TBD |
| **3** | Wrap `packages/views/issues/components/issue-detail.tsx` + søsterfiler via L2-composition (issue-detail-shell + slots) | Fjerner #3 konflikt-fil (+980/-626) + agent-live-card.tsx + comment-card.tsx + comment-input.tsx + reply-input.tsx | Stor — ~4 dage. Phase 1.3. | Medium-høj — wrap-pattern fejler hvis upstream ændrer props. P7-preflight har valideret det er muligt. | TBD |
| **4** | Flyt `server/internal/handler/daemon.go` + `runtime.go` + `issue.go` + `workspace.go` + `agent.go` til `server/internal/cerebro/<feature>/` med routing-patch | Fjerner 5 handler-filer fra konflikt-zonen (-2.000 linjer) | Stor — ~3 dage. L2-pattern. | Lav-medium — routing-patch er trivielt. | TBD |
| **5** | Take upstream's notifications-rewrite (origin-sync-overlap §1) — slet vores L1-flytning, re-flyt deres rewrite til cerebro-notifications efter merge | Eliminerer "rød" konflikt på 2 store filer; uden den her bliver næste merge dyr. | Lille — ~1 dag i merge-vinduet. | Medium — vi mister vores nuværende routing-shape. | TBD |
| **6** | Refaktor `packages/views/layout/app-sidebar.tsx` til slot-pattern via `packages/cerebro-ui/sidebar/` | Fjerner +445 in-place modifikation. | Medium — ~2 dage. | Lav. | TBD |
| **7** | Refaktor `packages/views/settings/components/workspace-tab.tsx` + `settings-page.tsx` til slot/registry-pattern (cerebro-settings-tabs) | Fjerner +404/+198 in-place modifikation. Nemt fordi settings allerede har tab-arkitektur. | Lille-medium — ~1-2 dage. | Lav. | TBD |
| **8** | PR til upstream: **editor-prose-cap (FIR-2114) + readonly-content + image-view fixes** | Fjerner 3 filer fra fork-surface hvis accepteret. | Lille — ~0.5 dag at lave PR. | Lav (men afhænger af acceptance). | TBD |
| **9** | PR til upstream: **skill ownership / versioning / forks (JEH-216)** | Fjerner +243 fra `skill-detail-page.tsx`. Per origin-sync-overlap §7 "grøn" overlap. | Medium — ~1 dag at sende PR. | Lav. | TBD |
| **10** | Slet revert-historik via squash-merge i en oprydnings-PR (NotificationBell, ChatFab, MCPServersCard, folders-feature) | Reducerer ikke konfliktflade men gør git-blame klart. Lav prioritet. | Lille. | Lav. | TBD |

**Samlet konflikt-reduktion ved Top-1 til Top-7:** ca. **8-12 af de værste konflikt-filer** fjernes permanent. Det er typisk 60-80% af et merge-event's manuelle arbejde.

---

## 3. Filer med højeste refactor-leverage

Disse er de upstream-filer hvor vi har mest in-place modifikation (efter alt der allerede er flyttet til `cerebro-*`). Hvert refactor herfra giver garanteret 0 konflikter fremover.

| Fil | Add | Del | Anbefalet flyt | Bucket |
|---|---|---|---|---|
| `packages/core/api/client.ts` | 1516 | 188 | `packages/cerebro-api-client/extensions.ts` (module-augmentation) | #1 |
| `packages/views/inbox/components/inbox-page.tsx` | 1202 | 171 | `packages/cerebro-inbox/inbox-page.tsx` (replacement) | #2 |
| `packages/views/issues/components/issue-detail.tsx` | 980 | 626 | Wrap via `packages/cerebro-ui/issue-detail-shell` | #3 |
| `server/internal/handler/daemon.go` | 656 | 162 | `server/internal/cerebro/runtime/handler.go` | #4 |
| `packages/views/issues/components/agent-live-card.tsx` | 537 | 3 | `packages/cerebro-ui/issues/agent-live-card.tsx` | #3 |
| `server/cmd/server/router.go` | 509 | 13 | Reducér til 1-linje cerebro-routes-import | #4 |
| `server/internal/service/task.go` | 479 | 67 | `server/internal/cerebro/tasks/service.go` | #4 |
| `packages/views/layout/app-sidebar.tsx` | 445 | 83 | Slot-pattern via `packages/cerebro-ui/sidebar/` | #6 |
| `server/cmd/server/notification_listeners.go` | 427 | 187 | `server/internal/cerebro/notifications/listeners.go` | #9 |
| `packages/views/settings/components/workspace-tab.tsx` | 404 | 9 | Tab-registry i `packages/cerebro-settings-tabs/` | #7 |
| `server/internal/handler/workspace.go` | 362 | 26 | `server/internal/cerebro/workspace/handler.go` | #4 |
| `packages/views/runtimes/components/runtimes-page.tsx` | 335 | 545 | `packages/cerebro-runtime/views/runtimes-page.tsx` | #15 |
| `server/internal/handler/runtime.go` | 335 | 22 | `server/internal/cerebro/runtime/handler.go` | #4 |
| `packages/views/runtimes/components/runtime-detail.tsx` | 317 | 179 | `packages/cerebro-runtime/views/runtime-detail.tsx` | #15 |
| `server/internal/daemon/daemon.go` | 294 | 38 | Patch-marker (L3) — daemon-loop er kerne-upstream | #16 |
| `packages/views/inbox/components/inbox-list-item.tsx` | 281 | 43 | `packages/cerebro-inbox/inbox-list-item.tsx` | #2 |
| `packages/views/editor/readonly-content.tsx` | 261 | 160 | PR til upstream + lokal patch indtil merged | #18 |
| `server/internal/handler/issue.go` | 247 | 82 | `server/internal/cerebro/issues/handler.go` | #4 |
| `packages/views/skills/components/skill-detail-page.tsx` | 243 | 144 | PR til upstream (JEH-216) | #17 |
| `packages/views/projects/components/projects-page.tsx` | 233 | 262 | Slot-pattern, delvist PR til upstream | #24 |
| `server/internal/handler/agent.go` | 202 | 120 | `server/internal/cerebro/agents/handler.go` | #4 |

**Total in-place på top-21:** ~9.700 add + ~3.600 del linjer. Det er den lommeflade som hver upstream-merge i dag skal navigere.

---

## 4. Slet-kandidater (kræver kode-søgning før delete)

| Område | Kandidat | Risiko-mitigation |
|---|---|---|
| Notifications-stack | Backend-listeners der allerede er revertet (FIR-1914) — efterladt skeleton i `server/internal/cerebro/notifications/` | Grep efter ikke-refererede listener-types; delete |
| ChatFab | `ChatFab` re-introduktion fra upstream sync (JEH-1889 revert) | Sikker at slette; allerede revertet |
| MCPServersCard | Legacy MCPServersCard (JEH-1710) | Grep efter component-importer; delete hvis 0 |
| Folders-feature (inbox) | Tidlig folders-implementation revertet (JEH-650, commit 7fce6da6c). Subfolder-support kom senere (b0be4ddb7) — verify de ikke duplicate'r hinanden | Diff de to commits; behold den nyeste |
| Per-member enforcement | Roll-back-commit `adb27ebb9` "revert: roll back per-member enforcement toggles" | Sikker at slette dead code |
| Duplicate capability declarations | FIR-2099, FIR-2129 cleanups antyder at duplicate-symbols stadig findes i grenede paths | Grep efter dobbelt-registrering |
| `e2e/fixtures.ts` (+261 -1) | Stor in-place modifikation af upstream-test-fixtures | Tjek om vores fixtures kan eksistere som `e2e/cerebro-fixtures.ts` |

**Hver slet-kandidat skal have dedikeret follow-up issue med kode-søgning før delete.**

---

## 5. PR-til-upstream kandidater (sorteret efter sandsynlighed for accept)

| Sandsynlighed | Bucket | Hvad | Hvorfor det er generelt |
|---|---|---|---|
| Høj | #18 editor | Prose-cap 70ch (FIR-2114), readonly-content forbedringer, image-view fixes | Editor-fixes er rene UX-forbedringer, ingen Firtal-coupling |
| Høj | #17 skills | Skill ownership / versioning / change requests / forks (JEH-216) | Per origin-sync-overlap §7 er overlap "grøn" — upstream har ikke konkurrerende implementation |
| Høj | #24 projects | Color-picker, repo URL field, nested projects (JEH-672) | Generel project-UX |
| Høj | #14 MCP | MCP-server-skeleton, list_comments, add_attachment | MCP er ved at blive standard — upstream vil sandsynligvis acceptere |
| Medium | #19 mobile | Mobile inbox push routing fix, swipe archive, sidebar burger (JEH-821), task-view forbedringer | Mobile-bug-fixes er generelle. Kræver review-tid hos upstream |
| Medium | #20 autopilots | Autopilot model override (JEH-1310), agent handoff provenance fix (JEH-1518) | Generel value, men touch'er upstream's autopilot-core |
| Medium | #15 runtimes | Cloud runtime launcher (FIR-2152) | Generel feature, men vores implementation er Firtal-flavored |
| Medium | #29 tasks | Kolonnevælger, søgning, 24h default | UX-forbedring. Default-værdier kan være Firtal-only |
| Lav | #16 sandbox | Runtime sandbox-policy enforcement | Vi er den eneste der har sandbox-behov i den form |
| Lav | #11 access/permissions | Phase 5 unified access page + audit UI | For Firtal-shaped til at acceptere as-is |

---

## 6. Sammenhæng med bot (FIR-2169) og catch-up (FIR-2171)

- **Bot ([FIR-2169](mention://issue/a4c50337-ddbb-4725-8dfb-02efece952f3))** løser auto-merge for L0 (genererede filer, lockfiles) og L1 (rene moves). Den **rører ikke** L2/L3 — det er filerne i §3 ovenfor. Hvis vi udfører Top-1 til Top-7 i §2, **flytter vi 8-12 filer fra L2/L3 til L1**, hvilket gør bot'en kraftigere automatisk.
- **Catch-up ([FIR-2171](mention://issue/0aee1c0e-0780-4c43-898b-dc140a70b80e))** er en éngangs-merge af 35 commits bagud. Den her audit påvirker **næste** merge, ikke catch-up'en. Men §4 slet-kandidater bør køres FØR catch-up, så vi ikke merger upstream-rewrites ind i dead fork-kode.

**Rækkefølge-anbefaling:**
1. Catch-up (FIR-2171) — clear backlog
2. Slet-kandidater fra §4 — fjern dead code
3. Top-7 refactor-actions fra §2 — flyt L2/L3 til L1
4. Bot deployment (FIR-2169) — får nu maksimal effekt
5. PR-til-upstream-kandidater fra §5 — løbende, parallelt

---

## 7. Følgebagklog (foreslået issue-tree)

Hver linje skal blive til et follow-up issue efter Jespers prioritering. Estimat er ren refactor-cost; review/QA tillægs.

| Forslået titel | Bucket | Estimat | Afhænger af |
|---|---|---|---|
| **Refactor: split api-client til cerebro-api-client/extensions** | #1 | 2 dage | — |
| **Refactor: replace upstream inbox-page med cerebro-inbox route-shadow** | #2 | 3-5 dage | Phase 0 zoner |
| **Refactor: wrap issue-detail.tsx + søsterfiler via L2-composition** | #3 | 4 dage | Phase 0, P7 preflight |
| **Refactor: flyt 5 server-handlers til server/internal/cerebro/** | #4 | 3 dage | Phase 0, routing-patch design |
| **Merge-action: take upstream notifications-rewrite, re-flyt til cerebro-notifications** | #9 | 1 dag (i merge-vindue) | — |
| **Refactor: app-sidebar slot-pattern via cerebro-ui** | #6 | 2 dage | — |
| **Refactor: workspace-tab + settings-page tab-registry** | #7 | 1-2 dage | — |
| **PR upstream: editor prose-cap + readonly-content + image-view** | #8 | 0.5 dag | — |
| **PR upstream: skill ownership / versioning / forks (JEH-216)** | #9 | 1 dag | — |
| **Slet: dead notifications-listener-skelet** | §4 | 0.5 dag | — |
| **Slet: legacy MCPServersCard + folders-feature reverter-dust** | §4 | 0.5 dag | — |
| **Slet: duplicate capability declarations (FIR-2099/2129 follow-through)** | §4 | 1 dag | — |
| **Refactor: cloud runtime launcher → cerebro-runtime/views** | #15 | 2 dage | — |
| **Refactor: chat-window + chat-input → cerebro-chat** | #21 | 2 dage | — |
| **Refactor: runtime sandbox-policy → server/internal/cerebro/sandbox** | #16 | 1-2 dage | — |
| **PR upstream: project color + repo URL + nested-projects** | #24 | 1 dag | — |
| **PR upstream: MCP-server skeleton + tools** | #14 | 1-2 dage | — |
| **Cleanup: squash revert-historik (NotificationBell, ChatFab, MCPServersCard)** | #32 | 0.5 dag | Lav prioritet |

**Total eksekverings-estimat:** ~30-35 udvikler-dage. **Forventet output:** næste upstream-merge går fra ~3-8 timers manuelt arbejde til ~30 min auto + 1 time manuelt.

---

## 8. Antagelser og åbne spørgsmål

Ting jeg ikke kan svare på alene — Jesper skal beslutte:

- [ ] **Hvor aggressivt vil vi PR'e til upstream?** Hver PR koster review-tid, og multica-ai kan afvise. Konservativ approach: send kun "høj sandsynlighed" (4 PRs). Aggressiv: send alt med medium+ (10 PRs).
- [ ] **Skal vi rebase-squashe revert-historik?** Lav værdi, men gør git-blame klart. Hvis ja → kræver koordineret freeze.
- [ ] **Inbox-replacement: behold "vi ejer 100%" beslutning?** Stadig korrekt? Eller skal vi acceptere upstream's inbox-evolution som baseline?
- [ ] **Bot-deployment-timing (FIR-2169) — før eller efter Top-7 refactor?** Min anbefaling: efter, så bot får maksimal effekt.

---

## 9. Metodologi

1. `git rev-list --count origin/main ^upstream/main` → 682 commits
2. `git log --pretty=format:"%h|%ad|%s" origin/main ^upstream/main` → fuld liste til `/tmp/fork-commits.txt`
3. Gruppér via commit-prefix (`feat(...)`, `fix(...)`, `refactor(...)` osv) → 327 unikke prefix-grupper, manuelt rullet op til 33 buckets
4. `git diff --numstat upstream/main origin/main` → filer + linje-tællinger
5. Filtrér ud cerebro-zoner + docs + generated → 440 upstream-zone modifikationer tilbage
6. Top-21 efter linje-volumen → primær refactor-target-liste
7. Kryds med `docs/upstream-sync/origin-sync-overlap.md` for vægt på næste-merge-konfliktrisiko
8. Mappér til Top-10 efter konflikt-reduktion / eksekverings-cost ratio

**Tids-stempel:** 2026-05-24
**Auditor:** Sara (CTO-agent)
