# Platform CLI Runtime Smoke Test Design

## 1. Objective

Add a repeatable Multica source-level integration test that launches the real Platform Agent CLI through Multica's production Codex backend and verifies the minimum app-server lifecycle end to end.

The test records compatibility in the Multica repository without adding a private protocol family or changing production runtime behavior.

## 2. Scope

### Included

- A Go integration test under `server/pkg/agent`.
- The existing `agentintegration` build tag.
- The existing `MULTICA_RUN_REAL_AGENT_SMOKE=1` safety gate.
- An explicit `MULTICA_PLATFORM_AGENT_CLI_PATH` executable path.
- Real subprocess execution through `New("codex", Config{ExecutablePath: ...})`.
- Assertions for CLI version, completed result, final mock output, session ID, and absence of tools.
- A documented command that reproduces the test locally.

### Excluded

- Changes to the Codex backend or daemon production code.
- A new Multica protocol family.
- Bundling the Platform Agent CLI into the Multica repository.
- Platform Agent API, Skill, Command, Tool, or MCP integration.
- Database, migration, frontend, or Runtime Profile API changes.

## 3. Test Boundary

The test exercises this path:

    Multica pkg/agent Codex backend
      -> external Platform Agent CLI executable
      -> app-server --listen stdio://
      -> initialize
      -> thread/start
      -> turn/start
      -> agentMessage/final_answer
      -> turn/completed
      -> Multica agent.Result

Runtime Profile registration, daemon task pickup, Issue creation, and comment delivery were already verified by the live workspace run. This source-level test focuses on the protocol boundary that Multica owns and can execute deterministically.

## 4. Test Design

The test is named:

    TestPlatformAgentCLIRealCodexCompatibility

Execution gates:

1. Call `requireRealAgentSmoke(t)` before executable lookup.
2. Skip in Go short mode.
3. Read `MULTICA_PLATFORM_AGENT_CLI_PATH`.
4. Require an absolute path to an executable regular file.
5. Run `--version` and require output containing `platform-agent-cli`.

Backend execution:

1. Construct the production Codex backend with the external executable path.
2. Execute the prompt `multica source integration smoke` in a temporary working directory.
3. Drain the message stream concurrently.
4. Wait for the Result with a bounded context and timeout.

Assertions:

- Result status is `completed`.
- Result error is empty.
- Result output equals `Mock Runtime 已收到任务：multica source integration smoke`.
- Result session ID is non-empty.
- The stream contains at least one text message.
- The stream contains no tool-use or tool-result messages.

## 5. Error Handling

- Missing opt-in gate: skip with the existing repository-standard message.
- Missing CLI path: skip with the required environment variable name.
- Relative, missing, non-regular, or non-executable path: fail before backend creation.
- Version command failure: fail with captured bounded output.
- Backend startup or protocol failure: fail with Result status and error.
- Timeout: fail after the bounded test deadline.

## 6. Verification

Focused real-binary smoke:

    cd server
    MULTICA_RUN_REAL_AGENT_SMOKE=1 \
    MULTICA_PLATFORM_AGENT_CLI_PATH=/Users/zxx/Documents/技术学习/platform-agent-cli/bin/platform-agent-cli \
    go test -tags=agentintegration ./pkg/agent \
      -run TestPlatformAgentCLIRealCodexCompatibility -count=1 -v

Default regression test:

    cd server
    go test ./pkg/agent -count=1

The new test remains excluded from default test runs because it executes a user-supplied external binary.

## 7. Change Record

All changes live on branch `codex/platform-cli-runtime-smoke`. The design, implementation plan, integration test, verification output, and final commit provide the reviewable modification record.
