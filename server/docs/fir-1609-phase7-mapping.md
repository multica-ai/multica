# FIR-1609 Phase 7 — Credential policy → toolpolicy chain mapping (DESIGN, pre-implementation)

## The security invariant (must not regress)
- Credential grants live in `cerebro_workspace_grant` and are **deny-by-default**:
  a non-owner actor with no matching live grant gets **Deny** for `credential.<perm>`.
- The `toolpolicy` chain (`internal/cerebro/toolpolicy`) is **default-ALLOW and tighten-only**
  (`chain.go` Resolve: base=Allow, layers can only raise restrictiveness; an Allow row can
  never loosen a Deny). Restrictiveness rank: Allow(0) < Ask(1) < Deny(2); Inherit = no opinion.
- Therefore a *naive* swap (resolve the verdict purely from `Store.Resolve`, base=Allow) would
  make every `credential.reveal` **Allow-by-default** for any non-owner with no rows authored —
  a critical regression. A coarse workspace-level `Deny` base also fails: tighten-only means a
  per-actor `Allow` row cannot loosen it ("a Deny base can't be loosened by Allow rows").

## Chosen mapping (mirror Phase 5 data-source projection)
`multicaCredentialPolicy.Check`, evaluated per scope (id first, then type fallback):

1. **Grant floor (deny-by-default, projected):** look up the actor's live grant for
   (`capability = credential.<perm>`, resource = scope) via the existing grant store.
   - granted, no approval needed → floor = `Inherit` (no Deny projected)
   - granted, approval_required → floor = `Ask`
   - not granted / no live grant → floor = **`Deny`** (the projected explicit Deny row)
2. **Admin/system caps (tighten-only):** `Store.Resolve` over the toolpolicy chain with
   `Base = Allow`, `ToolKey = credential.<perm>`, `ResourcePattern = scope`, and the actor's
   `RuntimeID/AgentID/UserID/GroupIDs/SystemID/IsSystem`. With no authored rows this yields
   `Allow` (contributes no cap); admin Deny/Ask rows tighten on top.
3. **Combine = most-restrictive(floor, capEff.Setting)** (monotonic max over rank — order-free):
   - `Allow` → allow
   - `Ask`  → route to the shared approval gate (`m.gate`, FIR-2586) — unchanged Ask path
   - `Deny` → deny (reason from whichever side won: "no credential grant" vs "capped by <layer>")
4. **Scope fallback:** if the **id** scope combines to `Allow`, allow immediately. Otherwise try
   the **type** scope and combine. A `Deny` at id does NOT block a `type` grant (matches the
   existing id→type fallback semantics in PersonaPolicyChecker / the current resolver path).
   Approval is raised most-specific-first only when no scope allows.

## Actor mapping
- `member` → `UserID = req.ActorID` (RuntimeID/AgentID/SystemID absent).
- `agent`  → `GetAgent(req.ActorID)` → `AgentID = id`, `RuntimeID = agent.RuntimeID`,
  `UserID = agent.OwnerID` (owner's user ceiling still applies), `IsSystem` left false here
  (credential calls are actor-driven, not human-less autopilot runs); `SystemID` absent.
- Owners never reach this checker — `OwnerPolicyChecker` short-circuits Allow first in the chain.

## Why keep the grant lookup (not a literal "rip out the resolver")
Deny-by-default in a tighten-only chain is *only* achievable by projecting grant-state into the
chain at resolve time (the Phase 5 pattern). The grant lookup is the source of that projection;
it cannot be dropped without losing deny-by-default. The chain becomes the **decision engine on
top of the projected floor** — that is the "swap": the verdict now flows through `toolpolicy`
(so System/runtime/user/group/condition caps apply to credentials too), while grant-state feeds
the floor. The full `cerebropermissions.Resolver` decision logic is replaced by the chain; only a
thin grant-state lookup remains to compute granted/approval/not-granted.

## Outcome (post adversarial review)
Two adversarial passes were run (Codex CLI not present in this runtime → adversarial
reviewer subagent on the memo, then on the concrete diff). The memo's first design
(external `max()` fold marketed as "mirror Phase 5") was rejected as unsafe. The
**implemented** design instead keeps the resolver as the deny-by-default grant floor
and layers the toolpolicy chain as a tighten-only cap: `final =
MoreRestrictive(grantFloor, adminCap)`, so the verdict is provably never looser than
today. Second review verdict: **SAFE TO COMMIT** — no default-allow hole, every error
path fails closed, cap proven tighten-only (tested 3×3). Tracked follow-ups: the caps
are inert until an admin-authoring surface emits rows with the canonical
ToolKey/ResourcePattern (pinned in code); owner-user ceiling applies to an agent's
credential call (fail-closed, intended); Ask raised from a type-scope cap is attributed
to the id scope (cosmetic). NOTE: the dedicated **Codex** pass mandated by the issue is
still owed as part of the final gate (Codex CLI unavailable here).

## Open questions for the adversary
- Q1: Is folding the floor as `max(floor, capEff)` exactly equivalent to injecting a synthetic
  Deny row into `toolpolicy.Resolve`? Any case where attribution/Ask-vs-Deny differs and matters?
- Q2: Scope fallback — can the id→type combine ever *loosen* below the grant floor (security hole)?
- Q3: IsSystem for agent credential calls — should a human-less autopilot agent revealing a
  credential get the Ask→Deny fail-safe? (Currently left false; argue both ways.)
- Q4: Gate Ask path: when floor=Ask (approval_required grant) but capEff=Allow, we still raise an
  approval — correct? When floor=Deny but capEff=Ask, we deny without asking — correct?
