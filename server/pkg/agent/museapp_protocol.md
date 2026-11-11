# Muse app execution contract

The daemon and Muse worker must run on the same host or share the exact workdir
paths. The receptionist wire protocol remains v1. An execution request carries
the task-scoped `task_token`, `server_url`, `workspace_id`, and task prompt.

For daemon tasks, the prompt includes a CLI command prefix with the daemon's
absolute executable path, localhost daemon port, task and agent identity, and
private Multica config root. The worker must use that prefix for each Multica
command, including commands copied from AGENTS.md. It must set `MULTICA_TOKEN`
from its own request's `task_token`; tokens and custom environment secrets are
never included in the prefix. The existing receptionist passes the prompt
through, so it needs no new request fields to deliver this context.

## Cancellation and interrupted execution

The daemon offers `museapp-process-cleanup-v1` when starting a task. The server
must echo that capability before the daemon submits any work. Older daemons
and other providers retain their existing start/cancel behavior.

`POST /v1/tasks/{id}/cancel` may confirm shutdown with:

```json
{"execution_stopped": true}
```

This is an acknowledgement that the worker and its running tools have actually
stopped. A flag written to status.json, a cancellation request accepted by the
receptionist, or an HTTP 200 alone is insufficient. The response must arrive
within the daemon's bounded cleanup window. A terminal `failed` or `cancelled`
status can also report `execution_stopped: true`; `completed` means the worker
finished normally.

The existing receptionist only acknowledges cancellation bookkeeping. With
that version, cancellation, timeout, or loss of contact yields
`cleanup_unconfirmed`. The server blocks automatic continuation on that agent
and runtime. Check and stop any remaining Muse worker and its tools before
manually rerunning. Do not add `execution_stopped: true` to a receptionist
until it can verify that guarantee.

An execute response lost after submission is also an unknown execution
outcome. Without a task id to cancel, the daemon reports unconfirmed cleanup
instead of allowing an automatic retry to launch duplicate work. Explicit
pre-execution HTTP 400/401/403 refusals remain ordinary launch errors.

Daemon restart recovery, prolonged runtime disconnection, and stale-task expiry
also cannot prove a started Muse worker stopped. The server persists
`cleanup_unconfirmed` in the same transaction as these terminal transitions,
before processing retries or exposing the failure to new claims. This applies
to started Muse tasks, including tasks from older Muse daemons; queued or
not-yet-started tasks and other providers retain their normal recovery policy.
