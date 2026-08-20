# #297 Onboarding / Mika retirement source audit

Fixed points: Multica `887cffd7822f4c11556014f7d83930c701c1e443`
(including the #292 Workspace/Members journey, #279 Inbox shell, and #296
Desktop retirement),
VIBES `cf689b401de60abb4f8db4e221388ddb4c8f3162`.

VIBES remains the only writable authority for identity, Workspace, human
Membership, roles, invitations, and Join Links. Multica continues to own
ordinary Chat, Tasks, Agents, Runtimes, Projects, Files, and execution state.
The replacement is the #292 VIBES Workspace journey and projection-ready Tag
handoff; #297 must not construct a second onboarding backend or workspace
writer.

| Product path                       | Existing source / production caller                                                                                                                                                                      | Classification                                                                                    | Concrete coupling or deletion boundary                                                                                                                                                                                                                                                                                                                                              |
| ---------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Browser `/onboarding`              | `apps/web/app/(auth)/onboarding/page.tsx`; `packages/views/onboarding/**`; login, callback, invitation, landing, dashboard and workspace guards through `resolvePostAuthDestination` / `useHasOnboarded` | Physically remove from Web and remove navigation/guards                                           | The route owns questionnaire state, native Workspace creation, Runtime selection, Mika creation, a starter Chat/Task and `onboarded_at`. None is an admission requirement after VIBES handoff.                                                                                                                                                                                      |
| Browser `/workspaces/new`          | `apps/web/app/(auth)/workspaces/new/page.tsx` mounts `OnboardingFlow` in `new_workspace` mode                                                                                                            | Replace with #292 VIBES journey; physically remove the Next writer page                           | `useCreateWorkspace()` writes Multica `POST /api/workspaces` and seeds native caches. The canonical Tag route instead uses `/api/tag-authority/workspaces`, waits for projection-ready, then switches through a fresh VIBES handoff.                                                                                                                                                |
| Web host handoff                   | Next login/callback/landing, server proxy recovery, and shared navigation previously resolved into deleted or legacy Next pages                                                                          | Thin host adapter to `/tag/workspaces/new`, `/tag/:slug/chat`, and matching Tag Issues deep links | Shared paths stay host-neutral for Desktop and Tag internals. `web-host-path.ts` mounts client destinations; `proxy.ts` moves root refreshes and stale Issues links before Next renders. Because `/tag` is served by the separate TanStack host, clients use document navigation/native anchors and skip Next prefetch rather than sending an unowned path through the Next router. |
| Post-auth destination              | `packages/core/paths/resolve.ts`, login/callback/invitation/no-access/realtime callers                                                                                                                   | Thin route adapter; remove `onboarded_at` and native-create decisions                             | The shared resolver remains host-neutral for surviving Web and Tag callers. The Web adapter maps an existing projection to `/tag/:workspaceSlug/chat` and an empty list to `/tag/workspaces/new`; VIBES session + active membership + projection acknowledgement decide Tag admission. Legacy Next callers must not redirect to onboarding or mint a Workspace.                     |
| Questionnaire / source attribution | `packages/core/onboarding/**`, onboarding steps, `SourceBackfillModal`, `/api/me/onboarding`                                                                                                             | Remove product callers and runtime writer routes                                                  | Analytics fields are coupled to the retired forced questionnaire. Historical columns/events may remain for audit; they are not a browser gate or authority truth.                                                                                                                                                                                                                   |
| `onboarded_at` gate                | `apps/web/app/[workspaceSlug]/layout.tsx`, `resolvePostAuthDestination`, `useHasOnboarded`, invitation completion                                                                                        | Remove all Web admission decisions and writes                                                     | The flag is Multica-local user state and cannot override a fresh VIBES assertion. Generated DB fields and old migrations remain until separately authorized data retention/schema cleanup.                                                                                                                                                                                          |
| Mika bootstrap                     | `packages/core/onboarding/use-bootstrap-mika.ts`; `packages/views/runtimes/components/runtimes-page.tsx`; `POST /api/agents/mika`                                                                        | Physically delete UI action, API route, handler, writer and production readers                    | Mika provisions a product-defined `system_key`, per-member Chat session and kickoff. It is not ordinary manual Agent creation and is not required to use a Runtime.                                                                                                                                                                                                                 |
| Mika welcome Chat                  | `POST /api/chat/sessions/:sessionId/onboarding`; `mika_onboarding*.go`; TaskService kickoff/opening writes                                                                                               | Physically delete route, handler and Mika-only message writer                                     | The endpoint accepts questionnaire answers and writes synthetic user/opening messages. Canonical Tag entry must show the ordinary empty/normal Chat surface without creating Agent, Chat, Task or message rows.                                                                                                                                                                     |
| Mika system instruction carrier    | `service/builtin_agents.go`, embedded Mika prompt, special response/task composition and daemon behavior                                                                                                 | Remove Mika-specific runtime readers; retain generic system-agent infrastructure                  | `system_key` is also used by live Agent Builder carriers, so the shared column, generated queries and generic protection cannot be deleted with Mika. Historical `system_key='mika'` rows remain data-retention work, not a reason to keep Mika behavior.                                                                                                                           |
| Desktop pre-v3 bootstrap shim      | `/api/me/onboarding/runtime-bootstrap`, `/api/me/onboarding/no-runtime-bootstrap`, `onboarding_shim.go`                                                                                                  | Physically delete shared server endpoints and writers                                             | #296 removed the Desktop application, leaving no production source caller. Stale binaries receive the router's stable not-found response and cannot write Workspace/Agent/Chat truth.                                                                                                                                                                                               |
| Legacy Next invitations            | Next `/invitations`, `/invite/[id]`, and the shared Web sidebar called native Multica invitation membership writers                                                                                      | Physically remove browser routes and sidebar mutations                                            | #292 accepts VIBES invitation/Join Link tokens at `/tag/invite` through `/api/tag-authority/*`. The caller-zero shared legacy invitation pages remain deleted after #296 removed Desktop.                                                                                                                                                                                           |
| Runtime install/connect            | normal Runtimes page, create/connect dialogs, generic Runtime APIs                                                                                                                                       | Retain shared capability                                                                          | Runtime availability is optional and independently reachable. Remove only the Mika CTA and onboarding side effects; do not alter generic install/connect/reconcile behavior.                                                                                                                                                                                                        |
| Manual Agent creation              | `/agents/new`, `/agents/new/manual`, `/agents/new/ai`; generic `POST /api/agents`                                                                                                                        | Retain shared capability                                                                          | These paths create user-selected Multica execution Agents and do not claim VIBES Workspace authority. They must remain reachable without onboarding.                                                                                                                                                                                                                                |
| Historical schema/audit            | migrations for `onboarded_at`, questionnaire, starter state, `system_key`; generated DB models                                                                                                           | Retain pending separate authorization                                                             | #297 has no Production migration or data-deletion authority. Runtime routes/readers/writers are removed while historical rows/columns remain inert.                                                                                                                                                                                                                                 |
| VIBES authority and handoff        | VIBES Workspace/Membership authority, `POST /api/tag-handoff/issue`, consume/assertion provider, #292 Tag authority pages                                                                                | Direct replacement; retain unchanged                                                              | VIBES fixed point contains no Mika or onboarding caller. Provisioning remains denied until projection-ready. The local `/tag-entry` tracer is a historical development seam, not an onboarding replacement and not changed here.                                                                                                                                                    |

