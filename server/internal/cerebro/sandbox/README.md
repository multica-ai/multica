# server/internal/cerebro/sandbox

Cerebro-fork sandbox feature backend (runtime sandbox UI + control plane glue).

**Owns:** Sandbox state endpoints, runtime config helpers exposed to the sandbox UI.

**May land here:** Sandbox CRUD, status endpoints, lifecycle controllers specific to cerebro.

**May NOT land here:** Generic runtime/daemon code (lives in `server/internal/daemon`).

**Naming:** snake_case files.

**Upstream-import-niveau:** L4 (cerebro-only).
