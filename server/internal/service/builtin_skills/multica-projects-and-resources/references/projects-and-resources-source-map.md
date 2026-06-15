# Projects and resources source map

- `server/cmd/multica/cmd_project.go` registers project `list`, `get`, `create`, `update`, `delete`, and `status`.
- The same file registers `project resource list/add/update/remove`.
- `project create --repo` attaches `github_repo` resources during project creation.
- `project resource add` supports shortcuts for `github_repo` (`--url`, `--default-branch-hint`) and `local_directory` (`--local-path`, `--daemon-id`, `--ref-label`), or generic `--ref '<json>'`.
- `project resource update` merges shortcut edits with existing `resource_ref` so a partial edit does not clobber required fields.
- `server/cmd/server/router.go` exposes `/api/projects` plus `/api/projects/{projectId}/resources` routes.
- `server/pkg/db/queries/project_resource.sql` is the CRUD query surface for `project_resource` rows.
- Project resources are written into `.multica/project/resources.json` for agent workdirs.

## Project Eidetix binding (admin-only; not agent-facing)

`multica project eidetix set/show/clear/enable/disable` binds a project to a
partner Eidetix shared-context graph. This is a workspace owner/admin config
command — agents never run it — so it is intentionally **not** documented in
this skill's agent-facing body. Sources:

- CLI: `server/cmd/multica/cmd_project_eidetix.go`
- REST: `server/internal/handler/eidetix.go` (`/api/projects/{id}/eidetix`)
- Storage: `eidetix_project_config` (`server/migrations/120_eidetix_project_config.up.sql`)
- Claim-time injection: `applyEidetixToClaim` in `server/internal/handler/eidetix.go`
