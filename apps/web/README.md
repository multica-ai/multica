# Multica Web

## Mobile workspace routes

Mobile workspace UI lives inside the existing Next.js app under:

```text
/{workspaceSlug}/m
/{workspaceSlug}/m/issues
/{workspaceSlug}/m/kanban
/{workspaceSlug}/m/projects
/{workspaceSlug}/m/inbox
/{workspaceSlug}/m/runtime
/{workspaceSlug}/m/chat
/{workspaceSlug}/m/settings
```

`/{workspaceSlug}/m` redirects to `/{workspaceSlug}/m/issues`. These routes are
implemented with the `apps/web/app/[workspaceSlug]/(mobile)/m` route group, so
they do not collide with the existing desktop routes at
`/{workspaceSlug}/issues`, `/{workspaceSlug}/projects`, and related paths.

The mobile shell uses the same web auth, cookie, CSRF, API, and WebSocket setup
as the desktop web routes. The WebSocket client derives `/ws` from the current
origin, so Tailscale Serve can expose a single HTTPS host while mapping `/ws`
directly to the backend.

Expected Tailscale Serve shape:

```text
https://<host>.<tailnet>.ts.net/      -> apps/web Next.js server
https://<host>.<tailnet>.ts.net/api/* -> backend through existing Next rewrite
https://<host>.<tailnet>.ts.net/auth/* -> backend through existing Next rewrite
https://<host>.<tailnet>.ts.net/ws    -> backend :8080 /ws
```

iPhone Safari verification starts at:

```text
https://<host>.<tailnet>.ts.net/{workspaceSlug}/m/issues
```

After login, any placeholder route above should render inside the mobile shell.
