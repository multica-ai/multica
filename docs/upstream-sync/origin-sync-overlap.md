# Origin-sync overlap rapport

**Branch:** `test/upstream-merge-2026-W19`
**Merge-base mod origin/main:** `91e91158`
**Tilstand:** 28 commits bagud, 32 foran origin/main
**Filer modificeret på BEGGE sider:** 47 filer + migration-namespace-kollision

## Sammenfatning

Origin/main har siden vores divergence tilføjet en stor channel-first
notifikations-omskrivning (~30 commits) plus PWA, Web Push, Skill ownership,
mobile-sweeps og diverse fix. Vores branch har samtidig flyttet 126 filer
ind i `packages/cerebro-*` zoner og introduceret feature-flag systemet.

To slags konflikter:

1. **Indholds-konflikter** — filer modificeret på begge sider.
2. **Namespace-konflikter** — begge sider har valgt migration-numre
   9010 og 9011 til forskellige features.

## Migration-namespace kollision (kritisk)

Origin har taget cerebro-zonen 9000+ til sine egne fork-features
(deres egne kommentarer kalder dem "Fork-specific"):

| Nummer | Origin | Vores |
|---|---|---|
| 9010 | `9010_push_subscription` | `9010_cerebro_feature_flags` |
| 9011 | `9011_notifications_channels` | (ingen) |
| 9011 | `9011_skill_ownership` | (ingen) |

Origin har endda brugt 9011 to gange — `notifications_channels` og
`skill_ownership` har samme prefix. Det vil migrate-runneren bryde sig om;
det er en separat origin-bug.

**Anbefalet beslutning:** renumber vores `9010_cerebro_feature_flags` til
`9012_cerebro_feature_flags`. Origin har skrevet sine 9010/9011 først (i
deres branch-tid); det er nemmere at flytte vores ene migration end at
omdøbe origins tre. Phase 6's plan om relocate til
`server/migrations/cerebro/` ville have undgået problemet — det er stadig
korrekt at gøre senere.

## Indholds-konflikter — feature for feature

### 1. Notifikations-omskrivning (rød)

Origin har skrevet sit notifications-system fundamentalt om — fra
"per-event single-route" til "per-channel on/off". Det rør:

| Fil | Origin (linjer) | Vores |
|---|---|---|
| `packages/core/notifications/routing.ts` | +149/-28 | flyttet til cerebro-notifications uden ændring |
| `packages/views/settings/components/notifications-tab.tsx` | +348/-182 | flyttet til cerebro-notifications uden ændring |
| `server/migrations/9011_notifications_channels.up.sql` | NY | — |
| `server/internal/handler/notifications.go` | M | M |

**Anbefalet beslutning:** TAKE ORIGINS rewrite på begge filer. Vores
flytning til cerebro-notifications var en mekanisk relocation — origins
355 nye linjer er den faktiske produkt-evolution. Re-applicer flytningen
til cerebro-notifications i en ny commit EFTER merge. Phase 6 kan
re-konsumere de nye filer, ikke omvendt.

### 2. Channels-feature (rød indirect)

Origin tilføjer hele kanal-konceptet — issues kan have `kind: 'channel'`,
ny `/channels/{id}` route, ny `channel-detail.tsx`, kanal-aware
issue-detail UI:

| Fil | Origin | Vores |
|---|---|---|
| `packages/views/channels/**` | NY pakke (8 filer) | — |
| `packages/core/channels/**` | NY pakke (3 filer) | — |
| `packages/core/types/channel.ts` | NY | — |
| `packages/views/issues/components/issue-detail.tsx` | +543/-295 (kanal-aware) | +8/-6 (cerebro-patches) |
| `packages/views/issues/components/comment-card.tsx` | +2 | +3/-1 (cerebro-patch) |
| `server/migrations/061_issue_kind.up.sql` | NY | — |
| `server/internal/handler/channel.go` | NY | — |

**Anbefalet beslutning:** TAKE ORIGIN på `issue-detail.tsx` (deres
omskrivning er ti gange større end vores patches), derefter re-apply
vores 8 cerebro-patch-linjer i den nye fil. Same for `comment-card.tsx`.
Channels-feature er ren tilføjelse — kan tages 1:1.

**Spørgsmål til dig:** skal kanal-feature gates bag en
`cerebro-feature-flag` (channels enabled/disabled per workspace+user),
eller skal det være altid-tændt upstream-funktionalitet? Default upstream-
adfærd er enabled.

### 3. Inbox-page omskrivning (rød — kompleks)

