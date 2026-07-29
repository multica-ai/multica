# Decision: Name and enforce a page pattern layer between primitives and pages

Status: proposed

## Problem

The interface is inconsistent, and the cause is not missing tokens or missing components. `packages/ui/styles/` supplies colors, surfaces, and spacing. `packages/ui/components/` supplies Button, Input, Card, Tabs, and Empty. Both are largely complete.

What is missing is the layer between them and the business pages. Tokens and primitives guarantee that two things look alike; they do not answer where a list page's title, count, description, and primary action go, whether a settings page uses cards or flat sections, whether secondary navigation is horizontal or vertical, or whether Empty, Loading, Error, and Not Found share a size and a voice. Without standard answers, every page re-decides, and the result is that the components match while the experience does not.

Early pieces of this layer exist — `CollectionPageHeader`, `CollectionPageState`, `SettingsTab`, `SettingsSection`, `SettingsCard`, `SettingsRow` — but they are partial. They do not cover the main page types, they have no stated boundaries, and nothing enforces their use, so a local change easily reintroduces a parallel implementation.

Custom Args shows the shape of the problem concretely. Every argument renders as a permanent Input separated by a divider, so adding one adds another input box and another line. At rest the data should read as a list; editing should be a temporary state; adding should append to the list rather than grow the form. That is not a styling bug, it is a missing Editable List pattern.

## Proposal

Fix five patterns as named, documented, enforced page types.

**Collection Page** — for Agents, Projects, Skills, Runtimes, Squads, Autopilots. Standardizes the header's icon, title, count, description, and actions; container padding and scrolling; the Empty, Loading, Error, and No Results states; how the primary action collapses on small screens; and where filter, search, and view switching sit. `packages/views/layout/collection-page.tsx` is the base; the work is coverage and documentation, not a second component set.

**Settings Page** — the `SettingsTab → SettingsSection → SettingsCard → SettingsRow` hierarchy. One setting per row by default, with a compound control requiring a stated reason. Consistent label, description, and control widths. Defined feedback for both auto-save and explicit-save, including dirty state, leave protection, and the read-only presentation when permission is missing.

**Workbench Detail Page** — for Agent, Runtime, Squad, and Member, which each carry identity, state, history, and configuration. An identity header, a first-level nav of Overview / Work / Capabilities / Settings, and a vertical section nav at the second level once there are more than two or three entries. Overview carries only what supports a judgment or an action, not every field. Settings holds editable configuration only; read-only metadata belongs on Overview. Tab and section state must be in the URL. No invented health or progress metrics.

**Editable List** — for Custom Args, environment variables, webhook headers, repository rules, token lists. At rest it is a list. Add opens one composer; Edit puts one item into edit state. Enter commits, Escape cancels, focus moves to the input. Delete updates the local draft and persistence follows the page's save strategy. Long values truncate, wrap, or expand. The layout, state, and action slots are shared; validation, field structure, and secret handling stay in the domain layer.

**Page State** — every page distinguishes initial loading, background refreshing, empty, no search results, permission denied, not found, recoverable error, and fatal error, reusing one size, voice, and action placement without collapsing different causes into one message.

Ownership follows a single rule: atoms with no business meaning in `packages/ui/`; cross-domain page patterns in `packages/views/layout/`, defining layout, slots, and interaction contract only; domain-shared patterns in `packages/views/<domain>/`; Next.js and Electron wiring in the app platform layer, never in shared views.

Before extracting anything, ask whether it expresses a visual detail or a stable page semantic, who the second real caller is, whether extracting removes repeated spacing and divider decisions from business pages, and whether slots can carry the differences instead of a row of boolean props.

Enforcement is what makes the difference between this and the current partial state: a page pattern entry in the PR template, a declared pattern for every new page with deviations explained, canonical examples with light and dark plus CJK visual coverage, and removal of parallel implementations as pages migrate.

## Alternatives considered

**Keep fixing pages individually.** Rejected. It is the current approach, and each fix is local while the divergence is structural, so the pages drift back apart at the rate new ones are written.

**Promote the Agent detail page's components into the global abstraction.** Rejected. That page is the first pilot of the Workbench Detail pattern, not a finished generalization. Runtime, Squad, and Member need to be examined for what they genuinely share — most likely the shell, header, navigation, and section layout — before a minimal common API is defined. Extracting one page's business components as the standard would encode its specifics as everyone's constraints.

**Write a visual specification instead of components.** Rejected. `docs/design.md` already specifies color, type, surface, spacing, and interaction, and the inconsistency persists above that level. A rule that is only prose is re-decided by every page; a component with slots is not.

**Build one universal Editable List component for every case.** Rejected. Custom Args, environment variables, and secret lists differ in validation, field structure, and how sensitive values are handled. Share the layout, state machine, and action slots; leave those three in the domain.

## Acceptance criteria

A new page starts by choosing a pattern and filling in business content, rather than from an empty `<div>`. Pages of the same kind agree on header, navigation, sections, empty and error states, and action placement. Business pages no longer decide base padding, dividers, cards, or save feedback. Pattern components handle keyboard, focus, overflow, responsive layout, and long translated strings. Canonical examples cover light and dark, Chinese and English, desktop and web. Deviations are explicit decisions. No abstraction ships until two real pages use it.

## Risks

Four questions are open and should be settled before the components they affect are extracted.

Whether Agent Overview's Recent Work is measured in issues or in sessions. Issues match how users think about delivered work; sessions are the right unit for diagnosing an execution. Mixing both granularities in one flat list serves neither; the likely answer is issue-level rows with the latest session status as inline metadata, and full session history on the Work tab.

Whether the Settings pattern stays in `packages/views/settings/components/settings-layout.tsx` or moves to `packages/views/layout/`. Agent Settings already reuses it, so its location no longer matches its scope. Until that is settled, do not create a second Settings layout.

Where Card ends and a flat section begins. The container priority in [`docs/design.md`](../../../design.md) — spacing, then a single divider, then a background change, then a Card — still governs. Cards suit an independent settings group or a block acted on as a whole; continuous reading content, metadata lists, and ordinary lists should not put each item in its own card, and cards should not nest borders inside borders.

What the Workbench Detail pattern's minimal common part actually is, and whether it includes the inspector or stops at the shell.

The main risk in execution is doing this alongside product changes. Each migration should be a small PR that does not change business behavior and global visual rules at the same time.
