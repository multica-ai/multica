---
name: smoke
description: Use when a Smoke issue is assigned to you. Runs smoke-test-agentrunner.sh for outer autopilot-triggered issues; replies with the embedded marker token for inner test-verification issues created by the script.
---

# Smoke Skill

**Use when assigned any Smoke issue.**

Read the issue description first to determine which case applies.

## Case A — Outer smoke run

The issue was created by an autopilot or manually and does NOT contain a `SMOKE_OK_` token in the description.

1. Run the smoke test script and capture its output:

   ```bash
   bash /opt/agentfarm/scripts/smoke-test-agentrunner.sh 2>&1
   ```

2. Post a comment on the issue with the outcome:
   - On exit 0: `PASS — smoke test completed successfully.`
   - On non-zero exit: copy the last `FAIL: <phase> — <reason>` line from the output as the comment.
3. Do not create PRs, write code, or take any other action.

**Exception — sync-pipeline runs.** If the issue tells you to run the script with
`SMOKE_SYNC_ISSUE_ID` / `SMOKE_PR_NUMBER` set (the upstream-sync tick dispatches it
that way), the script reports its own verdict onto the sync ticket and the PR. Skip
step 2 entirely in that case: a second comment of your own duplicates the verdict,
and `scripts/sync-tick.sh` reads the machine-readable marker off the PR, not your
prose. Just run the command the issue gives you verbatim.

## Case B — Inner test-verification issue

The issue description contains a `SMOKE_OK_` token (placed there by `smoke-test-agentrunner.sh` phase 8, "smoke task create").

1. Extract the exact `SMOKE_OK_...` token from the description.
2. Post a comment containing **only** that token — no other text.
3. Do not run the smoke script.

## Distinguishing the cases

```bash
# Check if the description contains a marker token
if echo "${ISSUE_DESCRIPTION}" | grep -q 'SMOKE_OK_'; then
  # Case B: reply with the token only
else
  # Case A: run the script
fi
```
