# Deterministic Tool Step Catalog

These files are workspace-authored deterministic tool sources. Import each `.go`
file into Multica as a deterministic tool with the matching name below.

Each tool is pure input-to-output: it does not read files, call ADO, call
Multica, post comments, update issues, or mutate repositories. The calling skill
remains responsible for performing any approved external action.

| File | Tool name | Use from skills |
| --- | --- | --- |
| `pipeline_state_parse.go` | `pipeline_state_parse` | Parse a Multica issue description into pipeline state/config/body fields. |
| `coding_watchdog_analyze.go` | `coding_watchdog_analyze` | Determine dropped coding-team handoff notifications from supplied state/comments. |
| `review_scope_partition.go` | `review_scope_partition` | Partition changed files into Python, .NET, DevOps, and misc review scopes. |
| `ado_payload_normalize.go` | `ado_payload_normalize` | Normalize supplied Azure DevOps work-item, relation, and comment JSON. |

## Import Notes

Use the file stem as the tool name, paste the file contents as the deterministic
tool source, and keep the tool enabled for the agents whose skills reference it.

The deterministic tool runtime advertises workspace-authored steps with a
permissive object schema, so the exact accepted input fields are documented in
each file's top comment.

