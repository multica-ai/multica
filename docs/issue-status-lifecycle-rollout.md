# Four-category issue lifecycle (MUL-7240)

## Contract

| Stored category | Fixed built-in statuses | Custom status behavior |
| --- | --- | --- |
| `unstarted` | `backlog`, `todo` | Open work; no Backlog parking or promotion rule |
| `started` | `in_progress`, `in_review`, `blocked` | Open work; no review completion, blocked failure, or active-status recovery rule |
| `done` | `done` | Successful terminal lifecycle |
| `closed` | `cancelled` | Cancelled terminal lifecycle |

Done is not an intermediate step toward Closed. In Review and Awaiting Response
belong to Started. A status's label does not grant behavior. Built-ins remain
locked, including name, color, order, category, archival and deletion. Custom
keys and categories remain immutable; their labels, descriptions and colors may
be edited, and they can be reordered or archived. Existing issues retain an
archived status; new assignments to it are rejected.

The server's `Effective`/SQL `issue_effective_status` functions preserve built-in
identity, map custom Done/Closed to terminal behavior, and leave other custom
keys distinct. Lifecycle grouping uses `Category`, not the effective behavior.
Ordinary creation, assignment, comments, and explicit run requests retain their
existing trigger rules. Merely entering a custom Started status is not an
instruction to start, finish or fail an agent. The built-in platform skill and
new daemon task briefs describe this distinction. This is not a workflow engine;
PR #7990 is subsequent work, not part of this release.

## Migration and release

1. Before deploying, count catalog rows by category and active/archive state;
   identify custom Backlog, In Review and Blocked usages, especially issues in
   active autopilot runs. Tell affected users that those custom statuses will
   no longer park, finish or fail automation. The decision applies to historical
   custom statuses too; no legacy behavior is retained.
2. Apply migration 467 and deploy the matching backend. The catalog rewrite,
   constraints and SQL behavior function change in one atomic statement. It
   preserves every status ID/key/name/color/order/archive marker and every issue
   reference. It does not update issues, replay events, or enqueue tasks.
3. The approximately one-minute mixed-backend window is an accepted maintenance
   window, not a zero-downtime guarantee. Old pods may reject catalog writes in
   that window. Finish replacing them before acceptance testing.
4. Deploy the Web/Desktop/Mobile client changes and update local daemons. New
   skills/prompts take effect in newly prepared task environments; already
   running agents do not have their prompt rewritten. Do not replay or restart
   tasks automatically. Review affected in-flight work and move it to an
   appropriate fixed built-in status only when that action is intended.
5. Verify four stored categories, seven unchanged fixed keys, unchanged row and
   issue-reference counts, custom status CRUD/archive, terminal filtering, board
   grouping, and lack of unintended task enqueues. Check API error rates and
   automation completion/failure paths.

Release policy is fix-forward. Do not roll back application code over the new
schema or restore a second behavior model. If migration fails, its atomic
statement leaves the prior catalog intact: diagnose and apply the corrected
forward migration. If it committed, repair with a new forward migration or
patch. The paired down file deliberately refuses reversal; reapplying the up
statement after an uncertain acknowledgement is idempotent.

## Installed-client compatibility

Status keys on issue create/get/update/assign do not change. The existing
catalog `category` / `categories` and issue `status_category` response fields
retain the seven-value wire enum accepted by installed clients. Built-ins emit
their fixed key; custom statuses emit `todo`, `in_progress`, `done`, or
`cancelled` as a lifecycle encoding. New clients normalize those fields into
four categories. This is one API-boundary adapter, not a second stored model
or behavior mapping. Old category inputs are accepted on catalog creation,
reordering and category filters, and normalized to the new lifecycle.

This prevents enum changes from breaking ordinary reads and writes after the
backend upgrade. It does not promise identical old UI grouping or old agent
instructions: old filters can broaden when categories combine, old saved
grouping views can require refresh/upgrade, and old daemons cannot render the
new lifecycle brief reliably. Update clients/daemons for the new feature.
Validation errors (unknown/archived status, permissions, stale reorder sets)
still return their normal error responses; no API guarantees every request
will succeed, especially during the accepted mixed-version window.

## Regression coverage

- Actual 332-to-467 migration and replay against an isolated transactional
  schema; non-category field preservation and SQL tenant isolation.
- Built-in identity, custom lifecycle vs special behavior, unknown-key handling,
  archived status reads, category filtering, table/swimlane grouping.
- Old/current category inputs and legacy response encoding, including realtime
  issue payloads; frontend normalization and mobile lifecycle parity.
- Backlog-to-custom-Unstarted promotion and no custom-to-Todo re-promotion;
  catalog locks, access checks, reorder atomicity, fixed built-in protection.
- Agent brief and bundled skill assertions enforce the new semantics.