## Stable retired-entry behavior

Removing the legacy registrations makes the old authenticated API entries
return the server router's stable `404 Not Found` before any handler can write.
The stale Next `/onboarding` page and its native `/workspaces/new` writer page
are removed rather than redirected to a Multica fallback. Canonical Workspace
creation remains the #292 Tag-host VIBES route.

## Production callers and writer paths to eliminate

- `PATCH /api/me/onboarding`
- `POST /api/me/onboarding/complete`
- `POST /api/me/onboarding/cloud-waitlist`
- `POST /api/me/onboarding/runtime-bootstrap`
- `POST /api/me/onboarding/no-runtime-bootstrap`
- `POST /api/agents/mika`
- `POST /api/chat/sessions/:sessionId/onboarding`
- Web calls to native `POST /api/workspaces` from the two retired Next routes
- Web calls to native invitation accept/decline from the retired Next routes
  and sidebar
- Mika-only per-member Chat/session, opening message, kickoff and starter Task
  creation

Ordinary `POST /api/agents`, Chat session/message APIs, Tasks and Runtime APIs
remain live. The #292 browser journey continues to prohibit native
Workspace/member/invitation/share-link writers.

## Necessary new-code boundary

The retirement adds only contract tests for public route behavior and the
smallest destination adapter needed by surviving Web callers. It does not
fork pages, add fallback auth, add a synthetic backend, or copy VIBES authority
logic into Multica. With the Desktop application removed at the fixed point,
the caller-zero shared onboarding views, core hooks, Mika API clients, and
compatibility endpoints are physically deleted.

## Cross-ticket isolation

- Preserve #296's Desktop deletion; do not recreate Electron-owned paths.
- Do not edit Inbox/#279, realtime/#290, cleanup/#291, CLI auth/#295, Settings,
  or advanced Agent surfaces beyond Mika-specific calls.
- Preserve #279 Inbox navigation, badges, and routes. Keep the
  `server/cmd/server/router.go` change limited to removing the seven retired
  route registrations so notification/realtime integration stays intact.
