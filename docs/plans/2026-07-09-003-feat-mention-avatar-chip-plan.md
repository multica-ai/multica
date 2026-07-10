---
title: Mention Avatar Chip - Plan
date: 2026-07-09
type: feat
topic: mention-avatar-chip
artifact_contract: ce-unified-plan/v1
artifact_readiness: implementation-ready
product_contract_source: ce-brainstorm
execution: code
deepened: 2026-07-10
---

## Goal Capsule

- **Objective:** Upgrade member/agent/squad/@all mentions from plain text to Avatar Chips on par with IssueChip/ProjectChip — closing the visual prominence gap that makes users unsure whether their mention was correctly recognized.
- **Product authority:** This plan governs the visual form, interaction, and platform scope of actor mentions in the Multica rich-text editor and readonly content.
- **Open blockers:** None.
- **Execution profile:** `code` — frontend components, CSS, and mobile rendering.

---

## Product Contract

### Summary

Upgrade actor mentions (member, agent, squad, @all) from plain `<span class="mention">@name</span>` to Avatar Chip components: a pill container with a 14-16px ActorAvatar + `@name` label, with type-specific background coloring (member=`bg-muted`, agent=`bg-brand/10`, squad=`bg-info/10`, @all=`bg-warning/10`). Unified rendering in both the editor and readonly content. Hover triggers the existing MentionHoverCard with no click navigation. Web and desktop get full platform coverage; mobile is scoped to suggestion-bar preview tint alignment (readonly chips deferred to a renderer-capable follow-up, editor stays plain-text by platform constraint — see U5, R11).

### Problem Frame

The mention system has a visual hierarchy inversion: IssueChip and ProjectChip (used less frequently) render as bordered inline cards with icons and structured layout, while member/agent/squad mentions (the most frequently used mention type) render as bare colored text — `color: var(--primary); font-weight: 600` with no background, no border, and no avatar.

The type distinction already exists in the data layer (`mention://member/{id}` vs `mention://agent/{id}`), in the ActorAvatar component (different icons and shapes per type), and in the autocomplete dropdown (type badges). But this information is entirely discarded at inline render time.

The autocomplete shows rich previews (avatar + name + type badge) when the user selects a mention, but the rendered result collapses to plain bold text. This creates a "recognition gap" — the user cannot visually confirm from the rendered output that their mention was correctly recognized. The stated user need is: "the current visual representation is not prominent enough — I want clearer visual feedback so I know the mention syntax was correctly used."

### Key Decisions

- **Parity with IssueChip/ProjectChip.** The Avatar Chip follows the same visual grammar as the existing chip components: bordered pill, inline-flex layout, `text-xs`, similar height budget (~22px). This is a deliberate alignment choice — the product already established a "chip" visual language for entity references; actor mentions should participate in it rather than using a different pattern.

- **All types get background color, not just agent/squad.** Members are the most frequent mention type, and one could argue for making them visually lighter (backgroundless) to reduce visual density. The decision is to give all types a background tint: consistency with IssueChip/ProjectChip (which always have borders), and the "color as signal" principle — the background color signals "this is an interactive entity reference, not plain text." Per-type colors carry the type distinction.

- **Hover-only interaction, no click navigation.** The chip triggers MentionHoverCard on hover (avatar + name + role popup). No click-to-navigate. This keeps the chip as a visual/confirmation element without adding new navigation paths. Navigation can be added later as a separate enhancement.

- **Unified rendering: editor and readonly use the same chip.** Both MentionView (editor inline) and ReadonlyContent (markdown rendering) use the same Avatar Chip component. The user sees the same visual form when composing and when reading.

- **Platform-appropriate scope in one release.** Web and desktop are fully in scope. Mobile has platform constraints that prevent inline chips in readonly content (native markdown renderer has no custom-React-renderer hook) and during editor composition (TextInput sentinel). In this scope, mobile parity is limited to suggestion-bar preview tint alignment; mobile readonly chips are deferred to a renderer-capable follow-up (see U5, R11, Scope Boundaries).

### Requirements

**Visual form**

