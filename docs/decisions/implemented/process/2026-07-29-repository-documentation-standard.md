# Decision: Documentation tiers are defined in a document and enforced by a gate

Status: implemented

## Problem

Repository documentation had no format standard and no home for status, and both gaps showed.

There was no agreement on what a document is. `docs/` mixed subsystem reference, a design system, a product overview, three planning documents, an audit, two runbooks, and a `plans/` subdirectory with its own YAML frontmatter schema. `apps/mobile/docs/` held a fourth shape — research, an ADR, a gap audit, a plan, a migration document. Status was expressed four different ways: a `> Status: Draft` blockquote, a `**Status**:` bold line, YAML frontmatter, and nothing at all. `Owner: TBD` and `Last updated:` appeared on documents nobody owned or updated.

Because no tier owned implementation status, it leaked into every tier. `docs/analytics.md` narrated its own change history in prose — "PostHog had become…", "is now flagged by…" — so a reader had to reconstruct the present from a description of a transition. `apps/mobile/docs/rnr-migration.md` said "Phase 1 (base infrastructure) — not started" while the dependencies were installed and the components were shipped. `docs/plans/2026-07-17-…` carried a dated "Implementation status" paragraph inside a plan. `docs/agent-quick-create-plan.md` sat at "Draft (设计阶段，未动工)" for two and a half months. `docs/product-overview.md` declared a data cutoff three months old and duplicated ground that 35 published site pages already covered in four languages.

The common failure is that status written into prose is only correct at the instant it is written, and nothing forces anyone to revisit it. A document that says "not started" about shipped work is worse than one that says nothing, because a reader believes it.

## Decision

Two artifacts and one gate.

**[`docs/AGENTS.md`](../../../AGENTS.md) is the standard.** It names every tier a document can belong to, states what each tier owns and what does not belong in it, gives the writing rules, and carries a slop checklist. Its central rule is that each fact has exactly one home and everywhere else links to it. It sits at `docs/AGENTS.md` so that an agent working under `docs/` picks it up automatically.

**[`docs/decisions/`](../../README.md) is the only place implementation status lives.** A decision record's path encodes both its status and its kind: `{lifecycle}/{class}/yyyy-mm-dd-topic-title.md`, where lifecycle is `proposed/`, `implemented/`, or `rejected/`, and class comes from a closed set of six. Status cannot go stale in place, because changing it means moving the file. Anything unbuilt or half-built belongs in `proposed/`, which is what gives half-finished work a legitimate home instead of a stale annotation in a reference document. Every record opens with `## Problem` and must carry `## Alternatives considered`; an implemented record must speak in the present tense, and the gate rejects proposal-era headings in one.

**[`scripts/verify-docs.mjs`](../../../../scripts/verify-docs.mjs) enforces the mechanical parts** through `pnpm docs:check`, in CI on every pull request and in `make check`. It verifies that every governed document is registered in [`scripts/docs.manifest.json`](../../../../scripts/docs.manifest.json) with a word ceiling and stays under it, that no status metadata or checkbox tracker appears outside `docs/decisions/`, that every relative link resolves, and that every decision record matches the format.

The registry requirement is what stops the original problem recurring: a new plan document dropped into `docs/` fails the gate until someone either gives it a tier and a ceiling or puts it where it belongs.

## Alternatives considered

**Write the standard and rely on review to enforce it.** Rejected. The documents being cleaned up were themselves the product of that approach — each was reasonable when written and nothing caught it going stale. A rule with no gate is a rule that decays at the rate people forget it, and stale status is invisible precisely because it looks like content.

**Adopt the reference repository's Agent Notes wholesale, including its `archived/` tree and its per-file supersession and translation rules.** Rejected as premature. That machinery is calibrated to 742 notes; this repository starts with eleven. Three lifecycles with git as the archive is the right size now, and an archive tier can be added when the volume justifies it. The parts that were adopted — path-encoded status, a closed class set, the mandatory alternatives section, no central index — are the parts that solve the stated problem.

**Enforce one physical line per paragraph, as the reference repository does.** Rejected. It makes diffs readable, but retrofitting it reformats every existing document, which buries the substantive changes in this cleanup under whitespace churn.

**Keep planning documents where they are and only add a status convention.** Rejected. A consistent status field would have made the rot legible without removing it, and the underlying problem is that a reference document is the wrong container for a plan. Moving plans into a lifecycle tree removes the class of error rather than labeling it.

**Add a generated index of decision records.** Rejected. The lifecycle and class folders are already the inventory, and an index is one more artifact that goes stale when someone forgets to regenerate it.

## Consequences

Implementation status has exactly one home, and it is a home where staleness requires moving a file rather than merely failing to edit one.

Adding a document now costs a manifest entry and a word ceiling. That friction is deliberate and aimed at the specific failure of ad-hoc documents accumulating in `docs/`.

Existing content was rewritten rather than relocated. Eleven planning, audit, and research documents became decision records in the present tense, which means their prose is new even where their substance is not. Superseded originals are in git history, not in the tree.

`docs/product-overview.md` was deleted rather than rewritten. Its concept and feature material duplicates the published site, its stated data cutoff was three months old, and keeping an internal copy accurate would mean maintaining the same facts twice. Its route map and database quick reference went with it; if either is wanted, the site is the maintained home.

Chinese-language internal documents were rewritten in English, since the repository is public and its readers are contributors and coding agents. Translated user-facing entry points and the site's localized pages are unaffected.

`apps/mobile/CLAUDE.md` remains far larger than any other subtree file. Its rules are load-bearing and mobile-specific, so it is registered with a ceiling that reflects its real size rather than being condensed in a change about documentation structure; only its inline incident narrative moved out to a decision record.

The gate checks that a link target exists, not that an `#anchor` within it does.
