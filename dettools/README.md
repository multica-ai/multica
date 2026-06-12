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
| `coding_comment_extract.go` | `coding_comment_extract` | Extract latest coding-team markers and structured artifacts from task comments. |
| `coding_plan_validate.go` | `coding_plan_validate` | Validate Planner implementation-plan artifacts before Implementer handoff. |

## Import Notes

Use the file stem as the tool name and keep the tool enabled for the agents
whose skills reference it. From the repo root, import or refresh all catalog
tools with:

```bash
for f in dettools/*.go; do
  multica dettool import-file "$f" --output table
done
```

`multica dettool import-file` creates the tool on the first run and updates the
existing tool with the same name on later runs, so the command is safe to reuse
after editing these sources. You can also paste the file contents into the
Multica Tools UI and use the file stem as the tool name.

The deterministic tool runtime advertises workspace-authored steps with a
permissive object schema, so the exact accepted input fields are documented in
each file's top comment.
