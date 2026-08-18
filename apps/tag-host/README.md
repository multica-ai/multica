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
  adapter, existing-session provider, and a Workspace gate that selects the
  slug/id before child queries or realtime code mount
- Explicit browser bases: `/api/tag` and `<same-origin>/ws/tag`

The direct package build succeeds without compatibility patches. The main
known sync cost is the large unoptimized Chat client chunk in this diagnostic
host; bundle shaping is outside Ticket #258. Identity federation, VIBES-owned
workspace authority, and Production topology are also intentionally outside
this tracer bullet.

## Local commands

```sh
pnpm --filter @multica/tag-host test
pnpm --filter @multica/tag-host typecheck
pnpm --filter @multica/tag-host build
pnpm --filter @multica/tag-host dev
```

