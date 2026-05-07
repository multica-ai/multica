# server/internal/cerebro/access

Cerebro-fork access control: project-level access, restrict+pick flow, key-lock policy.

**Owns:** HTTP handlers, services, and DB access for the cerebro project-access feature.

**May land here:** Endpoints, services, repository helpers tied to cerebro access semantics.

**May NOT land here:** Upstream multica permission/role logic. Cross-cutting middleware (lives in `server/internal/middleware`).

**Naming:** files use snake_case; handlers end in `_handler.go`, services in `_service.go`.

**Upstream-import-niveau:** L4 (cerebro-only). Never imported by upstream packages.
