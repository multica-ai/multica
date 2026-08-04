# FIR-4507 — Mobile open path for agent profile card

**artifact_contract:** ce-unified-plan/v1  
**artifact_readiness:** implementation-ready  
**execution:** code  
**Issue:** FIR-4507  
**Date:** 2026-08-04

## Direction

Agent/member/squad profile peek works on desktop hover but is unreachable on touch. Ship a coarse-pointer open path that reuses the same profile card content.

## Settled decisions (user-approved)

| Decision | Provenance | Rejected alternative | Reason |
| --- | --- | --- | --- |
| Coarse → Sheet (phone) / Popover (tablet); fine → HoverCard | user-approved | long-press only; navigate-only; floating popover everywhere | Discoverable tap; phone-native chrome; no desktop regression |
| Detail link is the path to agent page on touch | user-directed | invent a second link | Detail already points at `agentDetail`; only visibility is broken (`group-hover`) |
| Change surface is shared shell + card Detail visibility | user-approved | per-call-site mobile forks | One choke point covers 20+ `enableHoverCard` sites |
| Native app out of scope | user-approved | parity in same PR | Mobile app deliberately has no hover card today |

## Scope

**In**
- Touch-aware open mode for `ActorAvatar` hover cards (agent profile, agent live, member, squad)
- Always-visible Detail link on agent + squad cards; add Detail on member card
- Suppress avatar→profile navigation on coarse when hover card is enabled (tap opens peek)
- Unit tests for shell branching; e2e touch path if practical

**Out**
- Native `apps/mobile` parity
- Global HoverCard primitive rewrite
- Redesigning profile card content

## Approach

1. **Cerebro zone** — new `ActorAvatarHoverCardShell` in `@multica/cerebro-agent-avatar` (Sheet / Popover / HoverCard). Coarse via `(pointer: coarse)`; narrow via `useIsMobile` (<768).
2. **Upstream thin patch** — `packages/views/common/actor-avatar.tsx` imports the cerebro shell and skips `ActorAvatarProfileLink` when coarse + `enableHoverCard`.
3. **Detail visibility** — replace hover-only opacity on agent/squad cards; add member Detail → `memberDetail`.
4. **Markers** — every upstream edit carries `CEREBRO-PATCH(actor-hover-touch): FIR-4507 …` and is registered in `docs/cerebro-patches.md`.

## Files

| Path | Role |
| --- | --- |
| `packages/cerebro-agent-avatar/hover-shell.tsx` | Touch-aware shell |
| `packages/cerebro-agent-avatar/use-coarse-pointer.ts` | `(pointer: coarse)` hook |
| `packages/cerebro-agent-avatar/hover-shell.test.tsx` | Unit tests |
| `packages/cerebro-agent-avatar/index.ts` | Export |
| `packages/views/common/actor-avatar.tsx` | Wire shell + skip profile link on coarse |
| `packages/views/agents/components/agent-profile-card.tsx` | Detail always visible |
| `packages/views/squads/components/squad-profile-card.tsx` | Detail always visible |
| `packages/views/members/member-profile-card.tsx` | Add Detail link |
| `docs/cerebro-patches.md` | Register patch |
| `e2e/fir-4507-agent-profile-touch.spec.ts` | Optional Playwright coarse/tap |

## Test scenarios

1. Fine pointer: hover still opens HoverCard; content unchanged.
2. Coarse + narrow: tap avatar opens bottom Sheet with profile content; outside/dismiss closes.
3. Coarse + wide: tap opens Popover.
4. Coarse + hover enabled: tap does **not** navigate to profile; Detail does.
5. Agent/squad/member Detail links navigate to correct detail routes.
6. Nested row click (comment author): opening peek does not activate the row.

## Verification

- `pnpm --filter @multica/cerebro-agent-avatar test`
- `BASE_REF=origin/main bash scripts/validate-cerebro-patches.sh` after commit
- Typecheck affected packages
- PR with `Closes FIR-4507`, label `staging-only` until CI green then `prod-ready` if green
