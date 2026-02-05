# Exec Approval Protocol

Human-in-the-loop command execution approval for the `exec` tool. When an agent attempts to run a shell command that doesn't pass safety checks, the Hub requests approval from the connected client before proceeding.

## Architecture Overview

```
Agent (exec tool)          Hub                    Gateway          Client (UI)
    |                       |                       |                  |
    |-- onApprovalNeeded -->|                       |                  |
    |                       |-- evaluateCommandSafety()                |
    |                       |-- requiresApproval()?                    |
    |                       |                       |                  |
    |                       |== exec-approval-request =============>  |
    |                       |                       |                  |-- show UI
    |                       |                       |                  |-- user decides
    |                       |  <== resolveExecApproval RPC ==========|
    |                       |                       |                  |
    |  <-- approved/denied -|                       |                  |
    |                       |                       |                  |
```

1. The **Agent** calls the `exec` tool with a shell command.
2. The `exec` tool invokes the `onApprovalNeeded` callback (injected by the Hub).
3. The **Hub** evaluates the command through a 4-layer safety engine.
4. If approval is needed, the Hub sends an `exec-approval-request` message to the Client via the Gateway.
5. The **Client** displays the approval UI and the user makes a decision.
6. The Client calls the `resolveExecApproval` RPC with the decision.
7. The Hub resolves the pending promise and the command is either executed or denied.

## Safety Evaluation

Before requesting approval, the Hub evaluates the command through 4 layers:

| Layer | Description | Example |
|-------|-------------|---------|
| **Allowlist** | Glob patterns of pre-approved commands | `git **`, `pnpm **` |
| **Shell syntax** | Detects dangerous shell constructs | `\|&`, `` ` ` ``, `$()`, `;` |
| **Safe binaries** | ~40 known-safe commands (no file-path args) | `ls`, `cat`, `git status` |
| **Dangerous patterns** | 25+ regex patterns for risky commands | `rm -rf`, `sudo`, `curl \| sh` |

The result is a risk level: `"safe"`, `"needs-review"`, or `"dangerous"`.

### Configuration

Stored in profile config (`~/.super-multica/agent-profiles/{profileId}/config.json`):

```json
{
  "execApproval": {
    "security": "allowlist",
    "ask": "on-miss",
    "timeoutMs": 60000,
    "askFallback": "deny",
    "allowlist": [
      { "pattern": "git **" },
      { "pattern": "pnpm **" }
    ]
  }
}
```

| Field | Values | Default | Description |
|-------|--------|---------|-------------|
| `security` | `"deny"` \| `"allowlist"` \| `"full"` | `"allowlist"` | `deny` blocks all exec, `full` allows all, `allowlist` requires matching |
| `ask` | `"off"` \| `"on-miss"` \| `"always"` | `"on-miss"` | `off` never asks, `on-miss` asks when allowlist misses, `always` always asks |
| `timeoutMs` | number (ms) | `60000` | Time before auto-deny |
| `askFallback` | `"deny"` \| `"allowlist"` \| `"full"` | `"deny"` | What happens on timeout |
| `allowlist` | array of entries | `[]` | Pre-approved command patterns |

## WebSocket Protocol

### Step 1: Approval Request (Hub → Client)

When a command requires approval, the Hub sends a push message with action `exec-approval-request`:

```json
{
  "id": "019444a0-0000-7000-8000-000000000001",
  "from": "<hubDeviceId>",
  "to": "<clientDeviceId>",
  "action": "exec-approval-request",
  "payload": {
    "approvalId": "019444a0-1234-7abc-8000-abcdef123456",
    "agentId": "019444a0-5678-7def-8000-123456abcdef",
    "command": "rm -rf /tmp/test-data",
    "cwd": "/Users/alice/projects/my-app",
    "riskLevel": "dangerous",
    "riskReasons": [
      "Matches dangerous pattern: rm with -r or -f flags",
      "Uses recursive/force deletion flags"
    ],
    "expiresAtMs": 1738700060000
  }
}
```

#### Payload Fields

