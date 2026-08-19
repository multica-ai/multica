# VIBES Tag host

This local-only TanStack Start host mounts Multica's original `@multica/core`,
`@multica/views`, and `@multica/ui` packages directly. It does not copy their
source, replace the Go API, or establish a permanent fork.

## Compatibility record

- Baseline: `46ee413843cb6f6b3df3428581ad1c6b0ee682cb`
- TanStack Router/Start: `1.159.5`
- Vite: `8.0.1`
- React: the Multica workspace catalog (`19.2.3` at this baseline)
- Reusable Multica package changes: none
- Host-only seams: `/tag` router base, client-only Chat and Task routes,
  navigation adapter, cookie-session provider, VIBES Task vocabulary adapter,
  and a Workspace gate that selects the slug/id before child queries or
  realtime code mount
- Explicit browser bases: `/api/tag` and `<same-origin>/ws/tag`

## Canonical local entry

Ticket #273 makes this TanStack host the single local browser entry at
`http://localhost:3100`. Its loopback-only development gateway keeps every
browser request same-origin while preserving the existing ownership boundaries:

- `/tag/*` stays in this TanStack host.
- `/api/tag/*`, `/ws/tag`, and `/uploads/*` forward to the existing Multica Go
  API and realtime service.
- VIBES pages, identity handoff, and workspace authority forward to the VIBES
  development server on port 3101.
- Old Multica core links redirect into `/tag/<workspace>/*`; unported workspace
  surfaces show an explicit Tag migration state instead of falling back to
  Next. VIBES-owned and later-ticket global surfaces stay with their current
  owner.

The PWA manifest is Tag-scoped and launches `/tag/` through the same canonical
entry behavior. A remembered workspace is used directly only while the normal
Multica session cookie exists; otherwise the launcher runs the existing VIBES
identity handoff first. If Core later rejects a stale session during a direct
deep-link refresh, the host returns that exact workspace page through the same
handoff instead of presenting a second-login dead end. Browser navigation must use
port 3100; port 3001 is no longer a runtime entry. This gateway is a local
acceptance topology only and does not define a Production host.

The direct package build succeeds without compatibility patches. Ticket #259
adds the local Go-side `POST /api/auth/vibes-handoff` exchange and stable mirror
tables without changing Core, Views, UI, Chat, Task, Agent, or realtime code.
The browser reaches that exchange through `/api/tag`; the Go service consumes
the opaque code from the configured loopback-only VIBES endpoint, then sets the
normal Multica cookies. The service secret stays in ignored local environment
state. Production topology remains intentionally outside this tracer bullet.

Ticket #262 mounts Multica's existing `IssuesPage`, `IssueDetailRoute`, modal
registry, API client, React Query caches, and realtime protocol inside the Tag
host. The only creation affordance added here opens Multica's existing create
modal. No reusable package was changed, so there is no package sync cost or
upstream patch to carry; the vocabulary and route wiring remain host-only.

## Local commands

```sh
pnpm --filter @multica/tag-host test
pnpm --filter @multica/tag-host typecheck
pnpm --filter @multica/tag-host build
pnpm --filter @multica/tag-host dev
```
