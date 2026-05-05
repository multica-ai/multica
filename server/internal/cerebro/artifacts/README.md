# server/internal/cerebro/artifacts

Cerebro-fork artifacts feature backend (folders, origin issues, requesters).

**Owns:** HTTP + service layer for the artifact tree, including folder format, origin-issue link, requester audit.

**May land here:** Artifact CRUD, folder operations, search.

**May NOT land here:** Generic file storage (lives in `server/internal/storage`).

**Naming:** snake_case files; tests colocated as `*_test.go`.

**Upstream-import-niveau:** L4 (cerebro-only).