Begge sider har omfattende ændret `inbox-page.tsx`:

| Side | Hvad |
|---|---|
| Origin | +366/-... (channels-list view, mail-style rows, view-switch) |
| Vores | Splittet i 33-linje stub + 1457 linjer i `packages/cerebro-inbox/inbox-page.tsx` (Phase 2 feature flag) |

Vores split var en mekanisk extraction — hele filen flyttede over i en
cerebro-pakke for at kunne feature-flag inbox. Origin's 366 nye linjer
er produktudvikling oven på den oprindelige fil.

**Anbefalet beslutning:** TAKE ORIGINS `inbox-page.tsx` (med channels
view), drop vores split for nu. I en NY commit efter merge: re-extract
til cerebro-inbox med samme pattern. Phase 6 chunk 10 kan rerun mod den
nye fil — det er billigere end at rebase 366 linjer ind i en allerede
opdelt fil.

### 4. Inbox-list-item (rød)

| Side | Hvad |
|---|---|
| Origin | +219 linjer (channel-row rendering, kind-aware ikon/avatar) |
| Vores | +2 linjer (cerebro-patch) |

**Anbefalet beslutning:** TAKE ORIGIN, re-apply vores 2-liners cerebro-
patch.

### 5. Mobile sweeps på sidebar/dashboard (gul)

| Fil | Origin | Vores |
|---|---|---|
| `packages/views/layout/app-sidebar.tsx` | +48/-... | +6/-2 (cerebro-patch) |
| `packages/views/layout/dashboard-layout.tsx` | +5 | +2 |

**Anbefalet beslutning:** MERGE BY HAND. Begge har værdi —
origins mobile-fix og vores cerebro-zone gating skal sameksistere.
Brug `<<<<<<< HEAD / >>>>>>> origin/main` som breadcrumbs.

### 6. Server-side access (gul)

| Fil | Origin | Vores |
|---|---|---|
| `server/internal/handler/access.go` | +27 | +2 |

**Anbefalet beslutning:** MERGE BY HAND, trivielt.

### 7. Skill ownership (grøn)

Origin tilføjer skill ownership/forks/versioning. Vi har ingen ændringer
i `server/internal/handler/skill.go` ud over upstream baseline.

| Fil | Origin | Vores |
|---|---|---|
| `server/internal/handler/skill.go` | +96/-26 | uændret |
| `server/internal/handler/skill_ownership.go` | NY | — |
| `server/migrations/9011_skill_ownership.up.sql` | NY (namespace-kollision) | — |
| `packages/views/skills/components/skills-page.tsx` | M | uændret |

**Anbefalet beslutning:** TAKE ORIGIN 1:1. Eneste opgave er at flytte
9011_skill_ownership til et frit nummer (f.eks. 9013) hvis vi beholder
vores 9010_cerebro_feature_flags og origin's 9011_notifications_channels.

### 8. Inbox.go server (gul)

| Fil | Origin | Vores |
|---|---|---|
| `server/internal/handler/inbox.go` | +20 | +14/-24 (cerebro inbox folders) |

**Anbefalet beslutning:** MERGE BY HAND. Origins 20 nye linjer er
sandsynligvis kanal-routing — vores 14 er folder-extension. Trivielt.

### 9. PWA + Web Push (grøn)

Origin tilføjer:

- `apps/web/app/sw.ts`, `sw-badge.ts`, `manifest.webmanifest`, icons
- `server/migrations/9010_push_subscription.up.sql` (namespace-kollision)
- `server/internal/handler/push.go`, `service/push.go`
- `packages/views/settings/components/push-notifications-section.tsx`
- `packages/views/inbox/use-app-badge.ts`

Vi rør ingen af disse filer. Eneste konflikt er migration-numret 9010.

**Anbefalet beslutning:** TAKE ORIGIN 1:1.

**Spørgsmål til dig:** skal Web Push gates bag en cerebro-feature-flag,
eller altid-tændt? Default upstream er enabled.

## Filer modificeret på begge sider — fuldt overblik

47 filer total. De fleste er trivielle:

