# Decision: Agent access scope is a column, a filter, and a bulk action

Status: implemented

## Problem

Nothing on the agents list page said who could invoke an agent. The list showed agent, status, owner, runtime, last active, runs, model, and created. The only access signal was a small lock marker derived from `visibility`, and `visibility` is a lossy two-state projection of the authoritative `permission_mode` plus `invocation_targets`: an agent shared with three named people maps to `visibility: "private"`, indistinguishable from one nobody else can run.

The cost was paid outside the product. Inventorying which agents were private versus workspace-invocable required raw SQL, because no column or filter answered it. Changing scope across many agents required a bulk `UPDATE` plus `INSERT` — observed at the scale of 70 agents across 5 workspaces — because the UI edits one agent at a time.

## Decision

The list page owns the whole see → filter → change loop for access scope, computed entirely from fields already in the list payload.

**Three-state effective access**, derived from `permission_mode` and `invocation_targets` rather than from `visibility`. Workspace is `public_to` with a workspace target; Specific people is `public_to` scoped to member or team targets, or to none; Owner-only is `private`. This mirrors the authoritative gate in `canInvokeAgent`. The column is visible by default, and the horizontal room comes from accepting scroll rather than from hiding another column.

**A matching `access` filter dimension**, so a class of agents can be isolated in one click whether or not the column is showing.

**A "Set access scope" bulk action** on the existing batch toolbar. It offers all three scopes by reusing `AccessPicker` through a thin `BulkAccessPicker` wrapper, which passes a new optional `hideFooter` prop so the picker's own Save button does not compete with the dialog's confirm. A confirmation dialog states the target scope, the owned count, and the skipped count before anything is written.

The bulk action is gated on `isOwnedByMe`, not on the broader manage permission. The backend enforces owner-only writes for `permission_mode` and `invocation_targets`, so including workspace admins would hand them 403s partway through a sequential batch.

The derivation is a pure function in `packages/core/agents/` shared by the column and the filter, computed at the cell and the predicate with `useMemo` and never mirrored into Zustand. It fails safe: an absent `permission_mode` — possible from a stale cache or an older self-hosted backend — reads as Owner-only, and a `public_to` agent with absent targets reads as Specific people.

All agents are treated uniformly; there is no built-in-agent special case.

## Alternatives considered

**Show the raw `visibility` field.** Rejected. It would label an agent shared with specific people as "private", telling the operator the opposite of who can run it — which is precisely the confusion that sent people to SQL.

**Add a backend endpoint or schema change.** Unnecessary. `ListAgents` selects everything, so it carries `permission_mode`, and the handler already attaches `invocation_targets` per agent. Effective access is a pure function over data the page already has.

**Hide the column behind the column toggle by default.** Rejected. At-a-glance access information was judged worth more than horizontal density, and the list already scrolls horizontally.

**Restrict bulk to a two-tier choice, excluding Specific people.** Considered and overridden. Bulk offers all three, with the chosen member targets applying uniformly to every selected agent. A wrong choice is reversible by running bulk edit again, and the flexibility was worth more than the guardrail.

**Inline per-row access editing via a popover.** Rejected. The per-agent inspector and the new bulk action already cover the same ground.

## Consequences

Access-scope questions and changes are answerable in the product, which was the point.

`AccessPicker` still derives scope with its own inline logic, duplicating the shared helper. Consolidating them is a clean follow-up that was left out to keep the change reviewable; only the `hideFooter` opt-in prop was added to the picker.

A workspace-level default access scope for newly created agents would stop the cleanup backlog from recurring, but it belongs to the settings surface rather than the list page.

The "automation cannot trigger this agent" affordance for the Owner-only state is a separate upstream bug with its own fix path.