- R1. Actor mentions (member, agent, squad, @all) render as an inline Avatar Chip: a pill container with `rounded-full` or `rounded-md` border-radius, a 1px border, subtle background tint, a 14-16px ActorAvatar, and the `@name` label.
- R2. All avatar types render as circles, matching upstream's unified avatar shape invariant (MUL-4277). The avatar content follows existing ActorAvatar logic: initials for members, Bot icon for agents, Users icon for squads, and the `Users` icon for @all (per KTD4 — same icon as squads, rendered in a warning-tinted circle to distinguish it from the squad chip). Type distinction is carried by the pill's background tint only.
- R3. Background tint and border color vary by `data-mention-type`:
  - member: `bg-muted` with `border-border`
  - agent: `bg-brand/10` with `border-brand/20`
  - squad: `bg-info/10` with `border-info/20`
  - @all: `bg-warning/10` with `border-warning/20`
- R4. The chip retains the `@` prefix before the name label (e.g., `@张三`, `@ReviewerBot`).
- R5. The chip height fits within the existing prose line-box budget (~22px, matching IssueChip's proven fit in 14px/1.625 line-height prose).
- R6. Long names truncate with an ellipsis (`truncate`), using `max-w-[8rem]` as a tighter cap than IssueChip's `max-w-full`. The tighter cap reflects that actor mentions appear more frequently in dense paragraph text than issue references, and multiple long-name mentions on one line would otherwise dominate the layout. The `min-w-0` pattern is still applied to enable truncation.
- R7. All colors use design tokens (no hardcoded Tailwind colors). Dark mode works through token resolution.

**Rendering contexts**

- R8. The Avatar Chip renders identically in the Tiptap editor (MentionView) and in readonly markdown content (ReadonlyContent).
- R9. The Markdown encoding (`mention://{type}/{id}`) remains unchanged. The chip is purely a visual-layer change.
- R10. Existing mentions in stored content render as Avatar Chips without any data migration — the upgrade is automatic on render.
- R11. Mentions render with semantic/functional parity across web, desktop, and mobile, within platform constraints. Web and desktop render full Avatar Chips (editor + readonly). Mobile cannot render inline chips in readonly content: it uses `react-native-enriched-markdown`, a native renderer with no custom-React-renderer hook (see U5), where mentions appear as brand-colored links; mobile editor composition is plain-text `@name` (TextInput sentinel). Mobile parity is delivered where the platform allows — the suggestion-bar preview matches the chip's per-type coloring — and pixel parity is not required where the platform genuinely differs (e.g. mobile agent avatars use initials + brand color, with no distinct Bot icon).

**Interaction**

- R12. On hover, the chip background transitions to a slightly deeper tint (`hover:bg-accent` for members, `hover:bg-brand/15` for agents, etc.) with `transition-colors` (150ms, per DESIGN.md). Cursor remains `cursor-default` (no click navigation — hover background is the interactivity affordance; navigation may be added later).
- R13. Hovering the chip triggers the existing MentionHoverCard popup, showing the ActorAvatar (larger), full name, and role/type. The hover card works the same way for all mention types.
- R14. In the **editor**, the chip is focusable (`tabIndex={0}`), receives standard focus styling (`focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50`), and the MentionHoverCard opens on both hover and keyboard focus — giving keyboard users composing a message access to the same identity information as mouse users. In **readonly** content the chip is **not** focusable (no tab stop per mention, to preserve keyboard reader navigation in long comment threads); its `aria-label` conveys identity (name + type) to screen readers.

### Visualizations

The following shows the current state vs. the proposed Avatar Chip rendering:

```
Current (plain text):

  这个任务分配给了 @张三 并通知了 @ReviewerBot 和 @设计组。

Proposed (Avatar Chip):

  这个任务分配给了 [👤 @张三] 并通知了 [⚙ @ReviewerBot] 和 [◆ @设计组]。
                    ↑ muted bg          ↑ brand bg              ↑ info bg

Chip anatomy (agent example):

  ┌──────────────────────┐
  │  [⚙]  @ReviewerBot   │   bg-brand/10, border-brand/20
  └──────────────────────┘
    ↑ 14px ActorAvatar
       (Bot icon, circular)

Height: ~22px (fits in 14px/1.625 line-box, same budget as IssueChip)
```

### Scope Boundaries

**Deferred for later:**

- Insertion flash animation (a 150-200ms background-color transition when a mention is first inserted from autocomplete). This is a separate enhancement that complements the chip but is not required for the core visual-prominence goal.
- Contextually rich pills (online presence dots, agent runtime indicators, squad member counts). These require additional data subscriptions and are a natural follow-up once the chip component exists.
- Click-to-navigate behavior (navigating to member/agent profile pages on chip click). Can be added by wrapping the chip in a navigation handler later.
- Mobile readonly mention chips. Mobile readonly markdown renders through `react-native-enriched-markdown`, a native renderer with no custom-React-renderer hook (confirmed at `apps/mobile/lib/markdown/markdown.tsx:15-20`). Avatar chips on mobile readonly content require forking/replacing that renderer or building a parallel non-native mention renderer — a separate PR/plan. Mobile editor inline chips are likewise out of reach (TextInput sentinel, `apps/mobile/lib/mention-serialize.ts:3-5`). This scope aligns only the suggestion-bar preview (see U5).

**Outside this scope:**

- MentionRenderer unified dispatch component (replacing scattered rendering logic with a single dispatcher). This is an architectural refactoring that ce-plan can consider as a code organization decision, not a product requirement.
- Changes to the web/desktop autocomplete/suggestion UI — it already shows avatars and type badges, and is not part of this scope. (Mobile suggestion-bar per-type tint alignment remains in scope per R11/AE6/U5.)
- Changes to the Markdown encoding or mention data model — the `mention://` URL scheme and `data-mention-type` attribute already carry all needed information.
- Mobile activity rows — they render via `formatActivity` plain text (`apps/mobile/components/issue/activity-row.tsx`), not the markdown pipeline, so they are unaffected by mention-chip changes.

### Acceptance Examples

- AE1. **Member mention in editor.**
  - **Given:** A user is composing a comment in the Tiptap editor.
  - **When:** They type `@张` and select "张三 (Engineering)" from the autocomplete.
  - **Then:** The inline text shows an Avatar Chip: a muted-background pill containing a circular avatar with "张" initials and the label "@张三". The chip visually stands out from surrounding plain text.

- AE2. **Agent mention in readonly content.**
  - **Given:** An issue description contains `@ReviewerBot` (an agent mention).
  - **When:** A reader views the issue description in readonly mode.
  - **Then:** The agent mention renders as a brand-tinted Avatar Chip with a circular Bot icon avatar and "@ReviewerBot" label. The chip is visually distinct from member mentions (different background color) and from plain text.

- AE3. **Squad mention hover.**
  - **Given:** A comment contains a squad mention `@设计组`.
  - **When:** A user hovers over the squad mention chip.
  - **Then:** The chip background transitions to a deeper tint (cursor remains `default` — no click navigation), and a MentionHoverCard popup appears showing the squad's Users icon avatar, name "设计组", and "Squad" type indicator.

- AE4. **Mixed mention types in one sentence.**
  - **Given:** A comment reads: "Assigned to @张三, reviewed by @ReviewerBot, and the @设计组 team is notified."
  - **When:** Rendered in any context (editor or readonly).
  - **Then:** Three Avatar Chips appear with distinct background tints: muted (member), brand (agent), info (squad). All three share the same pill shape, avatar size, and height. The visual distinction allows the reader to identify mention types at a glance without hovering.

- AE5. **Long name truncation.**
  - **Given:** A member has a long display name (e.g., "Alexander Christopherson").
  - **When:** Their mention is rendered in a narrow container or inline context.
  - **Then:** The chip truncates the name with an ellipsis, respecting a max-width cap. The avatar and `@` prefix remain visible.

- AE6. **Mobile parity within platform constraints.**
  - **Given:** A comment with member and agent mentions is viewed in the mobile app.
  - **When:** The user opens the mention autocomplete (suggestion bar) while composing, and later views the saved comment in readonly mode.
  - **Then:** The suggestion bar shows each mention row with the chip's per-type tint (member=`bg-muted`, agent=`bg-brand/10`, squad=`bg-info/10`, @all=`bg-warning/10`) and the mobile `ActorAvatar`, matching the web chip coloring. In readonly mode the mentions render as brand-colored links (not chips) — this is the documented platform limit (`react-native-enriched-markdown` has no custom renderer), tracked as deferred follow-up, not a regression. Editor composition remains plain-text `@name`.

---

## Product Contract preservation

Changed during deepening (2026-07-10): the brainstorm framed mobile parity as "chips match the web spec in readonly mode"; verification during planning confirmed that is infeasible — mobile readonly uses `react-native-enriched-markdown`, a native renderer with no custom-React-renderer hook (`apps/mobile/lib/markdown/markdown.tsx:15-20`), and mobile editor composition is plain-text (TextInput sentinel, `apps/mobile/lib/mention-serialize.ts:3-5`). The following were reframed to reflect the platform constraint, all consistent with each other:

- **R11** and **AE6** (Product Contract): reframed as semantic/functional parity within platform constraints, with mobile readonly chips deferred to a renderer-capable follow-up. Product intent (mobile mentions should align with web where the platform allows) is unchanged; only the infeasible "readonly chip on mobile" framing was corrected. Confirmed with the user.
- **Summary** and **Key Decision #5** (Product Contract): updated to state mobile is scoped to suggestion-bar preview tint alignment, with readonly chips explicitly deferred, matching R11.
- **KTD5** and **System-Wide Impact mobile bullet** (Planning Contract): updated to defer the standalone mobile chip and readonly rendering, matching U5/R11.
- **Scope Boundaries** (Product Contract): extended — added "Mobile readonly mention chips" to "Deferred for later" and "Mobile activity rows" to "Outside this scope".

All other R-IDs (R1–R10, R12–R14), AE-IDs (AE1–AE5), Key Decisions (other than #5), and Visualizations remain carried forward verbatim from the requirements-only brainstorm.

---

## Planning Contract

### Key Technical Decisions

- **KTD1: ActorMentionChip placement — shared `packages/ui/components/common/`.** The chip sits alongside ActorAvatar in the shared UI package (not inside `packages/views/editor/` or `packages/views/issues/`). This is necessary because both MentionView (editor) and ReadonlyContent (markdown renderer) need it, and mobile needs a NativeWind equivalent that mirrors the same design spec. Placing it in the editor package would make it inaccessible to readonly and mobile. IssueChip lives in `packages/views/issues/components/` because it is issue-domain-specific; ActorMentionChip is cross-domain (members, agents, squads, @all) and belongs in the shared UI surface.

- **KTD2: Data sourcing — derive from `mention://` URL and label, no extra API calls.** ReadonlyContent only has the mention URL (`mention://member/{id}`) and the label text when rendering. The chip needs: actor type (from URL scheme), display name (from label/children), and initials (first character of label). `avatarUrl` stays `null` — ActorAvatar gracefully falls back to initials/icons when no URL is provided. This means no additional data fetching for readonly rendering; the chip works with data already available at render time. Editor MentionView gets the same data from Tiptap node attrs (`type`, `id`, `label`).

- **KTD3: Hover card data — pass what's available, skip role/lookups.** MentionHoverCard accepts `role` and `avatarUrl` props, both optional. For the chip integration, we pass `type`, `id`, `name` (label), `initials` (derived), and `avatarUrl=null`. The hover card will show the ActorAvatar (with the appropriate type icon) and the name, which is sufficient for the "this mention is alive" confirmation. The `id` is needed for potential future role/avatar lookup. Role lookup from workspace members API would add complexity without proportional value at this stage — it can be a follow-up.

- **KTD4: @all icon — reuse existing `Users` icon.** The mention:// URL uses type "all" for @all mentions. ActorAvatar doesn't have a dedicated "all" rendering mode (it handles member/agent/system/squad). Rather than extending ActorAvatar with a new mode, the AvatarChip renders @all with the `Users` icon in a warning-tinted circle (`bg-warning/10`). Note: the existing MentionHoverCard's @all branch currently uses `bg-primary/10` tokens — this should also be updated to `bg-warning/10` for visual consistency with the chip, or the chip integration should use a simpler tooltip matching the warning aesthetic.

- **KTD5: Mobile scope limited by platform constraints — suggestion-bar tint alignment only, chip deferred.** Mobile (`apps/mobile/`) uses React Native with NativeWind. Inline chips on mobile are blocked by two platform limits: readonly markdown (`react-native-enriched-markdown`) has no custom-React-renderer hook, and editor composition uses TextInput with a sentinel pattern that can only host plain text. This scope's mobile work is suggestion-bar preview tint alignment only; the standalone mobile chip component and readonly chip rendering are deferred to a renderer-capable follow-up (see U5, R11, Scope Boundaries).

### System-Wide Impact

- **Users (web + desktop):** See avatar chips instead of plain text in all mention contexts (editor, readonly comments, issue descriptions, activity feed). The change is purely visual — no behavior changes, no new navigation paths.
- **Users (mobile):** In-scope mobile work is suggestion-bar preview tint alignment (matching the web chip's per-type coloring). Mobile readonly mention chips are deferred to a renderer-capable follow-up (see U5, R11); the gap is documented, not a regression.
- **Developers:** New `ActorMentionChip` component in `packages/ui` becomes the canonical way to render an actor mention. Future code that displays mentions (e.g., in chat, notifications) should use this component rather than the old `.mention` class.
- **Design system:** The `.mention` CSS class in `prose.css` becomes legacy — still present for backward compatibility during the transition, but new code uses `ActorMentionChip`. Plan to remove in a future cleanup once all rendering paths are migrated.

---

## Implementation Units

### U1. Build ActorMentionChip component

- **Goal:** Create a reusable `ActorMentionChip` component that renders a type-aware avatar pill for actor mentions.
- **Requirements:** R1, R2, R3, R4, R5, R6, R7
- **Dependencies:** None (foundational unit).
- **Files:**
  - Create: `packages/ui/components/common/actor-mention-chip.tsx`
  - Test: `packages/ui/components/common/actor-mention-chip.test.tsx`
- **Approach:**
  - Accept props: `type` (`"member" | "agent" | "squad" | "all"`), `label` (string), `initials` (string, derived from label's first character), `avatarUrl` (optional), `className` (optional extra classes for callers to layer interaction hints).
  - Deleted/unresolvable entity fallback is **not** in this scope: per KTD2 the chip makes no data request, so no render context can detect a deletion today. Do not add a `fallbackLabel` prop or a fallback branch now — revisit only when the KTD3-deferred entity-resolution work lands and supplies a real deletion signal (then add the prop, a requirement, an AE, and a test together).
  - Render a pill container: `inline-flex items-center gap-1 rounded-full border px-1.5 py-0.5 text-xs font-medium`.
  - Inside: `ActorAvatar` at `size={14}` with the appropriate boolean flags (`isAgent={type === 'agent'}`, `isSquad={type === 'squad'}`), followed by `@{label}` text.
  - For type `"all"`: render a `Users` icon (from `lucide-react`) in a `warning`-tinted circle instead of ActorAvatar, since ActorAvatar doesn't have an "all" mode.
  - Add screen reader semantics: `role="mark"` with `aria-label` including type context (e.g., `aria-label="Mention: 张三, member"`, `aria-label="Mention: ReviewerBot, agent"`, `aria-label="Mention: all workspace members"` for @all).
  - Focusability is caller-controlled (R14): render `tabIndex={0}` and `focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50` only when the caller opts in (the editor U2 enables it; readonly U3 renders the chip non-focusable so it adds no keyboard tab stop). The `aria-label` is always present regardless of focusability.
  - Apply type-specific background and border via `cn()`:
    - member: `bg-muted border-border`
    - agent: `bg-brand/10 border-brand/20`
    - squad: `bg-info/10 border-info/20`
    - all: `bg-warning/10 border-warning/20`
  - Truncation: wrap the label in a `truncate` span with `max-w-[8rem]` cap (matching IssueChip's `max-w-full` approach but with a tighter cap since actor names in inline text shouldn't dominate).
  - Height budget: `py-0.5` (4px top+bottom) + `text-xs` (12px) + 2× border (2px) = 22px total, matching IssueChip's proven 22px fit in the 22.75px line box.
- **Patterns to follow:**
  - `packages/views/issues/components/issue-chip.tsx` — the `BASE_CLASS` pattern, `inline-flex min-w-0 max-w-full items-center gap-1.5 rounded-md border mx-0.5 px-2 py-0.5 text-xs`, and the docstring noting the 14px line-box budget constraint.
  - `packages/ui/components/common/actor-avatar.tsx` — the `isSquad ? "rounded-md" : "rounded-full"` shape logic and the icon rendering pattern.
- **Test scenarios:**
  - Renders member chip with initials avatar, muted background, `@Name` label.
  - Renders agent chip with Bot icon, brand-tinted background.
  - Renders squad chip with Users icon, rounded-square avatar shape, info-tinted background.
  - Renders @all chip with Users icon, warning-tinted background.
  - Long label truncates with ellipsis at max-width cap.
  - Avatar and @ prefix remain visible when label is truncated.
  - Extra `className` prop is merged (callers can layer `hover:bg-accent`, `focus-visible:*` styles, etc.).
- **Verification:** All test scenarios pass. Component renders correctly in isolation Storybook or test renderer. Height measures ≤ 22px.

---

### U2. Integrate chip in editor MentionView

- **Goal:** Replace the plain `<span className="mention">` in MentionView with `ActorMentionChip`, and wrap it with `MentionHoverCard` for hover interaction.
- **Requirements:** R8, R12, R13, R14. Covers AE1, AE3.
- **Dependencies:** U1 (ActorMentionChip must exist).
- **Files:**
  - Modify: `packages/views/editor/extensions/mention-view.tsx`
  - Modify: `packages/ui/components/common/mention-hover-card.tsx` (recolor the @all branch — already described in Approach below)
  - Test: `packages/views/editor/extensions/mention-view.test.tsx` (new or extend existing)
- **Approach:**
  - Import `ActorMentionChip` from `@multica/ui/components/common/actor-mention-chip` and `MentionHoverCard` from `@multica/ui/components/common/mention-hover-card`.
  - In the default branch of `MentionView` (currently renders `<span className="mention">@{label}</span>` for member/agent/squad/all types):
    - Derive `initials` from `label` (first character).
    - Render `<ActorMentionChip type={type} label={label} initials={initials} className="cursor-default transition-colors" />`.
    - Wrap the chip in `<MentionHoverCard type={type} id={id} name={label} initials={initials}>` to provide hover popup.
  - Add type-specific hover classes to the chip via `className`: `hover:bg-accent` for members, `hover:bg-brand/15` for agents, etc. (For members the base is `bg-muted`, so `hover:bg-accent` is the deeper tint that R12 specifies; `hover:bg-muted` would be a no-op because it equals the base.)
  - Also update `MentionHoverCard`'s @all branch (`packages/ui/components/common/mention-hover-card.tsx`): change `bg-primary/10` to `bg-warning/10` and `text-primary` to `text-warning` to match the @all chip's warning-tint spec. This is a one-line fix that aligns the hover card's visual language with the chip.
  - In `mention-hover-card.tsx`, also add `isSquad={type === 'squad'}` to the ActorAvatar in the non-'all' branch so squad mentions render a **rounded-square** avatar in the hover popup (matching the chip per R2/R13). Currently only `isAgent` is passed, so squad hovers show a circular avatar — a visible shape mismatch between the chip and its popup for every squad mention.
  - Make the editor chip keyboard-accessible per R14: render it focusable (`tabIndex={0}`) with `focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50`, and confirm MentionHoverCard opens on keyboard focus (add an `onFocus`/focus open trigger if it currently opens on mouse hover only), so Tab users get the same identity popup as mouse users.
  - Keep the existing `NodeViewWrapper as="span" className="inline"` wrapper — it handles Tiptap's inline node view positioning.
- **Patterns to follow:**
  - The existing `IssueMention` component in the same file — shows how `NodeViewWrapper` wraps a chip + interaction layer.
  - The existing `ProjectMention` — shows how `hover:bg-accent transition-colors` is layered via `className` on the chip.
- **Test scenarios:**
  - Member mention in editor renders as AvatarChip (not plain `.mention` span).
  - Agent mention renders with brand-tinted background and Bot icon.
  - Hovering the chip triggers MentionHoverCard popup showing name and avatar.
  - Chip receives focus styling on keyboard Tab navigation, and focusing it opens the MentionHoverCard (parity with hover).
  - Chip renders inside NodeViewWrapper with correct inline positioning.
- **Verification:** Editor renders chips for all actor types. Hover shows popup. Existing issue/project mention behavior unchanged. `pnpm typecheck` passes.

---

### U3. Integrate chip in readonly ReadonlyContent

- **Goal:** Replace the plain `<span className="mention">` fallback in ReadonlyContent with `ActorMentionChip` wrapped in `MentionHoverCard`.
- **Requirements:** R8, R9, R10, R12, R13, R14. Covers AE2, AE4.
- **Dependencies:** U1.
- **Files:**
  - Modify: `packages/views/editor/readonly-content.tsx`
  - Test: `packages/views/editor/readonly-content.test.tsx` (extend existing)
- **Approach:**
  - In the `isMentionHref` handler (around line 267-268), replace the current `return <span className="mention">{children}</span>` for member/agent/squad/all mentions.
  - Extract the actor type from `match[1]` — the capture regex at `readonly-content.tsx:248` currently matches `"member|agent|issue|project|all"` and is missing `squad`. **Add `squad` to the capture regex as a required first step** (the URL scheme already supports `mention://squad/{id}`). Note: the detection gate is the capture regex **only** — `isMentionHref` in `packages/views/editor/utils/link-handler.ts` is a type-agnostic prefix check (`href.startsWith("mention://")`) and already accepts `mention://squad/*` without modification.
  - Extract the actor ID from `match[2]` — needed by MentionHoverCard for potential future entity lookup.
  - Extract the label from `children` (same pattern as `IssueMentionLink` — check if children is a string or array, join if array).
  - Derive `initials` from the label's first character.
  - Render `<ActorMentionChip type={type} label={label} initials={initials} className="transition-colors" />` wrapped in `<MentionHoverCard type={type} id={id} name={label} initials={initials}>`. Pass hover classes via the chip's className prop, matching U2's approach (type-specific hover tints).
  - Render the readonly chip **non-focusable** (no `tabIndex`, no focus-visible styling) so a long comment with N mentions does not inject N keyboard tab stops; the chip's `aria-label` (from U1) conveys identity to screen readers (R14, reframed for readonly).
- **Patterns to follow:**
  - The existing `IssueMentionLink` and `ProjectMentionLink` components in the same file — they show how to extract label from children and handle the mention URL.
- **Test scenarios:**
  - Readonly markdown with `@member` renders as AvatarChip (not plain `.mention` span).
  - Readonly markdown with agent mention renders with brand tint.
  - Readonly markdown with squad mention renders with info tint and square avatar.
  - Mixed mentions in one paragraph (member + agent + squad) all render as correctly-tinted chips. Covers AE4.
  - Readonly chip adds no keyboard tab stop (non-focusable); screen readers read the `aria-label`.
  - Existing issue/project mention rendering unchanged.
- **Verification:** All existing readonly content tests pass. New chip rendering visible in Storybook or test renderer for all actor types.

---

### U4. Update prose.css and verify inline layout

- **Goal:** Ensure the prose CSS supports the new chip layout within the line-box budget, and remove or deprecate the legacy `.mention` class rules.
- **Requirements:** R5, R7. Covers AE5.
- **Dependencies:** U1, U2, U3 (chips must be rendering before layout can be verified).
- **Files:**
  - Modify: `packages/views/editor/styles/prose.css` (lines 361-366: `.rich-text-editor .mention` rules)
  - Modify: `packages/views/editor/styles/shell.css` (verify `[data-node-view-wrapper]` vertical-align works with chip height)
- **Approach:**
  - The existing `.mention` CSS rule (`color: var(--primary); font-weight: 600; text-decoration: none; margin: 0 0.125rem`) becomes legacy — it still applies to any remaining plain-text mentions during transition, but new code uses the chip component which has its own styling.
  - Add a comment marking `.mention` as deprecated with a note to remove after all rendering paths are migrated.
  - Verify that the chip's 22px height fits within the paragraph line box (14px × 1.625 = 22.75px). If the chip causes line-height expansion, adjust `vertical-align: middle` on the `[data-node-view-wrapper]` or add `leading-none` to the chip container.
  - Ensure the chip's `rounded-full` border-radius renders correctly inside the Tiptap editor's NodeView wrapper (which uses `display: inline`).
  - Verify dark mode: all type-specific background tints (`bg-muted`, `bg-brand/10`, `bg-info/10`, `bg-warning/10`) resolve correctly through CSS custom properties in dark theme.
- **Test scenarios:**
  - Chip in editor does not expand the line box beyond 22.75px (visual regression or measurement test).
  - Dark mode: all four type tints render with appropriate contrast (no washed-out text on tinted background).
  - Legacy `.mention` class still renders (backward compat) but new code paths use the chip.
- **Verification:** Visual check in browser at 100% zoom — chips align with surrounding text without line-height jitter. Dark mode toggle shows correct tint resolution. `pnpm typecheck` passes.

---

### U5. Mobile mention alignment within platform constraints

- **Goal:** Align mobile mention surfaces to the new chip aesthetic where the platform allows, and decisively defer mobile readonly chips to a renderer-capable follow-up.
- **Requirements:** R11 (reframed — semantic/functional parity within platform constraints). Covers AE6 (reframed).
- **Dependencies:** U1 (web chip spec is the parity reference).
- **Files:**
  - Modify: `apps/mobile/components/issue/mention-suggestion-bar.tsx` (verify/align per-type tint coloring to the chip spec)
  - Test: `apps/mobile/components/issue/mention-suggestion-bar.test.tsx` (extend existing or create)
  - Read (context only, edit only if gaps found): `apps/mobile/CLAUDE.md`, `apps/mobile/lib/markdown/markdown.tsx`, `apps/mobile/lib/mention-serialize.ts`, `apps/mobile/components/ui/actor-avatar.tsx`
- **Approach:**
  - Run the mandatory mobile pre-flight from `apps/mobile/CLAUDE.md` before any code change: read the real web/desktop implementation (U1–U3), list parity points (counts, enums, permissions, visual tint mapping), present an interactive plan + parity checklist (≤30s read, web vs. simulator screenshots if useful), and wait for an explicit "go" instruction. Read-only investigation is exempt from this gate.
  - Respect mobile import limits: only `import type` from `@multica/core` plus pure functions; never import `packages/views` or `packages/ui`. "Mirror, don't import." Mobile cannot reuse the web `ActorMentionChip`.
  - **Readonly chips are not achievable in this scope.** Mobile readonly markdown renders through `react-native-enriched-markdown` (`apps/mobile/lib/markdown/markdown.tsx`), a native attributed-string renderer with no custom-React-renderer hook — the library's prop surface (`markdown`, `markdownStyle`, `onLinkPress`, …, plus `...rest: ViewProps`) and the code comment at `markdown.tsx:15-20` confirm "no custom renderers, by design." Mentions render as brand-colored underlined links, not chips. Inline chips during composition are likewise impossible (TextInput sentinel, `apps/mobile/lib/mention-serialize.ts:3-5`). These are platform limits, not scope choices — see the Deferred entry in Scope Boundaries.
  - **In-scope work is narrow.** The suggestion bar (`mention-suggestion-bar.tsx`) is the one React-renderable mention surface and already composes the mobile `ActorAvatar`. Verify/align its per-type background tints to the chip spec (member=`bg-muted`, agent=`bg-brand/10`, squad=`bg-info/10`, @all=`bg-warning/10`) so the autocomplete preview matches what the web editor renders. Reuse the existing mobile `ActorAvatar` — do NOT add a new component primitive (`apps/mobile/CLAUDE.md` Principle 1 / Lesson 7). All these tokens already exist in the mobile NativeWind theme, so no theme work is required.
  - Activity rows render via `formatActivity` plain text (`apps/mobile/components/issue/activity-row.tsx`), not the markdown pipeline — unaffected by this change.
- **Patterns to follow:** Mobile `ActorAvatar` and the suggestion bar's existing NativeWind tint usage; the web chip spec from U1 as the parity reference.
- **Test scenarios:**
  - Suggestion bar renders the correct per-type tint for member, agent, squad, and @all rows, matching the web chip spec.
  - @all row shows the warning tint.
  - No readonly/editor inline-chip tests — those are platform-impossible in this scope (covered by the Deferred entry), and asserting them would be a false test.
- **Verification:** Suggestion bar preview matches web chip coloring per type. `apps/mobile` type check passes. Mobile readonly remains brand-colored links (gap documented as deferred, not a regression).

---

## Verification Contract

| Gate | Command | Applies to | Done when |
|---|---|---|---|
| Type check | `pnpm typecheck` | U1–U5 | Zero type errors across all packages |
| Unit tests | `pnpm test` | U1, U2, U3, U5 | All new test scenarios pass |
| Visual regression | Manual browser check at 100% zoom | U1–U4 | Chips align with text, no line-height jitter, dark mode correct |
| Mobile parity | Mobile app + web side-by-side | U5 | Suggestion-bar per-type tints match web chip spec; readonly gap documented as deferred |
| Existing tests | `pnpm test` (full suite) | U1–U5 | No regressions in existing mention, editor, or readonly tests |

## Definition of Done

- All 14 requirements (R1–R14) are satisfied across web, desktop, and mobile (within the platform constraints noted in R11/U5).
- All 6 acceptance examples (AE1–AE6) pass manual or automated verification (AE6 verified within platform constraints).
- `ActorMentionChip` is exported from `packages/ui/components/common/` and usable by any consumer.
- MentionView and ReadonlyContent both render Avatar Chips for all actor types (member, agent, squad, @all).
- Hover card triggers on chip hover for all actor types.
- Legacy `.mention` CSS class is marked deprecated but still functional (no broken rendering for any edge cases).
- Mobile mention surfaces aligned within platform constraints: suggestion-bar preview matches the chip's per-type coloring; mobile readonly chips documented as deferred (platform limit), not a regression.
- `pnpm typecheck` and `pnpm test` pass with zero errors.
- Dark mode verified for all four type-specific tints.
