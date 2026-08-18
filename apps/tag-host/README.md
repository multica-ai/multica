# VIBES Tag host tracer bullet

This local-only TanStack Start host mounts Multica's original `@multica/core`,
`@multica/views`, and `@multica/ui` packages directly. It does not copy their
source, replace the Go API, or establish a permanent fork.

## Compatibility record

- Baseline: `38c992ad0a757434fb51584fa34e3bc57d1b78e1`
- TanStack Router/Start: `1.159.5`
- Vite: `8.0.1`
- React: the Multica workspace catalog (`19.2.3` at this baseline)
- Reusable Multica package changes: none
- Host-only seams: `/tag` router base, client-only Chat route, navigation
  adapter, cookie-session provider, and a Workspace gate that selects the
  slug/id before child queries or realtime code mount
- Explicit browser bases: `/api/tag` and `<same-origin>/ws/tag`

The direct package build succeeds without compatibility patches. Ticket #259
adds the local Go-side `POST /api/auth/vibes-handoff` exchange and stable mirror
tables without changing Core, Views, UI, Chat, Task, Agent, or realtime code.
The browser reaches that exchange through `/api/tag`; the Go service consumes
the opaque code from the configured loopback-only VIBES endpoint, then sets the
normal Multica cookies. The service secret stays in ignored local environment
state. Production topology remains intentionally outside this tracer bullet.

## Local commands

```sh
pnpm --filter @multica/tag-host test
pnpm --filter @multica/tag-host typecheck
pnpm --filter @multica/tag-host build
pnpm --filter @multica/tag-host dev
```
