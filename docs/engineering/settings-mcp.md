# Settings through the built-in MCP

The local Codex and Claude Code providers automatically receive the existing
`multica_desktop_context` MCP. Settings operations extend that server; they do
not install a new global MCP or enable the user's inherited external plugins.

## Contract and execution

`packages/core/settings/operations.ts` is the bounded operation registry. It
defines tool names, input schemas, permission hints and requests against existing business APIs. The desktop MCP and the Settings
renderer bridge both validate against this contract. The CLI calls the same
execution-local MCP instead of implementing another set of REST mutations.

There are 53 Settings operations plus `settings_capabilities`. Use the live
catalog (`multica settings tools`) for their current schemas:

| Domain | Operations |
| --- | --- |
| Workspace and members | Read/update workspace details; list members/invitations; invite, change roles, remove, revoke invitations |
| Workflows | List/read/create/edit/delete; edit a column's handoff; preview a draft brief; preview/apply a project's workflow |
| Statuses | List/create/edit/archive; atomic category reordering |
| Wakeups | Read/update workspace system-wakeup defaults |
| Labels | List/create/edit/delete issue, agent and Skill label definitions |
| Properties | List/create/edit/archive/restore definitions and select options; types remain immutable |
| Quick actions | List/create/edit/archive/delete definitions; creating does not execute an agent |
| MCP library | Safe metadata; create a public HTTP endpoint; rename/delete entries. No implicit agent assignment |
| Code and channels | Read supported connection metadata; disconnect; merge code-setting flags without replacing unrelated settings |
| Account | Read/update profile, language/timezone; patch notification groups; token metadata and token creation/revocation |
| Device | Read/update theme, composer, issue-opening, floating-chat and create-field preferences; keyboard shortcuts; daemon/update preferences; bounded daemon and updater actions |
| Interactive setup | `settings_open` opens the relevant Settings tab for OAuth, credential entry, stdio MCP configuration and other user interaction |

Opening a tab returns `opened`, not `applied`. OAuth and credential-based
connections continue through their existing UI flows. Connection metadata is
not a successful connectivity probe. The tools do not manufacture a probe API
where the server does not provide one.

Device reads do not probe installed agent CLIs unless `probe_runtimes=true` is
explicitly requested. Daemon logs can be opened through the existing local UI;
their contents are not returned to the model. Externally managed daemons reject
local lifecycle and automatic-start/stop preference changes.

`settings_capabilities` identifies the execution's account/workspace, cloud
availability and the current membership role. Its confirmation fields are false. Role hints do not grant
permission: the existing backend checks resource ownership and feature gates on
every call. A workflow creator may edit their own workflow; Default and project
adoption retain their existing special authorization rules.

## Workflow changes

1. Read the workflow and current status keys.
2. Preview the requested edit with `settings_preview_workflow`, preserving
   `status_mapping` and the server's affected-task plan.
3. Apply with `settings_update_workflow`. The adapter re-runs the exact dry run;
   the requested change executes directly even when existing tasks are affected.
4. Project workflow changes default to `dry_run=true`. Applying uses
   `dry_run=false`, re-previews and executes without a confirmation dialog.
5. Preserve structured `workflow_status_mapping_required`, `workflow_in_use`,
   permission and feature-disabled errors. A failed request is not success.

Status reordering uses the atomic `/api/issue-statuses/reorder` endpoint.
Property option updates keep existing IDs. Built-in status keys/categories and
property types cannot be changed through the Settings schemas.

## Scope, direct execution and credentials

- Cloud requests use the verified desktop account and frozen workspace header.
  Tool arguments cannot select an arbitrary URL, workspace or HTTP operation.
- Desktopd MCP calls do not require a separate user approval. API writes run
  in the daemon with its scoped requester; device operations go directly to the
  host. PC and mobile clients use the same daemon policy. Codex's built-in MCP
  approval mode and Claude's allowed-tools list already allow these calls.
