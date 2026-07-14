---
title: "Hover card inconsistency: mention hover must reuse the same profile cards as actor avatars"
date: 2026-07-10
category: ui-bugs
module: mentions
problem_type: ui_bug
component: tooling
severity: medium
symptoms:
  - Mention hover card shows only avatar + name (simple inline card)
  - Comment-author hover card shows rich profile (description, runtime, skills for agents)
  - User reports mention hover is "not aligned with other hover cards"
root_cause: wrong_api
resolution_type: code_fix
tags: [hover-card, mention-chip, profile-card, consistency, actor-avatar]
---

# Hover card inconsistency: mention hover must reuse the same profile cards as actor avatars

## Problem

When upgrading actor mentions to Avatar Chips, the new hover card (`MentionHoverCard` in `packages/ui`) rendered a simple inline card — avatar (32px) + name + optional role. This was visually inconsistent with the product's established actor hover cards, where hovering a comment-author avatar or an assignee avatar shows a **rich profile card** (e.g. `AgentProfileCard` with description/runtime/skills, `MemberProfileCard`, `SquadProfileCard`). Users expected hovering a mention chip to show the same rich card as hovering that actor elsewhere.

## Symptoms

- Hovering a member/agent/squad mention shows a small card with just avatar + name.
- Hovering the same actor's avatar elsewhere (comment author, assignee) shows a full profile card with more information.
- User reports: "hover 后出现了卡片，但只显示了名称，没有和其他hover卡片对齐（起码智能体/小组的没对齐）"

## What Didn't Work

- `packages/ui/components/common/mention-hover-card.tsx` — simple inline card, can't import profile cards (package boundary: `packages/ui` cannot depend on `packages/views`).
- The simple card used `ActorAvatar(size=32) + name + role` — correct content but wrong level of detail compared to established cards.

## Solution

Move the mention hover card to the **views layer** where profile cards are accessible:

1. Create `packages/views/editor/mention-hover-card.tsx` that renders:
   - member/agent/squad → `AgentProfileCard` / `MemberProfileCard` / `SquadProfileCard` (same cards used elsewhere).
   - @all → static "All members" card (i18n'd via `editor.mention.all_members`).
   - Uses `HoverCard` + `HoverCardTrigger` (non-focusable — chip owns focusability per R14).
   - `HoverCardContent` width `w-72`, `align="start"` — matches `ActorAvatarHoverCardShell` layout.

2. Switch editor (`packages/views/editor/extensions/mention-view.tsx`) and readonly (`readonly-content.tsx`) to import the views-layer hover card instead of the `packages/ui` one.

3. The `packages/ui` `MentionHoverCard` becomes dead code (no importers) — delete it.

**Key code (`packages/views/editor/mention-hover-card.tsx`):**

```tsx
const content: ReactNode =
  type === "agent" ? <AgentProfileCard agentId={id} /> :
  type === "member" ? <MemberProfileCard userId={id} /> :
  type === "squad" ? <SquadProfileCard squadId={id} /> :
  <AllMembersContent label={t(($) => $.mention.all_members)} />;

return (
  <HoverCard>
    <HoverCardTrigger render={<span />} className="cursor-default">
      {children}
    </HoverCardTrigger>
    <HoverCardContent align="start" className="w-72">{content}</HoverCardContent>
  </HoverCard>
);
```

## Why This Works

The profile cards (`AgentProfileCard` etc.) are self-contained React components that fetch their own data via `useQuery`. They're used by the views `ActorAvatar` component with `enableHoverCard`. By rendering them directly in the mention hover card, the mention hover becomes identical to the actor-avatar hover — same width (`w-72`), same alignment, same content. The trigger uses a non-focusable span so the chip controls focusability (editor: focusable; readonly: not).

## Prevention

- When adding hover cards for actors, always check whether `packages/views/common/actor-avatar.tsx` already has a hover implementation for that actor type (via `enableHoverCard`). Reuse the same profile cards rather than creating a new simpler card.
- The `packages/ui` / `packages/views` boundary means UI-primitive hover cards can't import rich profile components. Design hover logic in the views layer when the content depends on domain data.
- Package boundary rule: `packages/ui` = atomic UI only, no `@multica/core`. If the hover card needs domain data or profile cards, it belongs in `packages/views`.

## Related

- E2E tested on a locally self-hosted Multica instance (PM2, production build on port 3001).
- `packages/views/common/actor-avatar.tsx` — the existing actor hover mechanism; its `ActorAvatarHoverCardShell` uses `w-72` + `align="start"` which the new mention hover card matches.
- PR multica-ai/multica#5199 — the upstream contribution containing this fix.
