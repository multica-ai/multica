# Mobile App Rules

This file contains rules that apply ONLY to `apps/mobile/`. The repo-root
`CLAUDE.md` still applies — read both.

## Cross-platform parity

**Default: "shared concept, shared algorithm."** When mobile renders any
concept that already exists on web (timeline grouping, coalescing, mention
parsing, reply tree, reaction aggregation, permission checks, etc.), it
must consume the SAME function from `@multica/core` — not a
re-implementation.

When divergence is genuinely needed (mobile-only UX adaptation), it
requires BOTH:

1. Discussion with the user before implementing.
2. An inline comment at the divergence point explaining why mobile
   differs and what behavior is intentionally not matched.

Why: mobile silently re-implemented timeline grouping in 2026-05 and
recursively-nested replies disappeared until a user diff'd two screenshots.
Re-implementations drift; shared functions don't.

How to apply: before writing a transform / aggregation / formatter on
mobile, grep `packages/core/` and `packages/views/` for an existing one.
If the logic exists only in `packages/views/` (entangled with DOM/i18n),
extract the pure part into `packages/core/` (zero `react-dom`, zero
`next/*`, zero i18n) and consume it from both ends rather than copying.

## Acceptable mobile-only adaptations

These are documented divergences from web — they are intentional, not
drift. Each is also annotated in code with the reason at the divergence
point.

- **No Collapsible thread cards** (`components/issue/comment-card.tsx`):
  screen too narrow, collapse is more friction than reward on a phone.
- **No inline ReplyInput per thread**: a single sticky bottom composer
  picks up reply-to via long-press ActionSheet. Thumbs stay at the bottom.
- **No comment Edit / Delete in v1**: feature scoped out, not omitted by
  accident. Will land alongside iOS-native context menu (M2).
- **No "Load older" button**: mobile auto-loads via `useInfiniteQuery`
  scroll. Revisit if long timelines surface UX pain.
- **English-only activity sentences**
  (`components/issue/activity-row.tsx` `formatSentence`): web uses i18n
  via `useT`; mobile v1 doesn't ship i18n yet. The action-coverage list
  MUST stay a superset of web's `formatActivity` — when web adds an
  action, add it here too. Tracked as a parity hazard until mobile i18n
  lands.

## Visual tokens

Mobile transcribes design tokens from `packages/ui/styles/tokens.css`
once into `apps/mobile/tailwind.config.js`. When web tokens change, sync
here. See `_features/ios-mobile/design-system.md` for the broader UI
stack rationale (NativeWind, Base UI substitutes, etc.).

## Other rules

(Add as encountered. Keep this file short — link out to feature docs for
detail.)
