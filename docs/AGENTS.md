# AGENTS.md — The documentation standard

This file defines the tiers a repository document can belong to, the rules every document follows, and the ceilings `pnpm docs:check` enforces. It governs documentation **inside this repository**. The product documentation site under [`apps/docs/`](../apps/docs/AGENTS.md) has its own authoring contract.

Read this before adding a Markdown file, and before writing more than a paragraph into an existing one.

## One home per fact

Every fact belongs to exactly one tier — the tier whose job it is. Everywhere else links to that home. Duplicated prose drifts silently; a link cannot, because `pnpm docs:check` fails when its target disappears.

| Tier | Job | Does NOT belong there |
|---|---|---|
| Root [`CLAUDE.md`](../CLAUDE.md) | Standing orders: the hard rules an agent needs in context every session, stated once, each a line or two | Tutorials, worked examples, rationale, per-subtree detail, anything restated from a linked home |
| Root [`AGENTS.md`](../AGENTS.md) | A pointer to `CLAUDE.md` for agents that look for `AGENTS.md` | Rule text of any kind — a second copy of a rule is a rule that will disagree with itself |
| Subtree `CLAUDE.md` ([`apps/mobile/`](../apps/mobile/CLAUDE.md)) | Orders that apply only inside that subtree | Repo-wide rules the root file already carries |
| [`docs/decisions/`](decisions/README.md) | Decision records: why a decision was taken, what it beat, what it cost, and — for anything unbuilt or half-built — what is still only proposed | Reference material, step-by-step procedures, task checklists |
| `docs/*.md` (reference) | The durable contract of one subsystem: how it behaves **today** | Plans, implementation status, change history, decision rationale |
| [`docs/runbooks/`](runbooks/) | Operator procedures: numbered steps, preconditions, and how to tell it worked | Design rationale, subsystem reference |
| [`CONTRIBUTING.md`](../CONTRIBUTING.md) | First-stop contributor onboarding: local setup, daily workflow, verification | Deployment guides, product explanation, decision history |
| [`README.md`](../README.md), [`SELF_HOSTING*.md`](../SELF_HOSTING.md), [`CLI_AND_DAEMON.md`](../CLI_AND_DAEMON.md), [`CLI_INSTALL.md`](../CLI_INSTALL.md) | The repository front door for users and operators: what this is, how to install and run it | Internal architecture, contributor procedure, decision history |
| [`apps/docs/`](../apps/docs/AGENTS.md) | The published product documentation site | Contributor procedures, internal decision history, repo-only detail |
| Package README (`apps/*/README.md`, `server/pkg/*/README.md`) | One package's contract: what it exposes, its configuration and limits | Other packages' concerns, restatement of doc comments |
| `.agents/skills/`, `server/internal/service/builtin_skills/` | Agent workflows and the skills shipped to users | Product or runtime contracts that belong in `docs/` or in source |

Placement in one line: rationale and status → a decision record; how a subsystem behaves → a `docs/` reference doc; how to perform an operation → a runbook; a rule an agent must not violate → `CLAUDE.md` with a link to the decision that owns the why.

## Writing rules

- **Document current state, not change history.** Do not write "previously", "now", "no longer", "used to", or name PRs and commits in durable prose. State the live mechanism. The change story belongs in git, in the PR, or in the decision record.
- **Implementation status lives in `docs/decisions/` and nowhere else.** No `Status: Draft` headers, `Last updated:` lines, phase checklists, `TODO` inventories, or "not started" annotations in any other tier. Status rots the moment someone ships without reopening the file; the lifecycle folder a decision record sits in cannot, because moving it is the act of changing its status.
- **Every non-trivial change carries a decision record in the same PR.** Add one, or update the record that already owns the decision. Only mechanical or purely local edits are exempt — see [when to write one](decisions/README.md#when-to-write-one).
- **Repository documents are written in English.** The audience is professional programmers and coding agents, and this repository is public. Three things are deliberately outside that rule: translated counterparts of the user-facing front door such as [`README.zh-CN.md`](../README.zh-CN.md), the documentation site's localized pages under their own [authoring contract](../apps/docs/AGENTS.md), and the product copy `packages/views/locales/` owns.
- **Cross-reference with relative Markdown links, never bare filenames.** `pnpm docs:check` verifies that every link target exists. It does not verify `#anchor` fragments — check those yourself.
- **Nothing durable is unregistered.** Every document in a governed tier has an entry in [`scripts/docs.manifest.json`](../scripts/docs.manifest.json). An unregistered Markdown file under `docs/` fails the gate, which is what stops a stray plan from quietly becoming a permanent fixture.
- **Prose is for professional programmers.** Prefer plain, direct English over metaphor. Reserve bold for the clause that changes behavior; if everything is emphasized, nothing is.

## Word budgets

[`scripts/docs.manifest.json`](../scripts/docs.manifest.json) sets a ceiling for each governed document, and `pnpm docs:check` rejects a file that exceeds its ceiling or has gone missing.

When the gate goes red:

1. **Relocate** the content that belongs in another tier, leaving a link if a reader needs the pointer.
2. **Condense** what genuinely belongs here.
3. **Raise** the ceiling only when the words truly need the space, and justify the manifest diff in the PR. A ceiling set too low is a bug in the ceiling.

Ceilings are guardrails, not reduction targets. Keep roughly 5% headroom.

## The slop checklist

Hunt these whenever you touch a document:

- The same rule stated in two tiers. Grep a distinctive phrase, keep one home, turn the rest into links.
- Narrated history: "previously", "now", "no longer", "was renamed", references to a PR or commit number.
- Implementation-status annotations outside `docs/decisions/`: `Status: Draft`, `Owner: TBD`, `Last updated:`, "Phase 1 — not started", "already shipped".
- A hand-maintained inventory that the tree, `package.json`, or a query already answers: file lists, test lists, per-page migration tables.
- A task tracker wearing a document's clothes: checkbox lists of work someone intends to do. Work lives in the issue tracker; the decision behind it lives in a decision record.
- A war story told inline where one rule plus a link to the record would do.
- Restating a code comment, a generated file, or a type declaration instead of linking to it.
- Reasoning transcripts: the path used to reach a conclusion, kept alongside the conclusion. Keep the contract; delete the derivation.
- Paragraph walls carrying four rules and three asides. Split them, or demote the detail to its home.
