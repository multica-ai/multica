# Community CLI Source Map

Evidence for claims in this Skill. Recheck these locations whenever the
community CLI or authentication behavior changes. This map intentionally does
not describe commands added only on a feature branch.

| Claim | Source |
| --- | --- |
| Cloud setup uses the default server and app URLs | `server/cmd/multica/cmd_setup.go` and `server/cmd/multica/cmd_agent.go` in `upstream/main` |
| `multica setup` configures a profile and runs browser authentication | `server/cmd/multica/cmd_setup.go` and `server/cmd/multica/cmd_login.go` in `upstream/main` |
| `multica login` may wait for workspace creation after authentication | `server/cmd/multica/cmd_login.go:149-202` in `upstream/main` |
| `multica workspace list --output json` returns the access-controlled workspace array | `server/cmd/multica/cmd_workspace.go:111-112,162-194` in `upstream/main` |
| Workspace creation requires a display name and immutable slug | `server/cmd/multica/cmd_workspace.go:197-251` in `upstream/main` |
| Workspace switching accepts the identifiers shown by workspace list | `server/cmd/multica/cmd_workspace.go:302-382` in `upstream/main` |
| Resource list/get/create/update commands and JSON output are discovered from the installed CLI | `server/cmd/multica/cmd_issue.go`, `cmd_agent.go`, `cmd_squad.go`, `cmd_skill.go`, `cmd_project.go`, and `cmd_autopilot.go` in `upstream/main` |
| The Skill must not rely on text auth status because the community command may print token material | `server/cmd/multica/cmd_auth.go` in `upstream/main` |

The installed CLI is the capability boundary. Before using a less common
resource command, run its local `--help`; if the command or required structured
output is missing, report the Web-only step instead of guessing a flag or
calling an HTTP endpoint directly.
