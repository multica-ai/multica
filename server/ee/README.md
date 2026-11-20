## server/ee

Commercial server features. Files here are covered
by [LICENSE](./LICENSE), not by the Multica License.

- **Dependency direction.** Code here may import open-source packages. Open-source
  code never imports `server/ee`; `internal/extension` checks this in
  `TestOpenSourceCodeDoesNotImportEE`.
- **Attaching.** A package registers its routes with `extension.RegisterWorkspaceRoutes`
  from `init`, and is listed in `ee.go`. `cmd/server/ee.go` links this directory into
  the server; `go build -tags oss ./cmd/...` builds the open-source server without it.
- **Handlers** use the exported surface in `internal/handler/extension_api.go` for
  request context, realtime events and visibility.
- **Schema** stays in the shared `migrations/` sequence. Queries live next to the
  package and generate into its own sqlc package (`sqlc.yaml`).

| Package | Feature |
| --- | --- |
| `projectfolder` | Project folders (MUL-9390) |
