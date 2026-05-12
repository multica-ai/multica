---
date: 2026-05-12
topic: issue-identifier-visibility
focus: multica 的 issue 详情页面没有显示当前 issue 的编号，导致我无法快速得知我位于哪一个 issue
mode: repo-grounded
---

# Ideation: Issue Identifier Visibility on Detail Page

## Grounding Context

- Multica: AI-native task management platform (Linear-like with AI agents)
- Go backend + TS monorepo (Next.js web + Electron desktop)
- Issue detail page: `packages/views/issues/components/issue-detail.tsx`
- Issue identifier currently shown in PageHeader breadcrumb (line 708-710) as muted text
- Format: `MUL-123` style human-readable identifiers
- List view shows identifier prominently in fixed-width column
- `IssueChip` component shows `<StatusIcon> <identifier> <title>`

### Current State
The issue identifier IS displayed but:
- In breadcrumb trail alongside workspace/parent links
- Styled as `text-muted-foreground tabular-nums` (small, gray)
- Easy to miss when scanning the page
- No copy/share action attached
- No keyboard shortcut for copying ID

### External Context
- **Linear**: ID prominently displayed near title; Cmd+. to copy ID; Cmd+Shift+. to copy branch name
- **GitHub**: "Title · #123" format in header; sticky header
- **Jira**: ID in breadcrumb; prominent in header with issue type icon
- **GitLab**: Copy Reference in ⋮ menu; `c`+`r` shortcut
- **YouTrack**: Cmd+C overrides to copy issue ID; most comprehensive shortcuts

## Topic Axes

1. Visual hierarchy — Size, color, typography, positioning
2. Navigation context — Breadcrumb vs standalone element
3. Interactivity — Copy, share, click actions
4. Information architecture — ID relation to title, status, actions

## Ranked Ideas

### 1. Prominent Identifier Badge with Copy Action
**Description:** Render the identifier as a distinct visual badge (pill/chip) with subtle background, positioned adjacent to the title. Click copies to clipboard with toast confirmation. Separates identity from navigation.
**Axis:** Visual hierarchy
**Basis:** `direct:` `packages/views/issues/components/issue-detail.tsx` lines 708-710 — the identifier is rendered as `text-muted-foreground tabular-nums` inside a crowded breadcrumb trail. `external:` Linear renders the ID as a visually distinct element near the title, not inline with breadcrumbs.
**Rationale:** The simplest, most direct fix. The current styling signals "this is unimportant" but the identifier is often the primary reference token users need.
**Downsides:** Slight increase in header visual weight; need to ensure it doesn't compete with status/priority indicators.
**Confidence:** 95%
**Complexity:** Low
**Status:** Explored

### 2. Fused Title-Identifier Display
**Description:** Render "MUL-123 · Fix login bug" as a single typographic unit in the title area, with identifier in different weight/color but same size. Breadcrumb drops identifier, shows only workspace/parent navigation.
**Axis:** Information architecture
**Basis:** `external:` GitHub renders issue titles as "Fix login bug · #2345" in page headers. `reasoned:` The identifier and title together constitute "the name of this issue." Separating them forces users to scan two locations.
**Rationale:** Eliminates the "where is the ID?" problem entirely by making it inseparable from the title.
**Downsides:** Affects TitleEditor component; non-editable prefix needs careful implementation to not confuse users.
**Confidence:** 85%
**Complexity:** Medium
**Status:** Unexplored

### 3. Global Keyboard Shortcut (Cmd+. / Ctrl+.)
**Description:** Register a global keyboard shortcut within the issue detail context that copies the identifier to clipboard. Show shortcut hint in tooltip. Matches Linear muscle memory.
**Axis:** Interactivity
**Basis:** `external:` Linear's Cmd+. shortcut for copying issue ID is well-established muscle memory. `direct:` No page-level keyboard shortcut infrastructure exists in `packages/views/` or `packages/core/`.
**Rationale:** Power users reference issues in Slack/commits dozens of times per day. A shortcut removes the "find and click" friction entirely.
**Downsides:** Requires establishing keyboard shortcut infrastructure; shortcut discovery for new users.
**Confidence:** 80%
**Complexity:** Low-Medium
**Status:** Unexplored