- The renderer validates the originating account and workspace before local
  settings actions. Replies from other windows/frames are rejected; aborted
  executions cancel pending host requests. Cancellation is checked again after
  asynchronous preflight and before local writes. An operation already submitted
  to the server or device is not rolled back by cancellation.
- Existing `settings.confirm` host messages execute directly after scope
  validation, without Cancel / Apply. The wire name and response stay compatible
  with installed daemons; new daemons use it only for secure token creation.
  Deploy the updated Desktop host as well as desktopd to remove old-host dialogs.
- The policy is unconditional for new and resumed executions: no stored setting,
  migration or opt-in switch is required. Previously completed/cancelled receipts
  are unchanged; pending or unknown outcomes are never automatically replayed.
- Token creation is only registered when the secure display callback exists.
  Creation runs before the native token display, without a prior approval. The
  display is informational and retains its Copy / Done actions. The new secret
  appears in the native UI, never in MCP replies, CLI output or
  operation receipts. Account changes prevent display to a different user.
- MCP configurations, headers, environment values, webhook secrets and
  connection credentials are not returned to the model. Property `config`
  remains readable because its select options are business data.
- Reads continue using server authorization. A server's human-only or
  machine-credential rejection is preserved, not bypassed with another profile.

Successful writes use existing backend events and additionally ask the active
renderer to refresh its query cache/profile. If that refresh is unavailable,
the result remains `applied` with `ui_refresh=pending`; it is not reported as a
failed server mutation.

## Retry receipts and concurrency boundary

Each mutating tool requires `request_id`. Reuse it with identical arguments for
a retry. The per-account/workspace/operation journal lives beside `cli-context`
in `settings-operation-receipts`, so normal execution cleanup does not erase it.
It records a fingerprint and pending state before attempting a write, then an
applied/cancelled result or a definite HTTP rejection. Unknown network outcomes
remain pending and cannot be blindly replayed after a restart. Receipts contain
operation/scope metadata and sanitized results, not input credentials.

These are durable **desktop-local** receipts. The existing business APIs do not
provide a universal cross-device idempotency or compare-and-swap contract. The
adapter does not claim distributed exactly-once execution or atomicity across
multiple settings operations. Inspect current settings after an unknown result;
do not retry under a new key merely to evade the pending receipt. A future
server contract is required for stronger cross-device guarantees.

## CLI

Inside an active desktop execution:

```sh
multica settings capabilities
multica settings tools
multica settings call settings_list_workflows
multica settings call settings_create_workflow --input-file ./workflow.json
```

Example `workflow.json`:

```json
{"name":"Delivery","request_id":"create-delivery-1"}
```

Use `--input-file -` for stdin. The adapter accepts only `settings_` tools and
the execution-provided `127.0.0.1` MCP endpoint/token. When a provider filters
those environment variables, the exact scoped CLI prefix in the system prompt
supplies `MULTICA_TASK_CONFIG_ROOT`; only that execution's private `mcp.json`
is eligible as a registration source. Partial environment credentials are not
mixed with file credentials. The adapter disables proxies, refuses redirects
and never falls back to an ambient CLI login. Outside an active desktop
execution it reports that the scoped MCP is unavailable.

## Verification

```sh
pnpm --filter @multica/desktop exec vitest run src/main/desktop-settings-host.test.ts src/main/settings-confirm.test.ts src/main/local-v2-host.test.ts src/renderer/src/platform/settings-host-bridge.test.ts
pnpm --filter @multica/desktop typecheck
cd server
go test -race ./internal/localconversation/settings ./internal/localconversation/service ./internal/localconversation/contextmcp
go test ./cmd/multica -run '^TestSettingsAdapter' -count=1
```

The Settings regression tests use fake business APIs, host callbacks and receipts.
They cover calls without a confirmation host, workflow changes affecting zero or
multiple tasks, direct device actions, permission errors, retries, uncertain
outcomes and token secret handling. Desktop host tests cover existing daemon
requests, scope changes, cancellation and native secret display. They do not run
user-installed agent CLIs or real models. Native PC/mobile UI smoke checks remain
separate from these shared daemon policy tests.
