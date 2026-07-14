# Cline 3.x NDJSON → Multica Adapter Plan

**Status:** Agreed design (not yet implemented)  
**Scope:** Adapt a Cline-based internal coding CLI to Multica via **`--json` NDJSON (形态 B / Cline 3.x)**  
**Out of scope:** ACP (`--acp`), Codex-style JSON-RPC app-server, upstream contribution process  

Related:

- Probe / capture cookbook: [`docs/cline-ndjson-probe.md`](./cline-ndjson-probe.md)
- Multica control plane: `server/internal/daemon/daemon.go` (`runTask`)
- Backend contract: `server/pkg/agent/agent.go` (`Backend.Execute` → `Message` / `Result`)
- Custom profiles: [`docs/custom-runtimes.md`](./custom-runtimes.md)

---

## 1. Goal

Let Multica’s local daemon **spawn an internal CLI** that speaks **Cline 3.x NDJSON**, stream tool/text events to the UI, and finish tasks with correct status / usage / optional session resume — without implementing ACP.

Multica still does **not** call the LLM. Business reads/writes stay on `multica` CLI inside the agent process (`MULTICA_TOKEN`, etc.).

---

## 2. Confirmed inputs

| Fact | Decision impact |
| --- | --- |
| Wire format is **形态 B** (Cline 3.x envelopes) | Parse `agent_event` / `hook_event` / `run_result`, not Overview `say`/`ask` only |
| Flags present: `--json`, `--auto-approve`, `-c`, `--id`, `-m`, `-t` | Build argv from this set |
| **`-s` / system prompt not usable** | Do **not** pass `-s`; inject brief by **prepending** to the user prompt |
| Prefer **simplest** reliable path | One `cline` Backend; internal binary name via Custom Runtime Profile |

---

## 3. Chosen approach

### 3.1 Provider key: `cline`

Add a first-class Multica provider / `protocol_family` named **`cline`**.

| Host setup | How it runs |
| --- | --- |
| Binary on PATH as `cline` (or `MULTICA_CLINE_PATH`) | Built-in daemon probe registers runtime |
| Internal binary, different name/path | **Custom Runtime Profile**: `protocol_family=cline`, `command_name=/path/to/internal-cli` |

One NDJSON parser serves both. No second Backend for branding.

### 3.2 Why not the alternatives

| Alternative | Why rejected |
| --- | --- |
| Reuse `cursor` / `opencode` / `claude` family | Different event schema and flags |
| Custom profile only (no new Backend) | No existing family understands Cline 3.x NDJSON |
| ACP / JSON-RPC | Internal CLI has no ACP SDK; Multica does not need it for this path |

---

## 4. Control-plane flow

```text
Server  enqueue task → claim
Daemon  prepare workdir / skills / AGENTS.md (best-effort)
        BuildPrompt(task) → userPrompt
        runtimeBrief → ExecOptions.SystemPrompt  (inline path)
        agent.New("cline").Execute(...)
Backend argv:
          <cli> --json --auto-approve true
                -c <workdir>
                [-m <model>]
                [--id <prior_session>]
                "<SystemPrompt>\n\n<userPrompt>"   # NO -s
        parse stdout NDJSON → Message stream
        last run_result → Result
Daemon  CompleteTask / FailTask (+ session_id, work_dir, usage)
```

Timeout: **Multica `runContext` / daemon timeout owns the wall clock.**  
First implementation **does not pass CLI `-t`**, to avoid dual-timeout semantics. May revisit later if needed.

---

## 5. Launch contract

### 5.1 Required / used flags

```bash
cli --json --auto-approve true \
  -c <workdir> \
  [-m <model>] \
  [--id <session_id>] \
  "<combined_prompt>"
```

| Flag | Source | Notes |
| --- | --- | --- |
| `--json` | Fixed | NDJSON on stdout |
| `--auto-approve true` | Fixed | Unattended tool runs |
| `-c` | `opts.Cwd` | Also set `cmd.Dir` |
| `-m` | `opts.Model` | Agent model or daemon default |
| `--id` | `opts.ResumeSessionID` | Only when non-empty |
| prompt arg | `SystemPrompt + "\n\n" + prompt` when brief non-empty; else `prompt` | Replaces unusable `-s` |
| `-t` | **Not used (v1)** | Daemon timeout only |
| `-s` | **Never** | Confirmed unsupported / unusable |

Also filter `CustomArgs` / `ExtraArgs` so users cannot override `--json`, `--auto-approve`, `-c`, `--id` in ways that break the protocol.

### 5.2 Environment (Daemon already injects)

Backend must use `buildEnv(cfg.Env)` so the child keeps:

- `MULTICA_TOKEN`, `MULTICA_SERVER_URL`, `MULTICA_WORKSPACE_ID`
- `MULTICA_AGENT_ID` / `MULTICA_TASK_ID` / …
- `PATH` with `multica` binary first

Do **not** call Multica HTTP from the Backend.

---

## 6. NDJSON → Multica mapping (形态 B)

Authoritative shapes follow open-source Cline 3.x + [`docs/cline-ndjson-probe.md`](./cline-ndjson-probe.md).

### 6.1 Top-level stdout types

| Line `type` | Multica handling |
| --- | --- |
| `agent_event` | Nested `.event.type` drives text / usage / done |
| `hook_event` | Lifecycle; `tool_call` / `tool_result` → tool messages |
| `run_result` | **Authoritative final line** for `Result` |
| other / invalid JSON | Skip + log; do not abort the scan |

### 6.2 Event → `Message` / `Result`

