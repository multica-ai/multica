# Projects resources source map

- `server/cmd/multica/cmd_project.go` registers project `list`, `get`,
  `create`, `update`, `delete`, `status` and `project resource
  list/add/update/remove`.
- `project create --repo` attaches `github_repo` resources during creation.
- `--start-date` / `--due-date`: calendar days (`YYYY-MM-DD`, like issue
  dates); update preserves omitted, `""` clears.
- `project resource add` shortcuts: `github_repo` (`--url`, non-JSON `--ref`
  checked), `local_directory` (`--local-path`, `--daemon-id`).
- `project resource update` merges shortcut edits into existing
  `resource_ref` so partial edits keep other fields.
- `server/cmd/server/router.go` exposes `/api/projects` plus
  `/api/projects/{projectId}/resources/*`; `server/pkg/db/queries/project_resource.sql`
  is the CRUD surface for the `project_resource` table.
- Project resources are written into `.multica/project/resources.json` in
  agent workdirs.
- `github_repo.resource_ref.ref` is lifted into daemon `RepoData.Ref` by
  `server/internal/handler/daemon.go` when preparing agent repos.
- Referring to a project in a comment:
  `[Label](mention://project/<uuid>)` — render-only link; `project` not in
  `util.MentionRe` type group, enqueues nothing, notifies nobody.
- Project `description` is injected as durable context into every task bound
  to the project.
