# BB runtime

Run Multica issues in visible [BB](https://github.com/get-bb/bb) Codex threads. Multica owns task scheduling and task identity; BB owns the agent session, tools, permissions and MCP configuration.

**Diagnostic visibility:** BB main (`365d7c523`) records plugin-contributed environment values in `provider.env-resolved`. Multica task tokens expire after at most 24 hours and terminal transitions attempt to revoke them immediately. While valid, a recorded token remains usable as the task actor. Treat BB diagnostic access accordingly. Long-lived credentials in custom environment variables are also affected. Masking plugin values in BB diagnostics is recommended separately; the real-agent smoke below uses only a random non-secret value.

```text
Multica daemon → bb multica run --request-stdin → BB SDK → Codex thread
                                                  └─ task environment
```

The companion plugin uses BB's `experimental_contributeEnv` API. The plugin keeps credentials in an execution-scoped memory map, supplies them to the corresponding provider process environment, and removes its copy after the provider stops. Prompts and persisted thread metadata contain no task credentials; BB diagnostic behavior is described above. There is no credential file or shell `source` step.

## Setup

Requires a running BB with plugin SDK 0.6.132 or later and a working Codex provider. Install from this checkout:

```sh
bb plugin install ./integrations/bb
bb project list
bb machine list
bb plugin config multica-runtime set projectId <project-id>
bb plugin config multica-runtime set hostId <machine-id>
bb multica status
bb multica models
multica daemon restart
```

Choose a project and machine on the same host as the Multica daemon. Each run uses the working directory prepared by Multica. The daemon registers BB only when the plugin is installed and configured. Set `MULTICA_BB_PATH` if `bb` is not on its PATH.

Model and reasoning selections come from BB's live Codex catalog. The returned session ID is the visible BB thread ID; a subsequent Multica run can resume that thread. BB's provider is stopped between runs so the next run receives fresh task credentials. Cancellation explicitly asks BB to stop and waits for `idle`; if BB cannot confirm release, Multica reports `cleanup_unconfirmed` rather than allowing an automatic retry.

## Initial scope

- Codex through BB, using BB's `auto` permission mode.
- Final output and BB thread ID; live tool-event streaming and usage accounting are not forwarded yet.
- Configure MCP and provider arguments in BB. Multica-managed MCP entries and custom CLI arguments are rejected rather than ignored.
- Task credentials are intentionally ephemeral. A BB/plugin restart fails the active run; retry from Multica to obtain a new task credential.

## Verification

```sh
node --test integrations/bb/server.test.js
(cd server && go test ./pkg/agent -run 'TestBB|TestLaunchHeaderCoversAllSupportedBackends')
```

An opt-in test invokes the installed BB and a real Codex model. It injects a random environment value and compares the agent's SHA-256 with an independent calculation:

```sh
cd server
MULTICA_RUN_REAL_AGENT_SMOKE=1 MULTICA_BB_PATH=/absolute/path/to/bb \
  go test -tags=agentintegration ./pkg/agent -run '^TestBBRealTaskEnvironment$' -count=1 -v
```
