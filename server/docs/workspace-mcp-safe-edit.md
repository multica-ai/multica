Workspace MCP safe editing
==========================

This API provides **safe prefill**, not full config reveal. It supplements the
existing collection GET/POST and item PUT/DELETE without changing their response
shapes. Apply migration 564 before deploying the handlers. Its positive BIGINT
revision starts at 1 for existing/new rows; a trigger advances every UPDATE,
including writes by an older server during a rolling deployment.

Both endpoints require a human workspace owner/admin. Both reuse
`requireWorkspaceMcpWriter`, including the agent-actor rejection under an owner's
PAT, and scope queries by workspace and server UUID. The existing inventory GET
remains member-visible. The admin frontend may enforce stricter owner-only access.

## Read

`GET /api/workspaces/{id}/mcp-servers/{serverId}/editable-config`

```json
{
  "id": "server-uuid",
  "name": "crm",
  "revision": "1",
  "policy_version": "1",
  "visible_config": {"type": "http"},
  "protected_fields": ["url", "headers"],
  "has_opaque_fields": true
}
```

Headers: `ETag: "1"`, `Cache-Control: no-store`.

`revision` is a positive decimal **string** and its quoted form is the exact ETag.
Only explicitly stored type values in `stdio`, `local`, `http`, `remote`,
`streamable-http`, `sse` are returned; there is no inference or normalization.
Only presence of the fixed fields `url`, `command`, `args`, `headers`, `env` is
reported. All their values, unknown keys, nested names, lengths and prefixes stay
private. `has_opaque_fields` signals undisclosed additional data. No masking
strings or sentinel values enter the config. Future richer reveal needs reviewed
provider schemas; heuristic detection cannot authorize disclosure.

## Change

`PATCH /api/workspaces/{id}/mcp-servers/{serverId}/config`

Require `If-Match: "1"`, copied from the read. Weak/wildcard/multiple tags and
numeric coercions are not accepted. Example:

```json
{
  "name": "crm-prod",
  "operations": [
    {"op": "set", "path": "/env/REGION", "value": "us-east-1"},
    {"op": "remove", "path": "/env/OLD_OPTION"}
  ]
}
```

`name` is optional. `operations` is required and may be empty (rename-only).
This is a custom envelope, not JSON Patch or JSON Merge Patch. Only set/remove
are accepted; values under untouched paths remain unchanged, including opaque
fields and exact numeric values. `set` replaces the supplied subtree, including
literal null; `remove` deletes an existing field and cannot carry a value.
There is no special meaning for a sentinel-shaped user object.

Paths use RFC 6901 escapes and are relative to one stored server entry. Root
replacement, invalid escapes, overlapping/duplicate paths, duplicate JSON keys,
unknown envelope properties and missing parents are rejected. Traversal is
object-only: arrays must be replaced as complete values, not indexed. All request
limits (64 KiB, JSON depth 32, 64 operations) are enforced before any pointer
traversal. Config merges and optional renames commit atomically under a row lock;
revision conflicts leave everything untouched, including agent bindings.

Success returns the same safe projection with the new revision and ETag. Errors
use generic messages: 400 invalid input, 403 unauthorized role/actor, 404 absent or
out-of-workspace ID after authorization, 409 duplicate name, 412 stale/invalid
If-Match, 428 missing If-Match. Existing authentication middleware handles 401.
No input fragments or database errors are echoed. Access and mutation logs include
user/workspace/server IDs and revision, never config bodies or arbitrary paths.
These structured events do not promise durable audit retention.

## Client behavior and rollout

Prefill only safe values. Offer Keep / Replace / Remove for protected fields,
warning that replacing an object replaces every child. Never submit a sanitized
projection via legacy PUT. Unknown fields stay server-side. Validate response
shape and ETag before enabling edits; do not automatically retry stale or uncertain
saves. Reload explicitly and reapply changes. Keep drafts out of browser storage,
analytics, error reporting and tracing. The proxy must forward the signed-in human
session and exact If-Match and use no-store responses.

Legacy PUT remains unconditional full replacement when config is supplied and
preserves config when omitted. Its writes increment revisions but remain
last-writer-wins. Deploy migration/backend before enabling the new editor; older
backends lack these endpoints. No deployment is implied by merging client code.