### 4. Split Navigation and Identity Header
**Description:** Split PageHeader into two bands: top nav bar (workspace -> parent) and bottom identity bar (identifier badge + title + status). Identifier becomes leftmost, prominent element.
**Axis:** Navigation context
**Basis:** `external:` Jira uses a two-tier header: breadcrumb/navigation above, issue key + title + actions below. `direct:` `issue-detail.tsx:684-793` crams workspace link, parent link, identifier, title, and 4-5 action buttons into a single 48px `PageHeader`.
**Rationale:** Clean separation of wayfinding from identity. Gives both jobs room to breathe and future-proofs for richer headers.
**Downsides:** Increases header height; larger visual change that may feel excessive on small screens.
**Confidence:** 75%
**Complexity:** Medium
**Status:** Unexplored

### 5. Reusable IssueIdentityBar Component
**Description:** Extract identifier + title + status + copy action into a shared `IssueIdentityBar` component in `packages/views/issues/components/`. Use across detail page, modals, sidebar peek panels.
**Axis:** Visual hierarchy
**Basis:** `direct:` `issue-detail.tsx` is 1215 lines and embeds all header logic inline. `issue-chip.tsx` exists as a compact representation but isn't used in the detail header. No shared "issue header" component exists.
**Rationale:** Compounding move — every future surface that shows an issue gets the same prominent identifier treatment for free.
**Downsides:** Refactoring effort; needs compact vs full variants for different contexts.
**Confidence:** 85%
**Complexity:** Medium
**Status:** Unexplored

### 6. Identifier in Document/Window Title
**Description:** Set `document.title` to "MUL-123 — Issue Title — Workspace" on issue detail load. Makes identifier visible in browser tabs, window switchers, bookmarks.
**Axis:** Navigation context
**Basis:** `external:` Linear sets tab title to "ENG-123 Issue Title". GitHub uses "#2345 Issue Title". `reasoned:` Users with multiple tabs open can instantly find the right issue by scanning tab titles.
**Rationale:** Zero UI footprint on the page itself. Solves the identification problem in the context where it often actually occurs (Alt-Tab, tab bar).
**Downsides:** Desktop app window title API may differ from web; Next.js App Router metadata patterns need verification.
**Confidence:** 90%
**Complexity:** Low
**Status:** Unexplored

## Rejection Summary

| # | Idea | Reason Rejected |
|---|------|-----------------|
| 1 | Hover-reveal copy button on title | Duplicates click-to-copy with lower discoverability |
| 2 | Invert breadcrumb order (ID as root) | Violates breadcrumb convention (general->specific) |
| 3 | Auto-copy when selecting title | Surprises users; violates selection expectations |
| 4 | User-configurable display mode | Over-engineered for stated problem; adds settings burden |
| 5 | Smart paste auto-expand in editor | Scope overrun — about comment editor, not detail page visibility |
| 6 | Clickable ID in list/board views | Scope overrun — about other views, not detail page |
| 7 | Command palette trigger for ID | Too expensive relative to value for this specific complaint |
| 8 | Identifier mandatory in all content | Scope overrun — touches entire content generation system |
| 9 | Persistent floating badge | Visually intrusive; addresses scrolling, not initial visibility |
| 10 | Copy branch name in actions | Good idea but about developer workflow, not visibility |
| 11 | Remove ID to metadata bar | Duplicated by stronger "Split header" idea |

## Axis Coverage

- Visual hierarchy: 2 survivors (Prominent badge, Shared component)
- Navigation context: 2 survivors (Split header, Document title)
- Interactivity: 1 survivor (Keyboard shortcut)
- Information architecture: 1 survivor (Fused title-ID)

All 4 axes covered. No gaps.
