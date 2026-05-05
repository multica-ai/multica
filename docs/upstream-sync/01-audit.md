# Step 1 — Audit

**Måling:** test-merge `git merge --no-commit upstream/main` mod mergebase `4ce3e5dd`.
**Resultat:** 201 konfliktfiler totalt (107 kode + 94 docs/MD).

## Lag-definition

| Lag | Betydning | Konfliktomkostning fremover |
|---|---|---|
| **L0** — Auto | Auto-resolve via script (take theirs / regenerate) | 0 |
| **L1** — Additive | Flyttes til `cerebro-*` zone som upstream aldrig rører | 0 |
| **L2** — Composition | Wrap/CSS-override/middleware uden at ændre upstream-fil | 0 |
| **L3** — Marked patch | Permanent inline-modifikation, markeret med `CEREBRO-PATCH(<navn>):` | Lille (3-5 linjer pr fil) |

## Cerebro-zonen

Alle Layer 1-ekstrakter lander her:

```
packages/cerebro-access/        — project access control UI + queries
packages/cerebro-users/         — Members admin + member detail + role toggles
packages/cerebro-artifacts/     — documents, attachments, file manager
packages/cerebro-notifications/ — notifications-tab, notifications page
packages/cerebro-inbox/         — vores rewrite af inbox-page (folders, archive, multi-select)
packages/cerebro-runtime/       — sandbox UI, profile UI
packages/cerebro-mcp/           — MCP install guide + onboarding hook
packages/cerebro-types/         — TypeScript module augmentation for upstream-types
packages/cerebro-test/          — test helpers cerebro-features bruger

server/internal/cerebro/
    access/        — access policy + middleware
    users/         — admin endpoints, member detail, enforcement
    artifacts/     — documents, attachments
    notifications/ — listeners, dispatch
    sandbox/       — execenv sandbox wrapper
    profile/       — profile persistence
    mcp/           — MCP-related server code

server/migrations/cerebro/      — separat migrations-stream (cerebro_NNNN_*.sql)

apps/web/app/(cerebro)/         — cerebro-only Next.js routes
apps/desktop/src/renderer/src/cerebro/  — cerebro-only desktop routes
```

## Bucket A — Layer 0 Auto (35 filer)

Alle håndteres af `scripts/upstream-sync.sh` uden manuel tankegang.

| Fil | Strategi |
|---|---|
| `pnpm-lock.yaml` | Slet, kør `pnpm install` |
| `packages/{core,views}/package.json` | 3-way merge på `dependencies`/`devDependencies` |
| `server/pkg/db/generated/*.sql.go` × 6 | Slet, kør `make sqlc` |
| `server/pkg/db/queries/*.sql` × 2 | Manuel merge → `make sqlc` |
| `.env.example`, `.gitignore`, `Makefile`, `docker-compose.selfhost.yml`, `.github/workflows/ci.yml` | 3-way text merge, alle additive linjer beholdes |
| `scripts/install.sh`, `scripts/init-worktree-env.sh` | 3-way text merge |
| `apps/web/app/docs/*` × 8 (alle docs-routes) | `git checkout --theirs` (docs er ligegyldigt) |
| `apps/web/components/{docs-settings,editorial,hero,mermaid,architecture-diagram}.tsx` | `git checkout --theirs` |
| `apps/web/lib/{i18n,site,translations}.ts`, `apps/web/middleware.ts` | `git checkout --theirs` |
| `packages/views/common/task-transcript/agent-transcript-dialog.tsx` | `git checkout --theirs` (vi har 0 ændringer) |
| `apps/web/.gitignore` | 3-way merge |
| 94 × MDX/MD content + SELF_HOSTING + CONTRIBUTING | `git checkout --theirs` |

## Bucket B — Layer 1 Additive (14 + 7 filer/pakker → flyttes/renames)

Filer med rene tilføjelser der kan leve i cerebro-zonen.

**Discovery-tilføjelse (F2):** 7 pakker er allerede isoleret i fork'en og findes ikke i upstream — de skal **rename til cerebro-prefiks**, ikke flyttes:

| Eksisterende sti | → Renamed |
|---|---|
| `packages/core/artifacts/` | `packages/cerebro-artifacts-core/` (eller flyt til `packages/cerebro-artifacts/core/`) |
| `packages/core/attachments/` | `packages/cerebro-attachments/core/` |
| `packages/core/notifications/` | `packages/cerebro-notifications/core/` |
| `packages/views/artifacts/` | `packages/cerebro-artifacts/views/` |
| `packages/views/attachments/` | `packages/cerebro-attachments/views/` |
| `packages/views/notifications/` | `packages/cerebro-notifications/views/` |
| `packages/views/members/` | `packages/cerebro-users/` |

Renamearbejdet er trivielt: imports updates + tsconfig-stier. Alle disse pakker er allerede uden conflict (de er i 100% vores territory) — rename er kun for klarhed og fremtidig disciplin.

**Plus disse 14 filer flyttes som oprindelig L1:**

| Nuværende sti | → Cerebro-mål | Indhold |
|---|---|---|
| `packages/views/members/index.ts` | `packages/cerebro-users/index.ts` | Hele Members-feature er ny |
| `packages/views/settings/components/notifications-tab.tsx` (ny fil, 384 lines) | `packages/cerebro-notifications/notifications-tab.tsx` | Ny komponent vi ejer |
| `packages/core/api/client.test.ts` (cerebro-additions) | `packages/cerebro-test/api-client.test.ts` | Test af cerebro-endpoints |
| `packages/views/agents/components/tabs/tasks-tab.test.tsx` | `packages/cerebro-test/tasks-tab.test.tsx` | Vores task-token tests |
| `apps/web/test/helpers.tsx` (cerebro-helpers) | `packages/cerebro-test/web-helpers.tsx` | Cerebro test-helpers |
| `server/internal/handler/daemon_test.go` (+96, additive) | `server/internal/cerebro/users/enforcement_test.go` | Vores enforcement-tests |
| `server/pkg/agent/claude_test.go` (additive) | `server/internal/cerebro/sandbox/claude_test.go` | Sandbox-tests |
| `server/internal/handler/inbox.go` cerebro-additions | `server/internal/cerebro/notifications/handler.go` | Folders, archive, etc handlers |
| `server/internal/realtime/hub.go` (+42 -0, kun additivt) | `server/internal/cerebro/realtime/events.go` + 1-line registration patch | Cerebro event-types |
| `server/cmd/server/notification_listeners.go` (+65 -11) | `server/internal/cerebro/notifications/listeners.go` + patch i main.go | Ny listener-registrering |
| `server/internal/cli/client.go` cerebro-commands (+68 -16) | `server/internal/cerebro/cli/commands.go` | Vores CLI-additions |
| `server/internal/daemon/config.go` cerebro-fields (+41 -0) | `server/internal/cerebro/runtime/config.go` med embedding | Sandbox + profile-config |
| `packages/core/realtime/use-realtime-sync.ts` cerebro-handlers (+69 -3) | `packages/cerebro-realtime/handlers.ts` + 1 registration call | Cerebro event-handlers |
| `packages/views/inbox/components/inbox-page.tsx` (+1198 -108) | `packages/cerebro-inbox/inbox-page.tsx` (replacement) + route-shadow | Vi erstatter upstream's inbox med vores egen — for stort til wrap |

**Net-effekt:** 14 filer fjernes fra fremtidig konfliktflade.

## Bucket C — Layer 2 Composition (15 filer → wrap, ikke modificér)

For hver: hold upstream-fil urørt; placér wrapper/override i cerebro-zone og brug den i stedet.

