# #292 Workspace / Members source reuse audit

Baseline: Multica `9d219d78e0f5d77de2d6b68efaae31341d116425`, VIBES
`a78e7471e62fa95cff34844613572f464a96fced`.

This slice preserves VIBES as the only writable authority for people,
Workspaces, human Membership, roles, invitations, and Join Links. Multica
continues to own Chat, Tasks, Agents, Runtimes, Projects, Files, and their
execution state.

| Journey                      | Existing Multica source to reuse                                                                                                                                  | Classification                                                                             | Concrete coupling that prevents direct data-layer reuse                                                                                                                                                                    | Browser authority writer                                                                                                                    |
| ---------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| Workspace list / switch      | `apps/tag-host/src/workspace/tag-workspace-shell.tsx` dropdown, current-row state, mobile sidebar close; `workspace-shell-model.ts` module-preserving destination | Direct UI reuse + thin VIBES adapter                                                       | `workspaceListOptions()` calls native `GET /api/workspaces`; `useCurrentWorkspace()` is backed by Multica's mirrored collaboration Workspace; current switch is navigation-only and does not clear the old workspace scope | `GET /api/tag-authority/workspaces`; `POST /api/tag-authority/workspaces/:id/switch`                                                        |
| Create Workspace             | `packages/views/onboarding/steps/step-workspace.tsx` name/slug validation, conflict/pending/error patterns                                                        | Reuse interaction and validation pattern through a new shared Tag view; thin VIBES adapter | Existing `useCreateWorkspace()` writes `POST /api/workspaces`, seeds the native Workspace cache, and the full onboarding flow also owns Runtime/Mika state                                                                 | `POST /api/tag-authority/workspaces`, then poll the projection-ready list before switch/handoff                                             |
| Members / roles              | `packages/views/settings/components/members-tab.tsx` role badges, row menus, last-owner disabled state, confirmation and narrow grid                              | Reuse UI language in a non-Settings presentational view + VIBES authority seam             | Existing view is inseparable from Settings layout and native `listMembers/updateMember/deleteMember`; #280 currently owns Settings files                                                                                   | `GET/PATCH /api/tag-authority/workspaces/:id/members`                                                                                       |
| Targeted invitation          | Existing Members invite card, pending row, role selector, revoke confirmation, toast/loading patterns                                                             | Reuse UI language + thin VIBES adapter                                                     | Native Multica invitations do not implement resend or role revision and use incompatible models/routes                                                                                                                     | `GET/POST /api/tag-authority/workspaces/:id/invitations`; `POST /api/tag-authority/invitations/:id`                                         |
| Invite accept / decline      | `packages/views/invite/invite-page.tsx` loading/error/success card structure                                                                                      | Reuse visible state-machine layout + thin VIBES adapter                                    | Existing page depends on Multica auth/onboarding and native invitation IDs, not opaque VIBES tokens                                                                                                                        | `POST /api/tag-authority/invitations/accept` or `/decline`                                                                                  |
| Join Link                    | Existing Members share-link card, seven-day default, copy fallback, revoke action; native join page state layout                                                  | Reuse UI language + thin VIBES adapter                                                     | Native route allows a role and lists links; VIBES is Member-only and currently exposes create/revoke/claim but no list/read seam                                                                                           | `POST /api/tag-authority/workspaces/:id/join-links`; `DELETE /api/tag-authority/join-links/:id`; `POST /api/tag-authority/join-links/claim` |
| Remove / leave / role change | Existing member action menu and confirmation flow                                                                                                                 | Reuse interaction + VIBES authority command                                                | Native mutation reports immediate success. VIBES has no browser completion-receipt reader while #289–#291 are open                                                                                                         | `PATCH /api/tag-authority/workspaces/:id/members`, followed by an honest sync-pending state                                                 |

## Native browser writer paths prohibited for this journey

- `POST/PATCH/DELETE /api/workspaces*`
- `POST/PATCH/DELETE /api/workspaces/:id/members*`
- native `/api/invitations*`
- native `/api/workspaces/:id/invitations*`
- native `/api/share-links*` and `/api/workspaces/:id/share-links*`

The new browser adapter may call only same-origin `/api/tag-authority/*` for
human Workspace/Membership authority reads and writes. The existing
`WorkspaceGate` may still read Multica's projected Workspace through native
`GET /api/workspaces` solely to prove the execution shell has been provisioned;
that mirror read is not used to authorize Members or derive capabilities. The
inner Members route always re-checks the VIBES authority projection before it
renders people data. The adapter must not populate native
Workspace/member/invitation/share-link query caches with VIBES authority data.

## Necessary new-code boundary

1. One headless authority module in `packages/core` owns response parsing,
   error normalization, query keys, commands, projection-ready polling, and
   the ordered switch cleanup interface.
2. New shared views in `packages/views` reuse Multica primitives and visible
   interaction patterns, while accepting only the headless authority
   interface. They contain no native writer or permission truth.
3. Tag host routes and the existing switcher provide platform navigation and
   handoff wiring. No Next.js page, native Multica writer, fallback auth, or
   copied collaboration backend is added.
4. Join Link management displays only the just-created link because VIBES has
   no read/list seam. Persistent link inventory is explicitly deferred.
5. Revocation remains `sync-pending`; durable HTTP/WS enforcement and cleanup
   remain #289–#291 and are not represented as complete by this slice.

## Settings writer isolation

#280 currently owns `packages/views/settings/**`, Settings locale files,
`apps/tag-host/src/workspace/tag-workspace-settings*`, and the Settings route.
This slice does not edit those files. The navigation shell/model are not in
the current #280 diff; changes there must stay limited to Workspace/Members
entry wiring.