```
apps/desktop/src/renderer/src/routes.tsx
apps/web/package.json
packages/core/api/client.ts
packages/core/inbox/index.ts
packages/core/inbox/queries.ts
packages/core/inbox/ws-updaters.ts
packages/core/package.json
packages/core/paths/paths.ts
packages/core/paths/reserved-slugs.ts
packages/core/realtime/use-realtime-sync.ts
packages/core/types/agent.ts
packages/core/types/api.ts
packages/core/types/index.ts
packages/core/types/issue.ts
packages/core/types/pin.ts
packages/ui/markdown/file-cards.ts
packages/views/autopilots/components/autopilot-detail-page.tsx
packages/views/chat/components/chat-input.tsx
packages/views/editor/utils/link-handler.ts
packages/views/inbox/components/inbox-list-item.tsx        ← rød (origin: +219)
packages/views/inbox/components/inbox-page.tsx             ← rød (rewrite på begge)
packages/views/issues/components/comment-card.tsx          ← gul
packages/views/issues/components/comment-input.tsx
packages/views/issues/components/issue-detail.tsx          ← rød (origin: +543)
packages/views/issues/components/issues-header.tsx
packages/views/issues/components/reply-input.tsx
packages/views/layout/app-sidebar.tsx                      ← gul
packages/views/layout/dashboard-layout.tsx                 ← gul
packages/views/modals/create-project.tsx
packages/views/projects/components/project-detail.tsx
packages/views/projects/components/projects-page.tsx
pnpm-lock.yaml
server/cmd/server/integration_test.go
server/cmd/server/main.go
server/cmd/server/notification_listeners.go
server/cmd/server/notification_routing_test.go
server/cmd/server/notification_routing.go
server/cmd/server/router.go
server/cmd/server/subscriber_listeners_test.go
server/internal/handler/access.go                          ← gul
server/internal/handler/handler.go
server/internal/handler/inbox.go                           ← gul
server/internal/handler/issue.go
server/internal/handler/workspace_reserved_slugs.go
server/pkg/db/generated/models.go                          ← sqlc-generated, regenereres
server/pkg/db/queries/inbox.sql
server/pkg/db/queries/issue.sql
server/pkg/protocol/events.go
```

Alle ufarvede er ~5-30 linje konflikter. De fleste klarer sig selv eller
trivielt manuelt.

## Anbefalet samlet strategi

**TAKE ORIGIN (overtag origins version, drop vores)**:
- `notifications/routing.ts`, `notifications-tab.tsx` — re-relocate til cerebro-notifications efter merge
- `inbox-page.tsx`, `inbox-list-item.tsx` — re-relocate til cerebro-inbox efter merge
- `issue-detail.tsx`, `comment-card.tsx` — re-apply 8 cerebro-patches efter merge

**TAKE OURS (behold vores)**:
- (ingen kandidater — alle vores ændringer er enten flytninger eller patches der kan re-applies)

**MERGE BY HAND**:
- `app-sidebar.tsx`, `dashboard-layout.tsx`
- `server/internal/handler/access.go`, `inbox.go`
- 30+ trivielle filer

**RENUMBER**:
- `9010_cerebro_feature_flags` → `9012_cerebro_feature_flags` (eller højere)
- Eventuelt origins `9011_skill_ownership` → `9013_skill_ownership` (hvis vi vil have unik prefix)

## Open spørgsmål til dig

1. **Channels-feature flag-gating?** Origin's nye kanal-feature er
   altid tændt. Skal vi gates bag `cerebro-feature-flags.channels` så
   du kan slå det fra per workspace/user?

2. **Web Push flag-gating?** Origin's nye PWA Web Push er altid tændt.
   Samme spørgsmål.

3. **Renumber-strategi for migrations?** Vores `9010_cerebro_feature_flags`
   skal flyttes til ledig prefix. `9012` virker. Origins
   `9011_skill_ownership` har duplikeret prefix med
   `9011_notifications_channels` — det er en upstream-bug. Skal jeg fixe
   den ved at flytte til `9013_skill_ownership` som en del af denne merge?

4. **Cerebro-patch genapplicering — atomic eller batched?** De 8 cerebro-
   patches på `issue-detail.tsx` skal re-applies efter origins rewrite.
   Skal det være én commit (`fix(cerebro): re-apply patches after origin
   merge`) eller integreret i merge-commit?

## Næste skridt

Når du har bekræftet beslutninger 1-4 ovenfor, kører jeg:

1. Rename `9010_cerebro_feature_flags` → `9012_cerebro_feature_flags`
   (separate commit FØR merge for at undgå migration-rerun)
2. `git merge origin/main` (forventet: ~30 conflicted files)
3. Resolve hver fil per beslutning ovenfor
4. Re-apply 8 cerebro-patches manuelt i `issue-detail.tsx`
5. Re-relocate notifications + inbox-page til cerebro-zoner (Phase 6 chunks)
6. `make check` grøn
7. Browser smoke-test af channels, push, project-access, mobile
