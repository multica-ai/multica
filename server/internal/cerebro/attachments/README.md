# server/internal/cerebro/attachments

Cerebro-fork attachments feature backend (issue/comment attachment handlers + storage glue).

**Owns:** Cerebro attachment endpoints, signed-URL flows, lifecycle hooks.

**May land here:** Upload/download handlers, attachment metadata persistence specific to cerebro.

**May NOT land here:** Generic blob storage primitives (lives in `server/internal/storage`).

**Naming:** snake_case files; one handler per route group.

**Upstream-import-niveau:** L4 (cerebro-only).
