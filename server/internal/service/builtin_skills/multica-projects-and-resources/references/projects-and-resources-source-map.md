# Projects and resources source map

- `server/cmd/multica/cmd_project.go` registers project `list`, `get`, `create`, `update`, `delete`, and `status`.
- The same file registers `project resource list/add/update/remove`.
- `project create --repo` attaches `github_repo` resources during project creation.
- `project resource add` supports shortcuts for `github_repo` (`--url`, non-JSON `--ref` for checkout ref, `--default-branch-hint`) and `local_directory` (`--local-path`, `--daemon-id`, `--ref-label`), or generic JSON `--ref '<json>'`.
- `project resource update` merges shortcut edits with existing `resource_ref` so a partial edit does not clobber required fields; non-JSON `--ref` updates `github_repo.resource_ref.ref`.
- `server/cmd/server/router.go` exposes `/api/projects` plus `/api/projects/{projectId}/resources` routes.
- `server/pkg/db/queries/project_resource.sql` is the CRUD query surface for `project_resource` rows.
- Project resources are written into `.multica/project/resources.json` for agent workdirs.
- `github_repo.resource_ref.ref` is lifted into daemon `RepoData.Ref` by `server/internal/handler/daemon.go`; `server/internal/daemon/daemon.go` stores it per task, and `server/internal/daemon/health.go` uses it as the default `/repo/checkout` ref when the checkout request does not explicitly pass one.
- A project's `description` is injected as durable context for every task in the project. The claim handler (`server/internal/handler/daemon.go`) reads `proj.Description` onto the claim response (`ProjectDescription`, `server/internal/handler/agent.go`); the daemon carries it through `Task` (`server/internal/daemon/types.go`) and `TaskContextForEnv` (`server/internal/daemon/execenv/execenv.go`) into the brief's `## Project Context` section (`server/internal/daemon/execenv/runtime_config.go`) and into `.multica/project/resources.json` as `project_description` (`server/internal/daemon/execenv/context.go`).
