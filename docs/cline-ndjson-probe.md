# Cline 3.x NDJSON Probe Guide (形态 B)

Probe and capture notes for adapting a **Cline-based internal coding CLI** to Multica via **`--json` NDJSON** (not ACP / JSON-RPC).

**Confirmed shape:** Format **B** (Cline 3.x envelope), not the older Overview `say`/`ask` message form.

Related Multica control plane:

- Daemon spawns agent CLI: `server/internal/daemon/daemon.go` (`runTask`)
- Unified backend contract: `server/pkg/agent/agent.go` (`Backend.Execute` → `Message` / `Result`)
- NDJSON-style references in-tree: `server/pkg/agent/cursor.go`, `opencode.go`, `pi.go`

Upstream references:

- [Cline CLI Reference](https://docs.cline.bot/cli/cli-reference)
- [Cline CLI Overview](https://docs.cline.bot/usage/cli-overview)
- [SDK Events](https://docs.cline.bot/sdk/reference/events)
- Official sample filter: `select(.type == "agent_event" and .event.type == "done")`

---

## 1. Why NDJSON (形态 B)

| Path | Use for Multica? |
| --- | --- |
| `cline --json` → NDJSON on stdout | **Yes** — headless one-shot, matches Daemon spawn/drain model |
| `cline --acp` → ACP JSON-RPC | No — requires bidirectional host; internal CLI has no ACP SDK |

Multica does not call the LLM. The daemon:

1. Prepares workdir / env (`MULTICA_TOKEN`, …)
2. Spawns `your-cli --json … "<prompt>"`
3. Parses stdout lines into `Message`
4. Maps final `run_result` (or fallback) into `Result`

---

## 2. Launch recipe (open-source Cline 3.x)

Replace `your-cli` with the internal binary name.

```bash
# Minimal headless NDJSON
your-cli --json --auto-approve true \
  -c /path/to/workdir \
  -t 120 \
  "Your prompt here"

# Optional Multica-aligned knobs (if supported by the fork)
your-cli --json --auto-approve true \
  -c "$WORKDIR" \
  -t 600 \
  -s "$SYSTEM_OR_BRIEF" \
  --data-dir "$PER_TASK_STATE_DIR" \
  --id "$PRIOR_SESSION_ID" \
  -m "$MODEL" \
  -P "$PROVIDER" \
  "$PROMPT"
```

Useful flags (open-source `cline --help`):

| Flag | Role |
| --- | --- |
| `--json` | NDJSON instead of TUI |
| `--auto-approve true\|false` | Tool auto-approval (often default `true`) |
| `-c, --cwd` | Working directory (= Multica task workdir) |
| `-t, --timeout` | Seconds (`0` = no timeout) |
| `-s, --system` | System prompt override |
| `--id` | Resume session |
| `--data-dir` | Isolated local state (avoid global `~/.cline` crosstalk) |
| `-m` / `-P` / `-k` | Model / provider / API key |
| `--thinking` | Reasoning effort |
| `--acp` | **Do not use** for Multica NDJSON path |

---

## 3. 形态 B schema (Cline 3.x)

Each **stdout** line is one JSON object with a top-level `type`.

### 3.1 Top-level types

| `type` | Meaning | Multica use |
| --- | --- | --- |
| `hook_event` | Lifecycle: `agent_start`, `agent_end`, `tool_call`, `tool_result` | Tool timeline (name/path often **missing**) |
| `agent_event` | Streaming content via nested `.event` | Text / usage / mid-run done |
| `run_result` | **Authoritative final line** | `Result.Status`, `Output`, `Usage` |

### 3.2 `agent_event.event.type` (SDK)

| `event.type` | Meaning |
| --- | --- |
| `content_start` / `content_update` / `content_end` | Text / reasoning / tool content block |
| `iteration_start` / `iteration_end` | Agent loop turn |
| `usage` | Token / cost update |
| `notice` | Status / recovery notice |
| `done` | Agent finished this run |
| `error` | Error |

`done.reason` (SDK): `completed | max_iterations | aborted | mistake_limit | error`

### 3.3 Illustrative stream (synthetic; verify on your binary)

```jsonl
{"type":"hook_event","event":{"type":"agent_start"},"ts":1710000000000}
{"type":"agent_event","event":{"type":"iteration_start","iteration":1}}
{"type":"agent_event","event":{"type":"content_start","contentType":"text"}}
{"type":"agent_event","event":{"type":"content_end","contentType":"text","text":"I'll inspect the repo first."}}
{"type":"hook_event","event":{"type":"tool_call"}}
{"type":"hook_event","event":{"type":"tool_result"}}
{"type":"agent_event","event":{"type":"usage","inputTokens":1200,"outputTokens":340,"totalCost":0.012}}
{"type":"agent_event","event":{"type":"iteration_end","iteration":1}}
{"type":"agent_event","event":{"type":"done","reason":"completed","text":"Done. Summary...","iterations":1}}
{"type":"hook_event","event":{"type":"agent_end"}}
{"type":"run_result","finishReason":"completed","text":"Done. Summary...","durationMs":12345,"iterations":2,"model":{"id":"xxx","provider":"yyy"},"usage":{"inputTokens":1200,"outputTokens":340,"totalCost":0.012}}
```

### 3.4 `run_result` fields to capture

| Field | Notes |
| --- | --- |
| `finishReason` | `"completed"` = success; timeout often ends as `"aborted"` |
| `text` | Final summary → Multica `Result.Output` |
| `durationMs` | Wall time |
| `iterations` | Loop count |
| `model.id` / `model.provider` | Model identity |
| `usage` / `aggregateUsage` | `inputTokens`, `outputTokens`, `totalCost`, cache fields if present |

### 3.5 Timeout shape (observed on open-source ~3.0.37)

- **stderr** (often one JSON line):

  ```json
  {"ts":"...","type":"error","message":"run timed out after 600s"}
  ```

- **stdout** last `run_result`: `finishReason` frequently `"aborted"` (not `"timeout"`); usage may be zeros.
- Multica mapping: classify as `timeout` / `blocked` using stderr + `finishReason`, not stdout text alone.

### 3.6 Known gaps

- `hook_event` tool markers may **omit tool name and file paths** — do not rely on the stream for file-level diffs; use git if needed.
- Exit code and `finishReason` can disagree. Prefer **last `run_result`** as truth; use exit code as fallback.
- Tolerate malformed lines (skip + log).

---

## 4. Prompt examples for probing

Use these as the CLI prompt argument. Prefer a clean temp directory and short timeouts.

### 4.1 Minimal (schema only, no tools / no writes)

```text
Reply with exactly one line: pong.
Do not create, edit, or delete any files.
Do not run shell commands.
```

```bash
your-cli --json --auto-approve true -c "$(pwd)" -t 60 \
  "Reply with exactly one line: pong. Do not create, edit, or delete any files. Do not run shell commands." \
  > /tmp/cli-probe/stdout.ndjson 2> /tmp/cli-probe/stderr.txt
```

**Expect:** `agent_event` text/done + final `run_result` with `finishReason=completed` and text containing `pong`.

### 4.2 Single tool call

```text
Run exactly one shell command: echo hello-from-tool
Then reply with the command output only.
Do not modify any files.
```

**Expect:** `hook_event` `tool_call` / `tool_result` (possibly without tool name) + assistant text with `hello-from-tool`.

### 4.3 Read-only listing

```text
List the files in the current directory using your tools if available.
Summarize filenames in at most 5 bullet points.
Do not modify any files.
```

### 4.4 Multica CLI smoke (only when `multica` is on PATH)

```text
You are a Multica coding agent.
1) Run: multica --help
2) Summarize available top-level commands in 5 bullets.
3) Do not create issues or comments.
4) Do not modify repository files.
```

### 4.5 Forced timeout (failure path)

```text
Work continuously without finishing. Keep exploring until stopped.
Do not modify any files.
```

```bash
your-cli --json --auto-approve true -c "$(pwd)" -t 3 \
  "Work continuously without finishing. Keep exploring until stopped. Do not modify any files." \
  > /tmp/cli-probe/timeout.out 2> /tmp/cli-probe/timeout.err
```

**Expect:** non-zero or aborted finish; stderr may carry `timed out`; stdout `run_result.finishReason` may be `aborted`.

### 4.6 Multica-shaped assignment (integration later)

```text
You are running as a local coding agent for a Multica workspace.

Your assigned issue ID is: ISSUE_UUID_HERE

Start by running `multica issue get ISSUE_UUID_HERE --output json` to understand your task, then complete it.
For comments, use `multica issue comment list ISSUE_UUID_HERE --recent 10 --output json`.
When replying, write UTF-8 content to ./reply.md and post with:
`multica issue comment add ISSUE_UUID_HERE --content-file ./reply.md`
```

(Replace `ISSUE_UUID_HERE`. Real Multica prompts are built in `server/internal/daemon/prompt.go`.)

---

## 5. Capture & analyze commands

```bash
INTERNAL_CLI=your-cli   # change me
mkdir -p /tmp/cli-probe && cd /tmp/cli-probe

$INTERNAL_CLI --help 2>&1 | tee /tmp/cli-probe/help.txt

$INTERNAL_CLI --json --auto-approve true \
  -c "$(pwd)" -t 120 \
  "Reply with exactly one line: pong. Do not create, edit, or delete any files. Do not run shell commands." \
  > /tmp/cli-probe/stdout.ndjson \
  2> /tmp/cli-probe/stderr.txt
echo "exit=$?"
```

### 5.1 Top-level `type` histogram

```bash
jq -r 'if type=="object" then (.type // "<no-type>") else "<non-object>" end' \
  /tmp/cli-probe/stdout.ndjson | sort | uniq -c | sort -rn
```

Expected (形态 B):

```text
  N agent_event
  M hook_event
  1 run_result
```

### 5.2 Nested event types

```bash
jq -r 'select(.type=="agent_event") | .event.type // "<missing>"' \
  /tmp/cli-probe/stdout.ndjson | sort | uniq -c | sort -rn

jq -c 'select(.type=="agent_event" and .event.type=="done")' \
  /tmp/cli-probe/stdout.ndjson

jq -c 'select(.type=="run_result")' /tmp/cli-probe/stdout.ndjson | tail -n 1
```

### 5.3 Keys per type (for Backend field map)

```bash
jq -s '
  group_by(.type)
  | map({type: (.[0].type // "null"),
         n: length,
         keys: (map(keys) | add | unique)})
' /tmp/cli-probe/stdout.ndjson
```

### 5.4 Extract final answer (official-sample style)

```bash
jq -r 'select(.type == "agent_event" and .event.type == "done") | .event.text' \
  /tmp/cli-probe/stdout.ndjson | sed 's/\\n/\n/g'

# Prefer authoritative run_result when present
jq -r 'select(.type=="run_result") | .text' /tmp/cli-probe/stdout.ndjson | tail -n 1
```

---

## 6. Field map checklist → Multica

Fill after probing the internal binary.

| Multica need | Internal NDJSON source (fill in) | Open-source default guess |
| --- | --- | --- |
| Streaming assistant text | | `agent_event.event.text` / content_* |
| Tool start | | `hook_event` + `tool_call` |
| Tool end | | `hook_event` + `tool_result` |
| Tool name | | often **absent** → use `"tool"` |
| Session id | | `run_result.sessionId` / CLI `--id` |
| Success | | last `run_result.finishReason == "completed"` |
| Failure / abort | | `aborted` / `error` / non-zero exit |
| Final output | | `run_result.text` or `done.text` |
| Usage | | `run_result.usage` / mid-stream `usage` events |
| Model | | `run_result.model.id` |
| Timeout | | stderr `timed out` + `finishReason=aborted` |

### Suggested parse policy

1. Line-scan stdout; skip invalid JSON with a log line.
2. `agent_event` with text → `MessageText` (dedupe partials if needed).
3. `hook_event` tool_* → `MessageToolUse` / `MessageToolResult`.
4. First session id → `MessageStatus` + pin for resume.
5. **Last** `run_result` decides `Result`.
6. If no `run_result`: fall back to last `done` + process exit code.
7. Never call Multica HTTP from the Backend; business I/O stays `multica` CLI inside the agent env.

---

## 7. Multica Backend mapping sketch

| Internal event | `server/pkg/agent` |
| --- | --- |
| Text chunk | `Message{Type: text, Content}` |
| tool_call | `Message{Type: tool-use, Tool, CallID, Input}` |
| tool_result | `Message{Type: tool-result, CallID, Output}` |
| session id | `Message{Type: status, SessionID}` |
| `finishReason=completed` | `Result{Status: "completed", Output, Usage, SessionID}` |
| aborted / error / timeout | `Result{Status: "failed"\|"timeout", Error, SessionID}` |

Template files to copy when implementing:

- Thin stream parser: `server/pkg/agent/cursor.go` or `opencode.go`
- Factory whitelist: `server/pkg/agent/agent.go` (`SupportedTypes`, `New`)
- PATH probe: `server/internal/daemon/config.go`
- Skills / brief: `server/internal/daemon/execenv/`

---

## 8. Recommended probe order

1. §4.1 minimal → confirm 形态 B histogram  
2. §4.2 tool → confirm hook events  
3. §4.5 timeout → confirm failure classification  
4. §4.4 / §4.6 only with Multica credentials and a disposable workspace  

After capture, attach (redacted):

1. `type` histogram  
2. One sample each of `agent_event`, `hook_event`, `run_result`  
3. Relevant `--help` lines for json/cwd/timeout/id/system/data-dir  

That is enough to implement a Multica `Backend` without guessing schema.

---

## 9. What not to do

- Do not implement ACP solely for Multica if `--json` already works.  
- Do not assume Overview docs `say`/`ask` shape on 3.x forks.  
- Do not treat missing tool names as a hard failure.  
- Do not fall back to the daemon PAT as `MULTICA_TOKEN` (task-scoped token only).  
