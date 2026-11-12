# Amp adapter protocol audit

Checked against the official `@ampcode/cli` executable
`0.0.1794456033-gb2ff22` on macOS arm64 (2026-11-12).

References:

- https://ampcode.com/docs/cli
- https://ampcode.com/docs/cli/execute-mode
- https://ampcode.com/docs/cli/streaming-json
- https://ampcode.com/docs/cli/settings
- https://ampcode.com/docs/customize/skills

The adapter runs local execute mode with newline-delimited JSON output and
plain prompt input on stdin. Continuation uses `threads continue <thread-id>`;
it never selects the last thread implicitly. The model field selects a native
Amp mode with `--mode`, while Amp owns the actual model routing. No model catalog
is invented, and absent terminal usage is left unreported.

`system/init` supplies the session ID. Assistant content contains text and tool
calls; user content contains tool results. Assistant `end_turn` and
`system/subagent_finished` are progress, not completion. Only `result` with
`subtype:success` and no error establishes success. The terminal boundary is
published immediately, before waiting up to ten seconds for CLI exit. A native
nonzero exit observed during that grace still fails; terminating a process that
remains alive afterward is bounded cleanup and preserves the terminal answer,
with a daemon warning. Cancellation or a deadline after the terminal boundary
also preserves the result. Missing cleanup acknowledgement remains a failure.
Failure clears the final output while retaining the transcript and observed ID.
Permission denials reported by Amp are shown as a redacted transcript warning;
the adapter does not override Amp's configured permission rules.

The adapter requires the documented non-interactive `AMP_API_KEY` access token.
An empty key fails before startup; interactive login is unsupported. A real CLI
test with an invalid token passed and confirmed authentication failure without
exposing the token. A real successful task and continuation could not be verified
because this execution environment has no valid Amp token. Before the key
preflight was added, the credential-free CLI waited until the 45-second task
deadline; it did not produce a terminal result.

Offline subprocess tests cover protocol completion/failure, stdin transport,
redaction, cancellation, timeouts, inherited output pipes, oversized frames,
wrapper exit and local descendant cleanup. Real tests require both the
`agentintegration` build tag and `MULTICA_RUN_REAL_AGENT_SMOKE=1`.

Cancellation owns the local process tree. Amp documents that service-side
asynchronous subagents can survive CLI exit; this adapter has no remote
cancellation API for them. Their thread ID remains available for inspection.
Orbs, runners, managed MCP, live supplements and `max_turns` are unsupported.

Roll out the additive runtime-profile constraint migration before the new
daemon. Older daemons retain their existing providers. Before a daemon downgrade,
disable Amp profiles; historical profile rows remain, including after the down
migration, whose constraint restricts subsequent writes without scanning rows.