| Upstream-fil | Composition-teknik | Cerebro-implementering |
|---|---|---|
| `packages/views/issues/components/comment-card.tsx` | Replace import via Next.js path-alias eller shadow-component | `packages/cerebro-comments/comment-card.tsx` (vores version, importerer upstream som base) |
| `packages/views/issues/components/issue-detail.tsx` | Slot-prop pattern: vores wrapper renderer upstream + cerebro-extras | `packages/cerebro-issues/issue-detail.tsx` |
| `packages/views/issues/components/agent-live-card.tsx` | Replacement (vores card differs significantly) | `packages/cerebro-issues/agent-live-card.tsx` |
| `packages/views/issues/components/list-row.tsx` | Wrapper med cerebro-felter (access dot, mobile padding) | `packages/cerebro-issues/list-row.tsx` |
| `packages/views/issues/components/reply-input.tsx` | Wrapper | `packages/cerebro-issues/reply-input.tsx` |
| `packages/views/projects/components/project-detail.tsx` | Wrapper med Documents-tab og access-toggle | `packages/cerebro-access/project-detail.tsx` |
| `packages/views/projects/components/project-picker.tsx` | Wrapper med restricted-dot | `packages/cerebro-access/project-picker.tsx` |
| `packages/views/agents/components/agents-page.tsx` | Slot-extension (vores ekstra kolonner/filtre) | `packages/cerebro-users/agents-page-ext.tsx` |
| `packages/views/agents/components/tabs/tasks-tab.tsx` | Wrapper | `packages/cerebro-users/tasks-tab-wrap.tsx` |
| `packages/views/runtimes/components/runtime-detail.tsx` | Wrapper med sandbox-toggle | `packages/cerebro-runtime/runtime-detail.tsx` |
| `packages/views/chat/components/chat-input.tsx` | Wrapper med MCP-onboarding hook | `packages/cerebro-mcp/chat-input.tsx` |
| `packages/views/chat/components/chat-message-list.tsx` | Wrapper for mid-stream UX | `packages/cerebro-mcp/chat-message-list.tsx` |
| `packages/views/editor/readonly-content.tsx` | CSS override + wrapper for mobil-tweaks | `packages/cerebro-ui/readonly-content.tsx` |
<!-- removed: settings-page.tsx er rykket til L1 efter discovery (F3 — bruger eksisterende extraAccountTabs prop) -->
| `packages/views/layout/app-sidebar.tsx` | Slot-pattern + sidebar-registry (vores nav-items) | `packages/cerebro-ui/sidebar-extras.tsx` med Layer 3 hook |

**Net-effekt:** 15 filer fjernes fra fremtidig konfliktflade. Hvis upstream ændrer komponentens props/API → vores wrapper fejler ved compile (fanges af `make check`), ikke ved merge.

## Bucket D — Layer 3 Marked Patches (42 filer)

Permanente små inline-modifikationer markeret med `CEREBRO-PATCH(<navn>):`.
Hver patch ≤5 linjer. Total budget: <150 linjer kode på tværs af alle 42 filer.

### Server (29 filer)

| Fil | Patch-type | Estimerede linjer |
|---|---|---|
| `server/internal/handler/handler.go` | Embed cerebro-handlers i Handler-struct | ~5 |
| `server/internal/handler/agent.go` | Cerebro access-check call | ~3 |
| `server/internal/handler/auth.go` | Cerebro user-mint hook | ~3 |
| `server/internal/handler/chat.go` | Cerebro private-agent check | ~5 |
| `server/internal/handler/comment.go` | Cerebro attachment integration | ~3 |
| `server/internal/handler/daemon.go` | Cerebro task-token mint hook | ~5 |
| `server/internal/handler/inbox.go` | Mount cerebro-notifications routes | ~3 |
| `server/internal/handler/issue.go` | `IsPrivate` field + `canAccessIssue` filter call | ~6 |
| `server/internal/handler/project.go` | `Color`/`RepoURL`/`Access` fields i ProjectResponse | ~6 |
| `server/internal/realtime/hub.go` | Cerebro events registration | ~2 |
| `server/internal/service/task.go` | Cerebro pre-claim enforcement check | ~5 |
| `server/internal/util/pgx.go` | Cerebro util-additions | ~3 |
| `server/internal/events/bus.go` | Cerebro event-types | ~3 |
| `server/internal/cli/client.go` | Mount cerebro CLI commands | ~3 |
| `server/internal/daemon/client.go` | Cerebro daemon-config hooks | ~3 |
| `server/internal/daemon/daemon.go` | Cerebro sandbox integration | ~5 |
| `server/internal/daemon/execenv/execenv.go` | Sandbox wrapper hook | ~5 |
| `server/internal/daemon/execenv/runtime_config.go` | Cerebro runtime-config fields | ~3 |
| `server/internal/daemon/types.go` | Cerebro type extensions | ~3 |
| `server/cmd/server/listeners.go` | Mount cerebro listeners | ~3 |
| `server/cmd/server/main.go` | Init cerebro module | ~3 |
| `server/cmd/server/router.go` | `cerebro.MountRoutes(r)` single call | ~2 |
| `server/pkg/agent/claude.go` | Sandbox-prepareCommand call + stderr-tail | ~5 |
| `server/pkg/agent/copilot.go` | Sandbox-prepareCommand call | ~3 |
| `server/pkg/agent/cursor.go` | Sandbox-prepareCommand call | ~3 |
| `server/pkg/agent/gemini.go` | Sandbox-prepareCommand call | ~3 |

