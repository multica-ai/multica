# TC-210: Fix agy tool-result steps leaking into issue comments (OPE-3445)

## Associated Issues / PRs

- Issue: OPE-3445
- PR: !427 (initial fix — sync re-discovery)
- PR: !432 (this fix — tool-result step filtering)

## Feature Summary

`multica run agy` output is correctly pushed to the issue comment thread.
Tool call result steps (LIST_DIRECTORY, RUN_COMMAND, etc.) are filtered out
and never appear as issue comments; only genuine PLANNER_RESPONSE replies do.

## Affected Files

- `server/cmd/multica/cmd_run_agy.go`
- `server/cmd/multica/cmd_run_test.go`

## Root Cause (documented for reference)

AGY transcript format differs from Claude:
- **Claude**: tool results embedded in next PLANNER_RESPONSE content → `lastStepHadToolCalls` flag works
- **AGY**: tool results emitted as independent MODEL steps with type=TOOL_NAME (e.g. `RUN_COMMAND`, `LIST_DIRECTORY`) → old flag was ineffective

Fix: `mapEntry()` now only dispatches MODEL steps with `type==PLANNER_RESPONSE` to `mapModelResponse()`.

## Commit SHAs

- `524f09bb0` — !427 merge (initial fix)
- `a721e4655` — !432 tool-result step filtering fix
