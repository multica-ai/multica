# Droid capability audit

Checked on 2026-11-12 against the [CLI overview](https://docs.factory.com/droid-cli/overview), its linked harness guides, and the official native macOS arm64 Droid 1.8.0 `exec --help`. This audits the local one-shot Multica harness; it does not claim parity with the Factory app or account-backed acceptance. Version, checksum, and protocol provenance are recorded in [the protocol evidence](droid-1.8.0-protocol.md).

“Delegated” means Multica preserves Droid's environment, working directory, and configuration discovery. The feature is implemented by Droid and has not been exercised with an authenticated account here. The fake CLI tests verify Multica's boundaries, not Factory's implementation.

## Execution and configuration contract

The following flag inventory was checked against the installed 1.8.0 help, rather than a hardcoded model list.

| Capability / flags | Multica behavior | Evidence or boundary |
| --- | --- | --- |
| Installation, version, discovery | Optional native CLI, minimum 1.8.0; `MULTICA_DROID_PATH`, `_MODEL`, `_ARGS` | `internal/daemon/droid_test.go`; older 0.150.1 is rejected. Windows execution and Linux execution remain untested. |
| Login / `FACTORY_API_KEY` | Existing execution-machine login or custom environment | Authentication failure is surfaced; keys are scrubbed from stream messages, results, stderr diagnostics, and command logs. No credentials were installed or changed. |
| Prompt / `-f`, `--file`, positional arguments | Task prompt uses stdin; file overrides are filtered, extra positional prompts and unknown options fail before startup | Native option arity is validated. Large Unicode/quoted prompts and wrapper-descendant stdin delivery are tested. |
| `-o`, `--output-format`, `--input-format` | Own one-shot `stream-json` output; alternate transports are filtered | Assistant, tool, init, completion, usage and error mapping use the synthetic protocol fixture. Output `stream-json` remains supported; deprecated input `stream-json` is not used. |
| `--auto`, `--skip-permissions-unsafe` | Own `--auto high`; custom overrides filtered | This supports unattended coding. Droid's command policy still applies; a failed task may have edited files. |
| `-m`, `--model` | Own model from Multica, otherwise Droid default | Opaque public/custom IDs are forwarded; no static model catalog or automatic Thinking picker is advertised. |
| `-r`, `--reasoning-effort` | Custom arguments pass through | Native model validates compatibility; BYOK reasoning is provider-controlled. |
| `--use-spec`, `--spec-model`, `--spec-reasoning-effort` | Custom arguments pass through | Native planning behavior is delegated, not a separate Multica planning UI. |
| `--only-tools`, `--add-tools`, `--remove-tools` | Custom arguments pass through | Includes native MCP selectors; does not provision an MCP server. |
| `--settings`, `--disable-builtin-skills` | Custom arguments pass through | No rewriting of user settings or disabling of native policy. |
| `--tag`, `--log-group-id` | Custom arguments pass through, repeated tags retained | Native metadata, not Multica task identity. |
| `-s`, `--session-id` | Own prior session from the daemon; only observed IDs returned by adapter | End-to-end daemon fixture tests a missing session, exactly one fresh retry, fresh failure, confirmed session, tool use, auth and network failures. |
| `--fork` | Filtered | A new fork must not replace the daemon's continuation identity. |
| `--cwd`, `-w`, `--worktree`, `--worktree-dir` | Filtered; daemon owns working directory / worktree | Prevents execution outside the prepared task environment. |
| `--append-system-prompt`, `--append-system-prompt-file` | Custom overrides filtered; an adapter caller's explicit `ExecOptions.SystemPrompt` uses an owned private file | The daemon leaves this field empty for Droid and delivers its brief through task `AGENTS.md`. Adapter tests cover private mode and cleanup; they do not prove native instruction discovery. |
| `--mission`, `--worker-model`, `--worker-reasoning-effort`, `--validator-model`, `--validator-reasoning-effort` | Filtered | Factory Mission orchestration is outside the current per-turn process and result contract. |
| `--list-tools`, `-h`, `--help`, `-v`, `--version` | Filtered from task arguments | Exit-only invocations cannot replace the task protocol. Discovery/version probes are separate. |
| Runtime wrappers / custom argument forms | Launch prefix preserved, protected flags filtered there too | Short attached values, equals forms, extra args and agent custom args covered by adapter/daemon tests. |
| Terminal result, stderr, cancellation | Completion plus clean root exit required; errors remain sticky; owned pipes and bounded descendant cleanup | Fake-process regression tests include inherited stdout/stderr, late completion/errors, root nonzero exit, missing completion, oversized frames and cancellation after completion. |
| `max_turns`, live supplements | Unsupported; use task timeout | No unsupported CLI option is invented. Persistent JSON-RPC/ACP requires a separate transport implementation. |

Execution reference: [Droid Exec](https://docs.factory.com/droid-exec/overview). Installation and sign-in: [Quickstart](https://docs.factory.com/droid-cli/quickstart). Model identifiers and BYOK: [Models](https://docs.factory.com/models), [BYOK](https://docs.factory.com/model-independence/byok). Current model names and defaults are intentionally not copied into the adapter.

## Every capability named by the CLI overview

| Overview capability | Audit result |
| --- | --- |
| Terminal / editor / tests / Git workflow | Multica invokes headless exec in its task cwd. Tools are mapped to existing task events. TUI keyboard controls, bash mode, slash-command menus and IDE integration remain native surfaces, not a Multica terminal emulator. See [CLI reference](https://docs.factory.com/droid-cli/cli-reference). |
| Repository context / `AGENTS.md` | The daemon delivers its brief through task `AGENTS.md`, without duplicate inline injection. Existing content and brief stability are covered by execution-environment tests. Gated `TestDroidRealAgentsMDCanary` checks native discovery with a random marker absent from the user prompt and no inline instructions or tools; account-backed execution remains pending. See [AGENTS.md](https://docs.factory.com/harness/agents-md). |
| Skills | Multica injects project skills into `.factory/skills` and discovers personal skills there. Native compatibility locations, builtin/plugin skills and progressive loading remain delegated; Multica does not claim to enumerate all native sources. See [Skills](https://docs.factory.com/harness/skills). |
| Custom Droids | Existing project/personal `.factory/droids` definitions remain delegated. A native Task subagent is not a separately registered Multica agent; no promise to stream every child session's events. See [Custom Droids](https://docs.factory.com/harness/subagents). |
| MCP / connected knowledge | Native `.factory/mcp.json` discovery and pre-authenticated servers are delegated. Multica-managed MCP configuration fails explicitly instead of being ignored or overwriting native files. Interactive OAuth must be completed on the execution machine. See [MCP](https://docs.factory.com/harness/mcp). |
| Hooks | Existing native lifecycle hooks remain delegated; Multica neither installs nor manages them. See [Hooks](https://docs.factory.com/harness/hooks). |
| Plugins | Existing native plugin bundles remain delegated; no marketplace installation occurs during discovery. See [Plugins](https://docs.factory.com/harness/plugins). |
| Missions | Not integrated; mission and dependent worker/validator flags are filtered together. See [Missions](https://docs.factory.com/missions/overview). |
| Headless scripts / CI / structured output | Implemented through one exec process per task turn. No long-lived raw JSON-RPC, ACP or SDK bridge is claimed. See execution contract above. |
| Plans, edits, tests and review | Native task behavior; a successful completion is not proof that a project's tests passed. Agent output and tool events carry that evidence. Actual successful tool use still requires the account-backed acceptance below. |

Related configuration was also checked: [settings](https://docs.factory.com/droid-cli/settings), [permission rules](https://docs.factory.com/autonomy-and-safety/permission-rules), [output styles](https://docs.factory.com/droid-cli/output-styles), and [Sessions API](https://docs.factory.com/api-reference/sessions). Native user/project settings, policy, output styles and cloud-sync preferences remain user-controlled. The local harness does not call the cloud Sessions API or claim cross-machine transcript migration. `ask` policies can stop headless execution even with `--auto high`; block rules remain enforced.

## Remaining acceptance and rollout

- The success fixture is synthetic. No genuine successful run, native tool call, continuation, native MCP/hooks/plugins/subagents, or running-app Droid assignment has been verified with a Factory account. Native `AGENTS.md` loading is also unverified; a passing fake-CLI test is not a canary result. The earlier real attempt stopped at authentication. Keep PR #10399 in draft until a credential owner completes `TestDroidRealAgentsMDCanary`, `TestDroidRealTaskSmoke`, and the running-app acceptance described in the PR.
- Default tests use fake executables, temporary homes and local HTTP fixtures. No shared database, Factory account, production resource or user configuration was changed by this audit.
- Migration 652 follows the rebased main's 651. The down whitelist matches main's last runtime-family whitelist exactly. Apply the additive migration before saving Droid profiles; retain it when rolling application binaries back. Disable Droid assignments before downgrading the daemon.