### Frontend (13 filer)

| Fil | Patch-type | Estimerede linjer |
|---|---|---|
| `packages/core/api/client.ts` | Mount cerebro-api sub-client | ~5 |
| `packages/core/projects/index.ts` | Re-export cerebro-projects helpers | ~3 |
| `packages/core/types/index.ts` | Re-export cerebro-types module augmentation | ~3 |
| `packages/core/types/agent.ts` | `WorkSession` type addition | ~3 |
| `packages/core/types/issue.ts` | `is_private` field addition | ~2 |
| `packages/core/types/workspace.ts` | `MemberUsage`, `MemberWithUser` extensions | ~3 |
| `packages/views/projects/components/projects-page.tsx` | Render cerebro project-extras slot | ~3 |
| `packages/views/issues/components/board-view.tsx` | Render cerebro access-dot | ~3 |
| `packages/views/issues/components/issues-page.tsx` | Mount cerebro filters | ~3 |
| `packages/views/issues/components/list-view.tsx` | Render cerebro list-row instead of upstream | ~3 |
| `packages/views/invite/invite-page.tsx` | Cerebro invite-extras | ~3 |
| `packages/views/workspace/new-workspace-page.tsx` | Cerebro workspace-init hook | ~3 |
| `packages/views/runtimes/components/runtime-list.tsx` | Cerebro sandbox-status column | ~3 |
| `packages/views/runtimes/utils.ts` | Cerebro utils additions | ~3 |
| `apps/desktop/src/renderer/src/components/desktop-layout.tsx` | Mount cerebro layout-slots | ~3 |
| `apps/desktop/src/renderer/src/routes.tsx` | `cerebroRoutes` spread into appRoutes | ~3 |
| `apps/web/app/[workspaceSlug]/(dashboard)/layout.tsx` | Mount cerebro providers | ~3 |

**Total estimeret patch-budget:** ~140 linjer på tværs af 42 filer = gennemsnit 3,3 linjer/fil.

## Per-fil opsamling

| Lag | Antal filer | Andel af 107 |
|---|---|---|
| L0 — Auto-resolve | 36 | 34% |
| L1 — Additive (flyt + rename) | 15 | 14% |
| L2 — Composition (wrap) | 14 | 13% |
| L3 — Marked patch | 42 | 39% |

**Plus 7 allerede-isolerede pakker** der renames til cerebro-prefiks (ikke i 107-tællingen — ingen konflikt).

**Centralt tal:** efter refactor er kun L3's 42 filer i upstream-konfliktflade, og hver enkelt har <5 linjer cerebro-kode. Det betyder en typisk upstream-merge rammer 3-8 af de 42 (afhængigt af hvor upstream rørte) med trivielle resolutioner.

## Råmaterialet

Fuld TSV i `/tmp/audit-data.tsv` med `file | our_commits | our_added | our_removed | upstream_commits | upstream_added | upstream_removed | our_commit_subjects` for alle 107 kodefiler.

Top-10 hotspots (modificeret af begge sider):

```
file                                                     ours      upstream
packages/core/api/client.ts                              +382 -4   +356 -9
packages/views/inbox/components/inbox-page.tsx           +1198-108 +91 -20
server/internal/handler/daemon.go                        +270 -12  +599 -65
packages/views/agents/components/agents-page.tsx        +131 -83   +826 -187
packages/views/issues/components/issue-detail.tsx        +129 -29  +278 -650
server/internal/service/task.go                          +182 -38  +984 -50
server/cmd/server/router.go                              +152 -9   +167 -23
packages/views/layout/app-sidebar.tsx                    +142 -8   +169 -85
server/pkg/db/generated/models.go                        +208 -32  +76 -30
packages/views/runtimes/components/runtime-detail.tsx    +110 -2   +406 -111
```
