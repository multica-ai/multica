# TC-211: Channel workdir GC (OPE-3458)

## Associated Issues / PRs

- Issue: OPE-3458
- PR: !426 (Gitee)

## Feature Summary

Channel-origin mention tasks (`@agent` in a channel) now have a dedicated
GC path. Previously they fell through `gcMetaForTask`'s `default` branch,
wrote no `.gc_meta.json`, and were reclaimable only by orphan-by-mtime —
which could blindly delete an envRoot a follow-up task was about to resume
into. Now they write `GCKindChannel` meta and are reclaimed only when:
the task is terminal, past `GCTTL`, the `(agent, channel, thread)` lane is
quiet past `GCTTL`, and the envRoot is not under active reference.

## Design Decision

Direction 2 (lane-level GC) chosen over Direction 1 (parameterized
injection / stateless shared workdir). Workdir statefulness comes from
`PriorWorkDir` + session resume, not from ChannelID binding; eliminating
GC via stateless sharing would require dropping resume semantics (cold
start every @mention) and re-introducing the cross-channel concurrency
race that per-TaskID envRoot + `activeEnvRoots` refcounting already
solves. Multica's parallel model (`MaxConcurrentTasks`, per-task process)
is incompatible with slock's per-agent shared-workdir + serialized
resident model.

## Affected Files

- `server/internal/daemon/execenv/execenv.go` — `GCKindChannel` constant + `GCMeta` fields (`AgentID`, `ChannelID`, `ChannelThreadID`)
- `server/internal/daemon/daemon.go` — `gcMetaForTask` channel branch
- `server/internal/daemon/gc.go` — `gcDecisionChannel` + dispatch registration
- `server/internal/daemon/gc_test.go` — 6 decision cases
- `server/internal/daemon/client.go` — `GetChannelLaneGCCheck`
- `server/internal/handler/daemon.go` — `GET /api/daemon/channels/{channelId}/gc-check`
- `server/internal/handler/daemon_test.go` — handler scenarios
- `server/pkg/db/queries/agent.sql` + generated — `GetChannelLaneLastActivity`

## Behavior Change

| Scenario | Before | After |
|---|---|---|
| Existing channel envRoot (no meta) | orphan-by-mtime (30d) | unchanged |
| New channel task | no meta, blind reclaim | writes meta, lane-liveness GC |
| Issue/Chat/Autopilot/QuickCreate | — | zero change |

## Commit SHAs

- (maintainer to fill on cherry-pick)
