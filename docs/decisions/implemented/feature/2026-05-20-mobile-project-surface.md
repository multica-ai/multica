# Decision: Mobile's Project surface targets operate, assign, and collaborate

Status: implemented

## Problem

Mobile was preparing its first user-facing release, and its Project surface was behind web and desktop on an unbounded list of things. Shipping "parity" as a single goal would have meant either an indefinite delay or an arbitrary stopping point chosen under deadline pressure.

The three use cases that actually define the phone context are operating on an issue, assigning work, and talking about it. Gaps that block those are release-blocking; gaps that do not are not, however visible they are next to the web version.

## Decision

Parity is scoped to the operate / assign / collaborate use cases, and every gap outside them is recorded as a deliberate deferral rather than left as an implied to-do.

In scope for the release: finishing the picker-sheet migration onto the route-modal pattern, a progress section on the project detail screen, a Board and List view switcher over the detail screen's related issues, and draft persistence on the create form mirroring the existing new-issue draft store. Two quality sweeps ran alongside — replacing hardcoded hex values with tokens, and replacing hand-drawn SVG icons.

Deferred, in full knowledge of the gap: pin, inline title editing, the emoji picker, filters, and Gantt. Some divergences are permanent rather than deferred, where the phone context makes the web behavior wrong rather than merely absent.

The audit that produced this split lives in git history alongside this record. Its inventory is not maintained here — the current state of the surface is in the code, and a hand-maintained gap table would be wrong within a release.

## Alternatives considered

**Full parity with the web Project surface before release.** Rejected. The list is unbounded and dominated by items that do not serve any of the three phone use cases; pursuing it delays the release without improving it.

**Ship what exists and handle gaps as they are reported.** Rejected. The distinction that matters is between a gap nobody has decided about and a gap someone decided to accept. Without the audit, every later reader has to re-derive whether an absence was intentional.

**Keep the gap audit as a living document.** Rejected. It is a snapshot taken to make one decision, and its value expires with that decision. Maintaining it would mean re-auditing on every change, which is what the phrase "documents that no longer match the code" describes.

## Consequences

The release ships a Project surface that is complete for its stated use cases and visibly incomplete next to web for everything else, which is the intended trade.

Deferred items have no tracking document. They are in the issue tracker or they are not being done — which is the point, since a deferral list inside the repository becomes a stale to-do list within a release or two.