| Field | Type | Description |
|-------|------|-------------|
| `approvalId` | `string` | Unique ID for this approval request (UUIDv7). Must be included in the response. |
| `agentId` | `string` | Session ID of the agent that initiated the command. |
| `command` | `string` | The shell command to be executed. |
| `cwd` | `string?` | Working directory for the command. Optional. |
| `riskLevel` | `"safe" \| "needs-review" \| "dangerous"` | Evaluated risk level. |
| `riskReasons` | `string[]` | Human-readable reasons for the risk assessment. |
| `expiresAtMs` | `number` | Unix timestamp (ms) when this request expires. After this time, the Hub auto-resolves based on `askFallback`. |

### Step 2: User Decision (Client → Hub)

The client sends a standard RPC request with method `resolveExecApproval`:

```json
{
  "id": "019444a0-0000-7000-8000-000000000002",
  "from": "<clientDeviceId>",
  "to": "<hubDeviceId>",
  "action": "request",
  "payload": {
    "requestId": "client-req-001",
    "method": "resolveExecApproval",
    "params": {
      "approvalId": "019444a0-1234-7abc-8000-abcdef123456",
      "decision": "allow-once"
    }
  }
}
```

#### Decision Values

| Decision | Effect |
|----------|--------|
| `"allow-once"` | Allow this command to execute. No persistent change. |
| `"allow-always"` | Allow and add the command's binary to the profile allowlist (e.g., `rm **`). Future commands from the same binary will auto-approve. |
| `"deny"` | Block the command. The agent receives a denial message. |

### Step 3: RPC Response (Hub → Client)

**Success** — the approval was found and resolved:

```json
{
  "id": "019444a0-0000-7000-8000-000000000003",
  "from": "<hubDeviceId>",
  "to": "<clientDeviceId>",
  "action": "response",
  "payload": {
    "requestId": "client-req-001",
    "ok": true,
    "payload": {
      "ok": true
    }
  }
}
```

**Error** — the approval was not found (already resolved or expired):

```json
{
  "id": "019444a0-0000-7000-8000-000000000004",
  "from": "<hubDeviceId>",
  "to": "<clientDeviceId>",
  "action": "response",
  "payload": {
    "requestId": "client-req-001",
    "ok": false,
    "error": {
      "code": "NOT_FOUND",
      "message": "Approval request not found or already resolved"
    }
  }
}
```

## Timeout Behavior

If the client does not respond within `timeoutMs` (default: 60 seconds), the Hub resolves the approval automatically based on the `askFallback` configuration:

| `askFallback` | Behavior on timeout |
|---------------|---------------------|
| `"deny"` (default) | Command is denied (fail-closed). |
| `"full"` | Command is allowed. |
| `"allowlist"` | Command is allowed only if it matched the allowlist; otherwise denied. |

## SDK Types

All protocol types are exported from `@multica/sdk`:

```ts
import {
  ExecApprovalRequestAction,    // "exec-approval-request"
  type ApprovalDecision,         // "allow-once" | "allow-always" | "deny"
  type ExecApprovalRequestPayload,
  type ResolveExecApprovalParams,
  type ResolveExecApprovalResult,
} from "@multica/sdk";
```

## Client Implementation Guide

A minimal client handling exec approvals:

```ts
import { GatewayClient, ExecApprovalRequestAction } from "@multica/sdk";
import type { ExecApprovalRequestPayload, ApprovalDecision } from "@multica/sdk";

// Listen for approval requests
client.onMessage((msg) => {
  if (msg.action === ExecApprovalRequestAction) {
    const payload = msg.payload as ExecApprovalRequestPayload;
    showApprovalUI(payload);
  }
});

// When user makes a decision
async function respondToApproval(approvalId: string, decision: ApprovalDecision) {
  const result = await client.request(hubDeviceId, "resolveExecApproval", {
    approvalId,
    decision,
  });
  // result.ok === true if resolved successfully
}
```

## Error Handling

The system is designed to be **fail-closed**:

- If sending the approval request to the client fails → command is denied.
- If the client disconnects before responding → timeout fires, command follows `askFallback` (default: deny).
- If the RPC response references an unknown `approvalId` → `NOT_FOUND` error returned, no side effects.
- If the agent is closed while an approval is pending → all pending approvals for that agent are auto-denied.
