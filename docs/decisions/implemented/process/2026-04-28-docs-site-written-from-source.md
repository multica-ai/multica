# Decision: The documentation site is written from source, concepts first

Status: implemented

## Problem

The first plan for the product documentation site was written before anyone read the code. Its division of concepts turned out to be wrong in ways that only showed up against the implementation: pages promised behavior the service layer did not have, and the shape it imposed on the product did not match how the product actually works.

Multica's documentation also differs from ordinary SaaS documentation in three ways that a generic outline does not account for. Its vocabulary is unusually heavy — Workspace, Agent, Runtime, Daemon, Skill, Autopilot, Trigger, session resumption — and none of it is obvious to a new user. Its execution model is distributed: the server does not run agents, the user's local daemon does, which is the root of nearly every "why isn't my agent working" question. And a real share of its readers are coding agents following the instructions literally, not humans skimming.

## Decision

The site under `apps/docs/` is written from the source, with concepts as its first pillar and the distributed architecture explained early. The authoring rules that follow from this are in [`apps/docs/AGENTS.md`](../../../../apps/docs/AGENTS.md); the load-bearing one is that every factual claim must be locatable in the code, and a capability that is not implemented is not documented even if the UI hints at it, a column exists for it, or a handler accepts it.

English is the primary language, with other locales as translations of it.

## Alternatives considered

**Extend the first outline instead of replacing it.** Rejected. Its problem was not coverage but its division of concepts, which was derived from an assumed product rather than the built one. Extending a wrong decomposition means every later page inherits it.

**Write the site from the product's own descriptions and the interface.** Rejected. That is how the first version acquired claims the code does not support. The interface shows affordances that are partly wired, and product copy describes intent.

**Lead with task-based guides rather than concepts.** Rejected. Task guides work when the vocabulary is already shared. Here a reader who does not know what a Runtime is cannot follow a guide that mentions one, so the concepts have to come first.

**Keep the planning documents in the repository as the site's tracker.** Rejected, and the reason generalizes beyond this site: a per-page status table is a to-do list that goes stale the moment someone ships without reopening it. What was durable in those plans — the writing rules and the reader tiers — moved into `apps/docs/AGENTS.md`, where it applies to every page written from now on. Progress belongs in the issue tracker.

## Consequences

Writing a page costs more than describing the product, because each claim has to be checked against the implementation. That cost is the point: it is what keeps the site from documenting things that do not exist.

Because coding agents read these pages and execute them, every command in a procedure has to be complete and independently runnable. A step that says to modify the previous command breaks for that reader.

The site is versioned with the code, so a change that alters documented behavior is expected to update the affected page in the same change.
