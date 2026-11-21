# Tomcat 0.1.97 protocol contract

Reference release: [cli-v0.1.97](https://github.com/teisun/tomcat-agent/releases/tag/cli-v0.1.97), commit `faffc91ccee83f024407e37d2ec8c41c6e0dbd25`.

The macOS x86_64 reference archive is `tomcat-cli-v0.1.97-x86_64-apple-darwin.tar.gz`, SHA-256 `4124e79efeff53d37f18fa4f753bcf60c068700bf87a60210c5c4edd61131479`; its executable SHA-256 is `55923bda02e4f496f9c9550444f0014fb45cfa0fae1607763d4b351e0cf3ac9e`. Check the release's `SHA256SUMS` before running it. This is not a claim of real-runtime validation on Linux or Windows.

## Wire surface and ownership

- `tomcat acp` speaks newline-delimited JSON-RPC over stdio, beginning with `initialize` at ACP protocol version 1. It forwards work to Tomcat's separately owned shared service. Multica owns only the task's forwarder process, not that service.
- `tomcat serve models` reads the saved catalog without starting a service, creating a session, or making a model request. The adapter consumes `models[].id`, `provider`, `keyPresent`, `supportedReasoningLevels`, and `defaultModelId`. IDs are opaque; only entries with saved credentials are offered. Credentials, endpoint URLs, and internal model names are not forwarded to the picker. Refresh does not use an adapter cache.
- `session/new` and `session/load` take an absolute `cwd` and `mcpServers`; load also takes the saved `sessionId`. HTTP/SSE MCP entries are filtered by the peer's advertised capabilities. Replayed history during load is not emitted as this run's output.
- Configuration is ordered: new/load, optional `session/set_config_option` with `configId: "model"`, then `configId: "effort"`, then `session/prompt`. The effort choices come from the loaded session, not a guess based on the global default. An empty Multica thinking level sends `default` to clear the session override when the peer exposes effort. A nonempty level must be supported and confirmed by the reply before a prompt is sent. A setup failure preserves an existing session rather than silently replacing it.
- Load error `-32603` / `unknown_session` is a missing-session rejection. `session_config_failed` is not: a startup configuration problem must not discard the existing session. Busy, permission, or transient errors likewise do not imply the session is gone.
- Only a valid terminal prompt result can finish a turn. Cancellation closes stdin, allowing the forwarder to acknowledge cleanup of its task in the shared service. The adapter allows 65 seconds for shutdown; missing acknowledgement or transport failure reports `cleanup_unconfirmed`. It never stops the shared service. The daemon's shorter handoff timeout also fails closed instead of authorizing worktree cleanup or automatic continuation.
- Instructions are read from task `AGENTS.md`; injected skills use `.agents/skills/<name>/SKILL.md`. They are not duplicated into a user prompt. The integration test checks both the project instruction and a skill description in the real model request.

## Reproduction

Default `TestTomcat*` tests use test-owned fake executables and require neither an installed CLI nor an account. The real protocol test is opt-in, uses only a local HTTP model fixture and a disposable HOME, and leaves the user's running Tomcat service untouched:

```sh
cd server
MULTICA_RUN_REAL_AGENT_SMOKE=1 \
MULTICA_TOMCAT_TEST_BIN=/absolute/path/to/verified/tomcat \
go test -tags=agentintegration ./pkg/agent \
  -run '^TestTomcatSharedServeWithHermesClient$' -count=1 -v
```

Use the verified binary, not a user's installed executable. The test exercises discovery, configuration and continuation, real tool output, injected instructions/skills, cancellation, and shared-service cleanup. A CLI upgrade does not update a service that is already running: users must restart their shared service separately after upgrading.
