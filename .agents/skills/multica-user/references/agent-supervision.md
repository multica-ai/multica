# Supervising platform agents (multica CLI)

All commands are plain `multica ...` invocations. Reads are free; writes
(cancel-task, rerun-task, steering comments, supplements) need user
confirmation.

## Agent roster

```bash
multica agent list --output json          # id is what mention://agent/<id> uses
multica agent get <id>
multica squad list --output json          # mention://squad/<id> runs the leader
```

## Seeing what an agent did on an issue

```bash
multica issue runs <issue-id>                 # full execution history, newest first
multica issue runs <issue-id> --active        # only in-flight runs ("is someone on it now?")
multica issue runs <issue-id> --siblings      # in-flight runs across the sub-issue family
multica issue run-messages <run-id> --issue <issue-id>   # full message stream of one run
multica issue usage <issue-id>                # aggregated token usage
```

`run-messages` is the primary review tool: the agent's plan, tool activity,
and final result. Read it before judging an agent from the comment thread
alone. `--active --output json` marks runs with `can_supplement: true` (able to
take another steered message). `--siblings` before starting overlapping code or
PR work; it's advisory, not a reservation.

## Intervening

| Action | Command |
| --- | --- |
| Cancel a run | `multica issue cancel-task <issue-id> <run-id>` |
| Repeat one run (same agent + trigger context) | `multica issue rerun-task <issue-id> <run-id>` |
| Add a note to one exact finished/running run | raw API `POST /api/issues/{id}/tasks/{taskId}/supplements` — see api.md |

## Steering a running agent

A plain comment reaches a busy agent only after its current run ends. To add
the message to the in-progress turn instead:

```bash
multica issue comment add <issue-id> --content "..." --steer <run-id>        # exact runs
multica issue comment add <issue-id> --content "..." --steer-active         # every running agent you may steer
```

- Only runs with `can_supplement: true` can be steered; a run that cannot take
  the message falls back to the normal trigger, and the command reports per run
  whether it was steered, queued, coalesced, deferred, or blocked.
- One agent can add at most 3 steered messages to a run (10 across all agents).
- If the steered run ends before reading the message, it is handled in a
  follow-up run.
- Steering is the right default when the user wants to redirect an agent
  mid-run ("tell it to also handle X") rather than queue new work.
