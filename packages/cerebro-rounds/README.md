# Cerebro Rounds

Rounds hold issue triggers until the owner starts a controlled run. Released
tasks keep the normal task lifecycle, progress events, cancellation and usage
recording.

## Safe AI batch execution

The server-owned `firtal-gateway` runtime may submit a released Round task to
the registry Anthropic Message Batches API only when all of these are true:

- task context explicitly contains `batch_tool_mode: "none"`;
- current tool resolution finds no callable tools for the agent;
- the selected logical model advertises `supports_batch: true` in the live
  registry model catalog.

This path is intentionally unavailable to daemon, CLI and ACP runtimes. A Round
task whose agent can call tools (including coding or connection tools) uses the
existing synchronous tool loop.

Before the registry accepts a job, unsupported capability, authentication,
transport and malformed responses fall back to the existing synchronous model
call. After acceptance, the runtime polls the job and reads its JSONL result.
Timeouts and invalid results trigger a best-effort batch cancellation before
synchronous fallback. Cancellation of the parent task cancels the batch and
does not start fallback work.

Batch completions use the same task message, token usage, cost, progress and
completion recording as synchronous completions.
