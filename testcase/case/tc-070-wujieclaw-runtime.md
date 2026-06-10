Purpose: Verify that the Fork-added WujieClaw runtime provider is auto-discovered, registered, executable, and shown distinctly in the runtime UI.

Associated issue: OPE-2247
Gitee PR: TBD
Commits: TBD

Feature summary: Add `wujieclaw` as an OpenClaw-compatible runtime provider, using the `wujieclaw` CLI command and WujieClaw-specific UI identity.

Affected source:
- `server/internal/daemon/config.go`
- `server/internal/daemon/daemon.go`
- `server/internal/daemon/execenv/`
- `server/internal/daemon/local_skills.go`
- `server/pkg/agent/`
- `packages/views/runtimes/`

Preconditions:
- The Multica daemon can run on a machine where `wujieclaw` is on PATH, or `MULTICA_WUJECLAW_PATH` points to a valid executable.
- A WujieClaw/OpenClaw-compatible config has at least one registered agent if model discovery is being verified.
- The web runtime page is reachable.

User flow:
1. Start the daemon with `wujieclaw` available on PATH or via `MULTICA_WUJECLAW_PATH`.
2. Verify daemon registration includes a runtime with provider `wujieclaw`.
3. Open the Runtimes page and verify the runtime uses the WujieClaw display name and a distinct temporary icon.
4. Open the runtime detail page and verify the provider/runtime name is shown as `WujieClaw`.
5. Trigger model discovery for the runtime.
6. Assign an agent to the WujieClaw runtime and run a disposable issue task.

Expected results:
- The daemon discovers `wujieclaw` independently from `openclaw`.
- The backend factory routes provider `wujieclaw` through the OpenClaw-compatible backend while defaulting the executable to `wujieclaw`.
- Per-task environment setup writes `AGENTS.md`, native `skills/`, and an OpenClaw-compatible config for WujieClaw tasks.
- Model discovery reads `wujieclaw agents list` output and returns models tagged with provider `wujieclaw`.
- The UI displays `WujieClaw` with a distinct placeholder icon until the official logo asset is provided.

Notes for automation: If a real `wujieclaw` binary is unavailable, use daemon/model parser unit tests and a fake executable fixture for discovery coverage, then mark real runtime execution as fixture-blocked.
