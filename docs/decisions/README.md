# Decision records

A decision record holds the part of a change that code and reference documentation cannot carry: **why** it was done, **what it beat**, and **what it cost**. It is also the only place in this repository where implementation status is allowed to live.

This file is the contract: where records go, when to write one, and the in-file format. [`pnpm docs:check`](../../scripts/verify-docs.mjs) enforces the mechanical parts; the [documentation standard](../AGENTS.md) owns everything above this tier.

## Layout and naming

Both axes of a record are encoded in its path — `{lifecycle}/{class}/yyyy-mm-dd-topic-title.md`:

**Lifecycle** is the top-level folder, and it *is* the record's status. A record moves between folders as its status changes; nothing inside the file tracks status independently.

- **`proposed/`** — decided in principle but not built, or only partly built. This is where anything half-finished belongs. Future tense is legitimate here.
- **`implemented/`** — it shipped. The record describes what is true in the code today, in the present tense, and is corrected in the same change that moves a file, renames a symbol, or changes a default.
- **`rejected/`** — considered and declined. Keep it only while its reasoning still prevents a tempting mistake; otherwise delete it.

**Class** is the nested folder, from a closed set. The gate rejects any other folder name.

| Class | What it covers |
|---|---|
| `feature` | A new user-facing or agent-facing capability. |
| `bug-fix` | Corrects a defect, or closes a gap an incident exposed. |
| `simplification` | Removes code, behavior, or surface area without adding a capability. |
| `architecture` | A structural decision about the shipped source: how packages relate, what a data model means, where a boundary sits. |
| `process` | Tooling, policy, or workflow around the code — gates, conventions, release mechanics — not runtime behavior. |
| `testing` | Test infrastructure and strategy. |

`architecture` is about the source we ship; `process` is about the machinery around it. There is deliberately no `refactor` class: `simplification` already covers it, and its question — "does observable behavior change?" — is the one that matters.

The date in the filename is when the topic was first proposed, not when the file was written. There is no index file: the lifecycle and class folders are the inventory, and an index would be one more thing to forget to update.

## When to write one

**Every non-trivial change adds or updates at least one decision record in the same PR.** A change is non-trivial when it alters behavior, architecture, a cross-package contract, a database or wire format, process or tooling, or testing strategy — anything a maintainer might reasonably revisit and ask "why is it like this?".

Updating the record that already owns the decision satisfies the rule. Do not create a second record for the same decision. A purely mechanical or local edit — a rename, a typo, a test that pins existing behavior — is exempt.

A record is never edited into a *different* decision. Supersede it with a new record and cross-link the two. Correcting an `implemented/` record so its file paths, symbol names, and defaults match the code is required, not forbidden — that is maintenance of fact, not revision of the decision.

## The file format

The first three lines of every record are exactly:

```markdown
# Decision: <title>

Status: <status>
```

followed by a blank line. `Status:` takes one of three forms and must agree with the lifecycle folder — the gate cross-checks them:

- `Status: proposed`
- `Status: implemented`
- `Status: rejected — <why, in one line>`

The status carries no dates and no parentheticals. The filename holds the first-proposed date and git holds the rest. Rejection is the one status with content, because the verdict is what a reader comes for.

### Body skeleton

Every record opens its body with `## Problem` — the motivation, written so it stands up without the solution. What follows depends on the lifecycle. Bespoke technical sections (schemas, wire contracts, topology) are free-form and go between the required ones.

**`proposed/`**

```markdown
## Problem
## Proposal
…bespoke sections…
## Alternatives considered
## Acceptance criteria
## Risks
```

`## Proposal` may speak in the future tense; plans, sequencing, and open questions belong here while the work is unbuilt. `## Acceptance criteria` states the observable condition that means done. `## Risks` covers what could go wrong *and* what the change knowingly gives up.

**`implemented/`**

```markdown
## Problem
## Decision
…bespoke sections…
## Alternatives considered
## Consequences
```

`## Decision` describes shipped reality in the present tense. `## Consequences` records what the trade-off cost **and** what it bought. Proposal-era headings are spec-speak once the work has shipped, and the gate rejects them here: `## Proposal`, `## Plan`, `## Migration plan`, and `## Acceptance criteria`. A present-tense `## Testing`, `## Deferred`, or `## Related` section is fine.

**`rejected/`**

The proposal, frozen. It keeps whatever sections it had; the verdict lives on the `Status:` line. Only the header block, the `## Problem` opener, and the alternatives rule below apply.

### `## Alternatives considered` is mandatory

Every record carries it: each genuine alternative and why it lost, one bold-led paragraph each. A decision recorded without what it beat gets re-litigated the first time someone finds it inconvenient — which is the exact failure this tier exists to prevent.

Alternatives are recorded, not invented. If a record predates this format and its alternatives cannot be reconstructed from the record, use this exact line in place of the section:

```markdown
<!-- decision-format: alternatives-not-recorded (pre-format record) -->
```

### Moving between lifecycles

Moving a file between lifecycle folders means rewriting it to satisfy the new folder's skeleton in the same change; the gate fails the move otherwise.

`proposed/` → `implemented/` rewrites `## Proposal` into a present-tense `## Decision`, folds `## Acceptance criteria` and `## Risks` into `## Consequences` (or a present-tense `## Testing` section for whatever now pins the behavior), and drops the plan in favor of what shipped. `proposed/` → `rejected/` only adds the reason to the `Status:` line and freezes the file.