| Source | Multica |
| --- | --- |
| `agent_event` with text (`event.text` / content blocks) | `Message{Type: text, Content}` |
| `hook_event` + `tool_call` | `Message{Type: tool-use, Tool, CallID, Input}` — if name missing, `Tool="tool"` |
| `hook_event` + `tool_result` | `Message{Type: tool-result, CallID, Output}` |
| `agent_event.event.type == "usage"` | Accumulate `TokenUsage` |
| Session id if present on any event / `run_result` | Pin early via `Message{Type: status, SessionID}`; return on `Result` |
| **Last** `run_result` with `finishReason == "completed"` | `Result{Status: "completed", Output: text, Usage, SessionID}` |
| Last `run_result` with other `finishReason` (`aborted`, `error`, …) | `failed` (or `timeout` if stderr indicates timeout) |
| No `run_result` | Fallback: last `done` + process exit code |
| stderr JSON / text matching timeout | Prefer `Result.Status = "timeout"` |

### 6.3 Parse policy (v1)

1. Line-scan stdout; large scanner buffer (same order as other backends).  
2. Prefer **last** `run_result` over mid-stream `done` for status and final `Output`.  
3. Empty `Output` with `completed` is valid (work may be only `multica` side effects).  
4. Tool name/path often **absent** in Cline 3.x hooks — do not fail; generic tool label is enough.  
5. No fancy partial-dedup in v1; streaming text + correct terminal state is enough.  
6. Resume: pass `--id` when `ResumeSessionID` set; if resume fails with empty SessionID, Daemon’s existing fresh-session retry applies.

### 6.4 Illustrative stream (reference only)

```jsonl
{"type":"hook_event","event":{"type":"agent_start"}}
{"type":"agent_event","event":{"type":"iteration_start","iteration":1}}
{"type":"agent_event","event":{"type":"content_end","contentType":"text","text":"Working..."}}
{"type":"hook_event","event":{"type":"tool_call"}}
{"type":"hook_event","event":{"type":"tool_result"}}
{"type":"agent_event","event":{"type":"usage","inputTokens":100,"outputTokens":20}}
{"type":"agent_event","event":{"type":"done","reason":"completed","text":"Summary","iterations":1}}
{"type":"run_result","finishReason":"completed","text":"Summary","durationMs":1234,"usage":{"inputTokens":100,"outputTokens":20}}
```

---

## 7. Context injection without `-s`

| Mechanism | Role |
| --- | --- |
| `providerNeedsInlineSystemPrompt("cline") == true` | Daemon sets `ExecOptions.SystemPrompt = runtimeBrief` |
| Backend prepends brief to prompt arg | **Required** — primary path |
| Write `AGENTS.md` via `InjectRuntimeConfig` | Best-effort secondary; not relied on |
| Skills dir | v1: default `.agent_context/skills/` until a native Cline project skill path is confirmed |

---

## 8. Implementation checklist (when coding starts)

### 8.1 Required for a working path

| Area | Change |
| --- | --- |
| Backend | `server/pkg/agent/cline.go` + unit tests with NDJSON fixtures |
| Factory | `SupportedTypes`, `New("cline")`, `launchHeaders` |
| Lockstep test | `agent_supported_types_test.go` whitelist |
| Daemon probe | `MULTICA_CLINE_PATH` / default binary `cline` in `config.go` |
| Inline brief | `providerNeedsInlineSystemPrompt` includes `cline` |
| Brief file | `runtimeConfigPath` → `AGENTS.md` for `cline` |
| DB | migration widen `runtime_profile.protocol_family` CHECK with `cline` |
| Display name | optional override e.g. `Cline` in `runtimeDisplayNameOverrides` |

### 8.2 Deferred (do not block v1)

- Fancy provider logo / onboarding marketing copy  
- Dynamic `ListModels` discovery (empty catalog + manual `-m` is OK)  
- CLI `-t` passthrough  
- Native Cline skill directory layout (if later confirmed)  
- Docs row in `CLI_AND_DAEMON.md` (nice-to-have)

### 8.3 Explicit non-goals (v1)

- ACP host  
- File-level timeline from stream (use git outside if needed)  
- Falling back to daemon PAT as agent token  
- Opening PRs to upstream unless explicitly requested  

---

## 9. Verification plan

1. **Unit:** fixture NDJSON (success, tool hooks, aborted, missing `run_result`, bad lines) → assert `Message` sequence and `Result`.  
2. **Local daemon:** `MULTICA_CLINE_PATH=... multica daemon start --foreground` with a disposable workspace.  
3. **Happy path:** assign a read-only / low-risk issue → claim → stream → complete.  
4. **Resume (if session id appears):** second task with `--id` / `prior_session_id`.  
5. **Custom profile:** same Backend with non-default `command_name`.  
6. **Regression:** `go test ./server/pkg/agent/ …` and existing SupportedTypes lockstep tests.

---

## 10. Working branch policy (this effort)

- Develop and push only on the **fork** remote (`origin` = personal fork).  
- Do **not** open a PR against upstream unless the owner explicitly asks.  
- Design docs for this adapter live under `docs/` on the feature branch.

---

## 11. Decision log

| Date | Decision |
| --- | --- |
| 2026-07-14 | Use NDJSON, not ACP/JSON-RPC |
| 2026-07-14 | Confirmed wire format = Cline 3.x 形态 B |
| 2026-07-14 | Provider key `cline`; internal binary via Custom Runtime Profile |
| 2026-07-14 | No `-s`; prepend system/brief into prompt |
| 2026-07-14 | v1: no CLI `-t`; Multica daemon timeout only |
| 2026-07-14 | Simplest implementation preferred over feature-complete UI |

---

## 12. Next step after this doc

Implement §8.1 on the fork branch, using §6 mapping and §5 launch contract as the source of truth. Update this file’s **Status** line when implementation lands or decisions change.
