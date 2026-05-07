# server/internal/cerebro/users

Cerebro-fork user/member admin: members admin UI backend, role projections, enforcement toggles.

**Owns:** Cerebro-specific member admin endpoints and projections beyond upstream multica members API.

**May land here:** Admin endpoints, role projections, enforcement-toggle persistence.

**May NOT land here:** Upstream user/auth logic (lives in `server/internal/auth`).

**Naming:** snake_case files; one handler per route group.

**Upstream-import-niveau:** L4 (cerebro-only).
