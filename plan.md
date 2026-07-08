# Multica — Master Improvement Plan

> **Generated:** 2026-07-03
> **Status:** 🟡 In Progress
> **Base:** Multica v0.2.0 (AI-native task management platform)
> **References:** [Huly](file:///Users/gaurav/personal/playground/multica/huly/) · [AFFiNE](file:///Users/gaurav/personal/playground/multica/AFFiNE/) · [Plan Docs](file:///Users/gaurav/personal/playground/multica/plan/)

---

## Table of Contents

1. [Executive Summary](#executive-summary)
2. [Current State Assessment](#current-state-assessment)
3. [Problems Identified](#problems-identified)
4. [Phase 0 — Foundation & Quick Wins](#phase-0--foundation--quick-wins)
5. [Phase 1 — Media Review Module (Video + Graphics)](#phase-1--media-review-module-video--graphics)
6. [Phase 1.5 — Advanced Media Review Workflow](#phase-15--advanced-media-review-workflow)
7. [Phase 2 — Marketing & Creative Workflow Features](#phase-2--marketing--creative-workflow-features)
8. [Phase 3 — Rich Text Editor Upgrade](#phase-3--rich-text-editor-upgrade)
9. [Phase 4 — Project Architecture & Access Control](#phase-4--project-architecture--access-control)
10. [Phase 5 — Enhanced GitHub Integration](#phase-5--enhanced-github-integration)
11. [Phase 6 — Communication Layer](#phase-6--communication-layer)
12. [Phase 7 — PWA, Mobile & Cross-Platform Polish](#phase-7--pwa-mobile--cross-platform-polish)
13. [Phase 8 — Dynamic Custom Fields](#phase-8--dynamic-custom-fields)
14. [Phase 9 — Project & Issue Templates](#phase-9--project--issue-templates)
15. [Phase 10 — Autopilot Automation Presets](#phase-10--autopilot-automation-presets)
16. [Phase 11 — Web Performance & "Instant DB" Optimizations](#phase-11--web-performance--instant-db-optimizations)
17. [Reference Architecture Decisions](#reference-architecture-decisions)
18. [Open-Source Libraries & References](#open-source-libraries--references)

---

## Executive Summary

Multica is a powerful AI-native task management platform where AI agents are first-class teammates. It already has a mature issue tracker, agent execution pipeline, autopilots, squads, and integrations with GitHub/Slack/Lark. However, after comparing it with **Huly** (project management) and **AFFiNE** (knowledge management), and analyzing the gap documents in `plan/`, several critical gaps exist:

| Gap Area                                          | Severity  | Phase   |
| ------------------------------------------------- | --------- | ------- |
| Zero video/graphic review & annotation capability | 🔴 High   | Phase 1 |
| No creative/marketing workflow features           | 🔴 High   | Phase 2 |
| Editor lacks rich block-based authoring           | 🔴 High   | Phase 3 |
| No granular project access control (RBAC)         | 🔴 High   | Phase 4 |
| No milestones or project documentation hub        | 🟠 Medium | Phase 4 |
| GitHub integration missing auto-link PR → Issue   | 🟠 Medium | Phase 5 |
| No real-time team chat (beyond task threads)      | 🟠 Medium | Phase 6 |
| Mobile app needs feature parity                   | 🟡 Low    | Phase 7 |

**Strategy:** Fix foundation issues first (Phase 0), then tackle the highest-impact creative features (Phase 1-2), followed by editor and architecture improvements. Each phase is designed to be independently shippable.

---

## Current State Assessment

### ✅ What Multica Already Has (Don't Rebuild)

- **Issue Tracker:** Full CRUD, statuses, priorities, labels, sub-issues, batch ops, search
- **Views:** Board (Kanban), List, Swimlane, Gantt — all 4 views exist
- **Agents as Teammates:** 14 supported agent runtimes, polymorphic assignees
- **Squads:** Agent + human groups with leader delegation
- **Autopilots:** Cron/webhook-triggered recurring agent work
- **Skills:** Reusable agent skills (YAML-based)
- **Comments:** Rich threading, reactions, resolved threads, agent-authored
- **PR Tracking:** Per-issue PR tracking with CI/conflict status
- **Chat:** Real-time chat with agents (tied to runtimes)
- **Inbox:** Notification inbox
- **Desktop App:** Electron with tabs, window overlays
- **Mobile App:** Expo/React Native iOS app (exists but needs polish)
- **CLI:** Full CLI for workspace/issue management
- **Integrations:** GitHub, Slack, Lark, Composio, MCP
- **Self-Hosting:** Docker Compose setup with GHCR images
- **i18n:** English + Chinese

### 🏗️ Tech Stack

| Layer    | Technology                                                         |
| -------- | ------------------------------------------------------------------ |
| Backend  | Go 1.26.1, Chi v5, PostgreSQL 17, sqlc, Redis, gorilla/websocket   |
| Frontend | React 19, Next.js 16 (App Router), Tailwind CSS v4, shadcn/Base UI |
| State    | React Query v5 (server), Zustand v5 (client)                       |
| Desktop  | Electron (electron-vite)                                           |
| Mobile   | Expo / React Native                                                |
| Build    | Turborepo, pnpm v10.28 workspaces                                  |
| Testing  | Vitest v4, Playwright, Go testing                                  |

### 📦 Package Boundaries (Must Follow)

- `packages/core/` → zero react-dom, zero localStorage, zero process.env
- `packages/ui/` → zero `@multica/core` imports
- `packages/views/` → zero `next/*`, zero `react-router-dom`, use `NavigationAdapter`
- `apps/web/platform/` → only place for Next.js APIs

---

## Problems Identified

### From `plan/Feature Breakdown by Platform.md`

| #   | Problem                                            | Compared Against                          |
| --- | -------------------------------------------------- | ----------------------------------------- |
| P1  | No creative/marketing communication features       | Huly has chat, voice, video calls         |
| P2  | No graphic/video review & annotation               | Neither Huly nor AFFiNE solve this either |
| P3  | Communication limited to task threads & agent chat | Huly has channels, DMs, threads           |
| P4  | Dev-only terminology in UI                         | N/A                                       |
| P5  | No visual canvas/whiteboard for planning           | AFFiNE has infinite edgeless canvas       |

### From `plan/Multica Gap Analysis and Feature that needed in it.md`

| #   | Gap                        | Details                                                                                          |
| --- | -------------------------- | ------------------------------------------------------------------------------------------------ |
| G1  | **Editor Experience**      | No slash-command palette, no floating format menus, non-technical users can't format efficiently |
| G2  | **Multi-Assignee**         | `assignee_type` + `assignee_id` is singular — can't assign human + AI + marketer to same issue   |
| G3  | **Project Access Control** | No RBAC (Admin/Viewer/Editor per project), no `ProjectMembers` table                             |
| G4  | **No Milestones**          | No milestone entity, no timeline/calendar views for project planning                             |
| G5  | **No Project Wiki/Docs**   | No per-project documentation hub                                                                 |
| G6  | **GitHub Auto-Link**       | PRs mentioning issue IDs (e.g., `Fixes MUL-102`) don't auto-update board state                   |

### From Video/Graphic Review Documents (4 files)

| #   | Technical Requirement                                       | Source File                |
| --- | ----------------------------------------------------------- | -------------------------- |
| V1  | Need `ReviewComment` + `AnnotationShape` data models        | `How can I architect...`   |
| V2  | Normalized coordinates (0.0–1.0) for responsive annotations | `How do I calculate...`    |
| V3  | Canvas overlay with fabric.js for drawing                   | `How can I architect...`   |
| V4  | ResizeObserver + requestAnimationFrame for performance      | `Show me how to set up...` |
| V5  | Pre-signed URL upload for large video files                 | `How can I architect...`   |
| V6  | Open-source refs: OpenFrame, Clapshot, sm-annotate          | `timestamped video...`     |

---

## Phase 0 — Foundation & Quick Wins

> **Goal:** Fix small but impactful issues before larger features.
> **Effort:** 1-2 days
> **Dependencies:** None

### 0.1 Multi-Assignee Support

- [x] **DB Migration:** Add `issue_assignees` junction table
  ```sql
  CREATE TABLE issue_assignees (
    issue_id      UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    assignee_type TEXT NOT NULL CHECK (assignee_type IN ('member', 'agent')),
    assignee_id   UUID NOT NULL,
    role          TEXT NOT NULL DEFAULT 'assignee' CHECK (role IN ('assignee', 'reviewer', 'observer')),
    assigned_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (issue_id, assignee_type, assignee_id)
  );
  ```
- [x] **Backend:** Update sqlc queries in `server/` — add `ListIssueAssignees`, `AddIssueAssignee`, `RemoveIssueAssignee`
- [x] **Backend:** Update issue handlers to support multiple assignees in create/update payloads
- [x] **API Schema:** Update Zod schemas in `packages/core/api/` to accept `assignees[]` array
- [x] **React Query:** Update issue queries/mutations in `packages/core/issues/` to handle assignee arrays
- [x] **UI:** Update assignee picker in `packages/views/issues/` to support multi-select
- [x] **Backward Compat:** Keep `assignee_id` column temporarily, migrate existing data to junction table, deprecate old field

### 0.2 Clean Up Dev-Only Terminology

- [x] Audit all user-facing strings in `packages/views/locales/` for developer jargon
- [x] Replace dev terms with user-friendly alternatives where appropriate (e.g., "Runtime" → "Agent Environment")
- [x] Update i18n keys for both `en` and `zh-CN` locales

---

## Phase 1 — Media Review Module (Video + Graphics)

> **Goal:** Build a frame-accurate video and graphic annotation/review tool integrated into issues.
> **Effort:** 10-15 days (largest phase)
> **Dependencies:** Phase 0 (multi-assignee for reviewer role)
> **Reference:** VideoReview, OpenFrame, Clapshot, and our newly vendored `@multica/canvas-drawing-editor`

### 1.1 Data Model

- [x] **DB Migration:** Create review tables

  ```sql
  -- Assets attached to issues for review
  CREATE TABLE review_assets (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id    UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    asset_type  TEXT NOT NULL CHECK (asset_type IN ('video', 'image')),
    file_url    TEXT NOT NULL,
    thumbnail_url TEXT,
    width       INT,              -- intrinsic width
    height      INT,              -- intrinsic height
    duration    REAL,             -- video duration in seconds (NULL for images)
    version     INT NOT NULL DEFAULT 1,
    status      TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'changes_requested')),
    uploaded_by UUID REFERENCES members(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
  );

  -- Review comments with optional timestamp and annotations
  CREATE TABLE review_comments (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_id    UUID NOT NULL REFERENCES review_assets(id) ON DELETE CASCADE,
    author_id   UUID NOT NULL REFERENCES members(id),
    content     TEXT NOT NULL,
    timestamp   REAL,             -- video timestamp in seconds (NULL for images / general comments)
    shapes      JSONB DEFAULT '[]',  -- array of AnnotationShape objects
    resolved    BOOLEAN NOT NULL DEFAULT false,
    resolved_by UUID REFERENCES members(id),
    resolved_at TIMESTAMPTZ,
    parent_id   UUID REFERENCES review_comments(id),  -- threaded replies
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
  );
  ```

- [x] **AnnotationShape JSONB structure:**
  ```json
  {
    "type": "rectangle|circle|arrow|freehand",
    "x": 0.35,
    "y": 0.2,
    "width": 0.15,
    "height": 0.1,
    "color": "#FF4444",
    "strokeWidth": 2,
    "points": []
  }
  ```
  > **Critical:** All coordinates are normalized (0.0–1.0). Never store pixel values.

### 1.2 Asset Upload

- [x] **Backend:** Pre-signed URL generation endpoint for S3 direct upload (bypass Next.js API routes for large files)
- [x] **Backend:** Upload completion webhook — extract metadata (dimensions, duration via ffprobe or similar)
- [x] **Backend:** Thumbnail generation for videos (extract frame at 1s mark)
- [x] **Core:** `packages/core/reviews/` — queries, mutations, upload hooks
- [x] **UI:** Drag-and-drop upload zone in issue detail page
- [x] **UI:** Upload progress indicator with cancel support

### 1.3 Media Player Component

- [x] **UI:** Create `packages/views/reviews/media-review-player.tsx`
- [x] **Video player:** HTML5 `<video>` with custom controls (play/pause, scrubber, frame step ←/→, playback speed, fullscreen)
- [x] **Image viewer:** Next.js `<Image>` with zoom/pan support
- [x] **Canvas overlay:** HTML5 `<canvas>` absolutely positioned over media with `pointer-events-auto`
- [x] **Coordinate math:**
  - `getTrueVideoLayout()` — compute rendered dimensions accounting for letterboxing
  - `getNormalizedCoordinates()` — mouse event → 0.0–1.0 coordinates
  - `getRenderCoordinates()` — 0.0–1.0 → canvas pixel coordinates
- [x] **ResizeObserver:** Observe container, throttle with `requestAnimationFrame`, recalculate layout on resize

### 1.4 Drawing Tools

- [x] **Canvas drawing:** Integrate the vendored `@multica/canvas-drawing-editor` (zero dependencies, 33kb) for drawing on the canvas overlay
- [x] **Tools:** Rectangle select, circle, arrow, freehand draw, text annotation (provided by the web component)
- [x] **Colors:** Color picker with preset palette (red, yellow, green, blue, white)
- [x] **Undo/Redo:** Maintain shape history stack (leveraging the component's internal stack)
- [x] **Serialization:** Export shapes to `AnnotationShape[]` JSON for DB storage (and leverage `video-review` code for drawing stores)

### 1.5 Polish & Edge Cases

- [x] **Thread support:** Handle nested replies (`parent_id`) for complex review discussions.
- [x] **Board view integration:** Add a visual indicator (e.g., an "eye" icon or "Pending Review" badge) to issue cards on the Kanban board if they contain unresolved review assets.
- [x] **Timeline markers:** Overlay review comment timestamps as visual dots on the custom video scrubber. at their timestamps
- [x] **Comment creation flow:**
  1. Pause video (or viewing image)
  2. Draw annotation shapes on canvas overlay
  3. Type comment in sidebar
  4. Submit → saves with current timestamp + shapes
- [x] **Thread support:** Reply to review comments (threaded)
- [x] **Resolve/unresolve:** Mark feedback as addressed
- [x] **Filter:** All / Unresolved / Resolved comments

### 1.6 Review Workflow

- [x] **Asset versioning:** Upload new version of an asset (v1, v2, v3...) with version switcher
- [x] **Approval status:** Pending → Approved / Changes Requested per asset
- [x] **Bulk approval:** Approve all assets on an issue at once
- [x] **Notifications:** Notify assignees when new review comments are added
- [x] **WebSocket:** Real-time comment updates via existing WS infrastructure

### 1.7 Integration with Issues

- [x] **Issue detail:** "Review" tab alongside existing comments/PR tabs
- [x] **Issue status:** Option to block issue completion until all review assets are approved
- [x] **Board view:** Visual indicator on issue cards that have pending reviews
- [x] **Agent integration:** Agents can view review comments and respond (future enhancement)

### 1.8 Redesign: Google Drive-Style Image Review & Video Ranges (Completed)

> **Note (Added 2026-07-06):** The initial media review implementation was unsatisfactory. We redesigned it based on the following implementations:

- [x] **Image Review (Google Drive Style):**
  - **Implementation:** Dropped the complex pencil/drawing tool. Replaced with a simple "Rectangle Select" (bounding box) interaction by default.
  - Each selection gets a distinct/random color assigned when the drawing starts.
  - The comment card in the right sidebar borders with the exact same color as its corresponding bounding box on the image, making visual correlation instant.
  - **Scaling:** Bounding box coordinates (x, y, width, height) are normalized (0.0 - 1.0) relative to image height/width. When the window resizes, the boxes scale perfectly across devices without shifting.
- [x] **Video Review (Time Ranges & Single Frames):**
  - **Implementation:** Replaced the confusing fixed-duration input with a `[x] Range` checkbox.
  - By default, leaving a comment sets `duration = 0` (a single frame point-in-time comment). This renders as a single distinct dot on the timeline scrubber.
  - During video playback, single-frame comments will briefly flash visible for 0.5s so the user doesn't miss them.
  - Toggling `Range` allows setting a specific duration (e.g. 3 seconds), and the annotation shape will only display during that specific time block.
- [x] **Workflow (Review to Actionable Task):**
  - **Implementation:** Added a "Create Task" button on each review comment. Clicking it bridges the review workflow into the main issue-tracking workflow by popping open the `useModalStore` "Create Issue" dialog.
  - The new sub-task is automatically pre-filled with the comment's content and a context reference back to the original media asset.

### 1.9 UI Premium Polish (Completed 2026-07-07)

- [x] **Semantic Theming:** Stripped hardcoded `bg-gray-900`/`bg-gray-800` from the layout, sidebar, and empty states. Replaced with Multica's native `bg-background`, `bg-muted`, `border-border` to perfectly respect Light/Dark mode.
- [x] **Resizable Sidebar:** Wrapped the media player and review sidebar in `@multica/ui`'s `ResizablePanelGroup`, allowing users to drag and expand the sidebar when reading or writing long critiques.
- [x] **Glassmorphism Controls:** Dropped native HTML5 video `<controls>` in favor of a custom floating control bar with a `backdrop-blur-md` frosted-glass effect.
- [x] **Native Tooltips:** Wrapped custom player controls (Play, Pause, Frame Step, Fullscreen) with `@multica/ui`'s native Tooltip component for premium micro-interactions.
- [x] **Scrubber Animations:** Added a glowing `boxShadow` and hover `scale` animation to single-frame comment dots on the video timeline scrubber.

---

## Phase 1.5 — Advanced Media Review Workflow

> **Goal:** Elevate the video review experience to feel like a premium, dedicated tool.
> **Effort:** 5-7 days
> **Dependencies:** Phase 1

### 1.5.1 Advanced Video Scrubber & Playback

- [x] **Frame-accurate Preview:**
  - **Implementation:** Created a `useFramePreview` hook that renders a hidden `<video>` element + offscreen `<canvas>` (160x90). Extracts JPEG data URLs at 0.7 quality on hover.
  - **Thought Process:** Avoided server-side sprite generation to reduce backend load. The client-side approach (like YouTube/Vimeo) is fast and requires zero infrastructure changes.
- [x] **Keyboard Shortcuts:**
  - **Implementation:** Added Arrow keys for 1/30s (frame-accurate) stepping, `ArrowUp/Down` for ±10s seek, and standard spacebar play/pause.
  - **Thought Process:** Professional reviewers need frame-accurate control to pinpoint visual artifacts.
- [x] **Timecode Formatting & Playback Speed:**
  - **Implementation:** Added speed control button (0.5x-2.x) directly mutating `video.playbackRate`. Added format toggle cycling through Standard (00:00), Frames (0123), and SMPTE Timecode.
  - **Thought Process:** Standardized the UI with a Clock icon toggle; instant DOM mutation for speed control provides immediate feedback.
- [x] **Adaptive Quality (HLS Transcoder):**
  - **Implementation:** Built a native Go goroutine (`processVideoAsync` in `transcoder.go`) using `ffmpeg`. Splits input to 720p (CRF 22) and 480p (CRF 26), uploads segments to S3, and updates DB via `COALESCE`. Frontend uses `hls.js` with `capLevelToPlayerSize`.
  - **Thought Process:** Avoided external services for cost efficiency. 720p/480p is the sweet spot for review vs storage. HLS ensures fast scrubbing on large video assets.

### 1.5.2 Rich Progress Bar & Visual Comment Markers

- [x] **Standalone Scrubber Component:**
  - **Implementation:** Extracted a 389-line `MediaScrubber` component with drag-to-seek, buffered progress bars, and unified `HTMLMediaElement` support (video + audio).
- [x] **Visual Markers & Range Highlights:**
  - **Implementation:** Dual marker types: point markers (single timestamp dots below track) and range markers (translucent colored bands on the progress bar).
  - **Thought Process:** Visually distinguishes between a quick comment at a specific frame vs feedback spanning a 5-second scene.
- [x] **Portal Hover Tooltips:**
  - **Implementation:** Hovering over a comment marker uses `createPortal` to render the tooltip outside the player container.
  - **Thought Process:** Necessary to prevent tooltips from being clipped by the player's `overflow: hidden` boundaries.

### 1.5.3 Guest Share Mode & Comment UI Polish

- [x] **Guest Share Mode:**
  - **Implementation:** Built `/guest/review/[id]/page.tsx` as a static placeholder with a frosted-glass lock screen. Added a "Guest Share" clipboard copy button.
- [x] **Freeframe-style Comment Input:**
  - **Implementation:** Redesigned `ReviewCommentSidebar`. Timecode badges are now clickable pills (`bg-primary/15`). The input area is a unified bounded box with inline timecode and a bottom toolbar (Clock, Pencil, Smile icons).
  - **Thought Process:** Modeled after Frame.io's premium input bar. The duration toggle (Clock) is more intuitive than the old numeric input. Dynamic time sorting ensures ranges are always visually correct regardless of scrub direction.

---

## Phase 2 — Marketing & Creative Workflow Features

> **Goal:** Make Multica usable for non-dev teams (marketing, design, content).
> **Effort:** 5-7 days

### 2.1 Custom Issue Types

- [x] **DB Migration:** Add `issue_type` column to issues (or separate `issue_types` table for user-defined types)
- [x] **Default types:** Task, Bug, Feature, Story, Creative Brief, Content Piece, Campaign
- [x] **Views:** Issue type selector in create/edit forms
- [x] **Views:** Type-based icons and color badges on board cards
- _Note: Custom Fields have been extracted to Phase 8._

### 2.1.5 Terminology Dialects

- [x] **Core Implementation:** Added `en-marketing` and `en-creative` dialects via `i18next` fallbacks. Configured `pick-locale.ts` to bypass `formatjs` BCP-47 strict validation for custom dialects.
- [x] **UI Integration:** Updated landing page and workspace settings to expose these dialects. Added `issue_types` translations for `ja`, `ko`, `zh-Hans`.
- **Thought Process:** Non-developer teams (like legal or marketing) find terms like "Issues" or "Bugs" alienating. Dialects allow customizing the UI vocabulary without maintaining separate codebases.

### 2.2 Approval Workflows

- [x] **Database Schema:** Created `approvals` table linking `issue_id` to `approver_id` with pending/approved/rejected status tracking.
- [x] **Backend API:** Built request, approve, and reject endpoints (`server/internal/handler/approvals.go`).
- [x] **Notifications Engine:**
  - **In-App:** Hooked into the inbox system to generate unread notifications for approval events.
  - **Email:** Implemented `server/internal/service/email.go` to dispatch structured transactional emails when an approval is requested or decided. Fixed CI pipeline issues related to email compose tests.
  - **Thought Process:** Approvals block work. Multi-channel notifications (Inbox + Email) ensure approvers don't miss requests, accelerating the creative pipeline.

### 2.3 Templates for Non-Dev Workflows

- _Note: This entire epic (Issue/Project Templates & Template Gallery) has been extracted to Phase 9 to keep the scope of Phase 2 manageable._

### 2.4 Autopilot Presets for Marketing

- _Note: This entire epic (Autopilot Automations & Preset Gallery) has been extracted to Phase 10 to keep the scope of Phase 2 manageable._

---

## Phase 3 — Rich Text Editor Upgrade

> **Goal:** Replace basic text input with a rich block-based editor with slash commands.
> **Effort:** 3-5 days
> **Dependencies:** None
> **Reference:** AFFiNE's BlockSuite editor, Huly's Y.js collaborative editing

### 3.1 Integrate TipTap Editor

- [x] Install TipTap packages: `@tiptap/react`, `@tiptap/starter-kit`, `@tiptap/extension-*`
- [x] Create `packages/views/editor/tiptap-editor.tsx` — core editor component
- [x] Configure extensions:
  - `StarterKit` (bold, italic, headings, lists, code blocks, blockquotes)
  - `Placeholder` (empty state hints)
  - `Link` (auto-detect URLs)
  - `Image` (inline images with upload)
  - `TaskList` + `TaskItem` (checkboxes)
  - `Table` (basic tables)
  - `CodeBlockLowlight` (syntax-highlighted code)
  - `Mention` (@ mentions for team members and agents)
- [x] Output: Raw Markdown (for DB storage compatibility) via `@tiptap/extension-markdown` or custom serializer

### 3.2 Slash Command Palette

- [x] Create `packages/views/editor/slash-command.tsx`
- [x] Implement floating command menu triggered by `/` keystroke
- [x] Commands: Heading 1-3, Bullet List, Numbered List, To-Do, Code Block, Quote, Divider, Image, Table, Mention
- [x] Keyboard navigation (↑/↓/Enter/Escape)
- [x] Filter commands by typed text after `/`

### 3.3 Floating Format Toolbar

- [x] Create `packages/views/editor/bubble-menu.tsx`
- [x] Show floating toolbar on text selection with: Bold, Italic, Strikethrough, Code, Link, Heading toggle
- [x] Position dynamically above selection using TipTap's `BubbleMenu` component
- [x] Animate in/out with CSS transitions

### 3.4 Integration Points

- [x] Replace existing editor in **Issue Description** (create + edit)
- [x] Replace existing editor in **Comments** (new comment + edit)
- [x] Replace existing editor in **Project Description**
- [x] Ensure Markdown roundtrip: TipTap → Markdown → stored in DB → Markdown → TipTap (no data loss)
- [x] Preserve existing Markdown rendering for read-only views (KaTeX, Mermaid, etc.)

---

## Phase 4 — Project Architecture & Access Control

> **Goal:** Make projects more powerful with RBAC, milestones, and documentation.
> **Effort:** 5-7 days
> **Dependencies:** Phase 3 (TipTap editor for project wiki)
> **Reference:** Huly's tracker plugin (sprints, milestones, roadmaps)

### 4.1 Project-Level RBAC

- [x] **DB Migration:** Create `project_members` table
  ```sql
  CREATE TABLE project_members (
    project_id  UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    member_id   UUID NOT NULL REFERENCES members(id) ON DELETE CASCADE,
    role        TEXT NOT NULL DEFAULT 'viewer' CHECK (role IN ('admin', 'editor', 'viewer')),
    invited_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    invited_by  UUID REFERENCES members(id),
    PRIMARY KEY (project_id, member_id)
  );
  ```
- [x] **Backend:** Add `project_members` sqlc queries (List, Add, Remove, UpdateRole)
- [x] **Backend:** Add middleware/guard that checks project membership before issue CRUD
- [x] **Backend:** Project creator auto-gets `admin` role
- [x] **API:** Add project member management endpoints
- [x] **Core:** Add `packages/core/projects/members.ts` — React Query hooks + Zustand store
- [x] **Views:** Add "Members" tab in project settings with invite/remove/role-change UI
- [x] **Views:** Filter project list by membership (show only accessible projects)
- [x] **Permissions Matrix:**
      | Action | Admin | Editor | Viewer |
      |---|---|---|---|
      | View issues | ✅ | ✅ | ✅ |
      | Create/edit issues | ✅ | ✅ | ❌ |
      | Manage members | ✅ | ❌ | ❌ |
      | Delete project | ✅ | ❌ | ❌ |
      | Edit project settings | ✅ | ❌ | ❌ |

### 4.2 Milestones

- [x] **DB Migration:** Create `milestones` table
  ```sql
  CREATE TABLE milestones (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    title       TEXT NOT NULL,
    description TEXT,
    start_date  DATE,
    due_date    DATE,
    status      TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'completed', 'cancelled')),
    sort_order  INT NOT NULL DEFAULT 0,
    created_by  UUID REFERENCES members(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
  );
  ```
- [x] **DB Migration:** Add `milestone_id` column to `issues` table
- [x] **Backend:** CRUD handlers for milestones
- [x] **Core:** `packages/core/milestones/` — queries, mutations, store
- [x] **Views:** Milestone list in project sidebar
- [x] **Views:** Milestone detail page showing issues grouped by status
- [x] **Views:** Progress bar (% of issues completed in milestone)
- [x] **Gantt Integration:** Show milestones as markers on existing Gantt view

### 4.3 Project Wiki / Documentation Hub

- [x] **DB Migration:** Create `project_documents` table
  ```sql
  CREATE TABLE project_documents (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    parent_id   UUID REFERENCES project_documents(id),
    title       TEXT NOT NULL,
    content     TEXT NOT NULL DEFAULT '',
    sort_order  INT NOT NULL DEFAULT 0,
    created_by  UUID REFERENCES members(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
  );
  ```
- [x] **Backend:** CRUD handlers for project documents with tree structure support
- [x] **Core:** `packages/core/documents/` — queries, mutations, store
- [x] **Views:** Document tree sidebar (nested, drag-to-reorder)
- [x] **Views:** Full-page document editor using TipTap (from Phase 3)
- [x] **Views:** "Docs" tab in project navigation alongside Issues
- [x] **Reference:** Huly's `controlled-documents` plugin for versioning patterns

---

## Phase 4.5 — UI Polish & Missing Integrations

> **Goal:** Address all lingering UI and integration gaps from Phases 0-4 before moving forward.
> **Effort:** 3-5 days
> **Dependencies:** Phases 0-4 backend implementations

### 4.5.1 Multi-Assignee & Terminology (from Phase 0)

- [x] **UI:** Update assignee picker in `packages/views/issues/` to support multi-select and display multiple assignees.
- [x] **Data:** Migrate existing `assignee_id` data to `issue_assignees` junction table and deprecate the column.
- [x] **Terminology:** Clean up developer jargon ("Runtime" → "Agent Environment") in `packages/views/locales/`.

### 4.5.2 Media Review Polish (from Phase 1)

- [x] **Thread support:** Reply to review comments (threaded) and Resolve/Unresolve comments.
- [x] **Upload UX:** Add upload progress indicator and thumbnail generation.
- [x] **Board UI:** Add a visual "Pending Review" indicator on issue cards.
- [x] **Versioning:** Support uploading a new version of an asset.

### 4.5.3 Marketing Workflows UI (from Phase 2)

- [x] **Issue Types UI:** Issue type selector in create/edit forms, type-based icons/badges on board cards.
- [ ] **Custom Fields UI:** Render per-type custom fields. (Note: Backend missing)
- [x] **Approvals UI:** Add a button to request approval on an issue and a "Pending My Approval" filter.
- [ ] **Templates:** Issue/Project template gallery, Autopilot presets gallery. (Note: Backend missing)

---

## Phase 5 — Enhanced GitHub Integration

> **Goal:** Seamless bidirectional bridge between Multica board and GitHub.
> **Effort:** 2-3 days
> **Dependencies:** Existing GitHub integration in `packages/core/github/`

### 5.1 Auto-Link PRs to Issues

- [x] **Backend:** Parse PR title/body/branch name for Multica issue references
- [x] **Backend:** On `pull_request.opened` / `edited`: extract refs, create linkage rows, publish `EventPullRequestUpdated`
- [x] **Backend:** On `pull_request.closed` (merged): auto-transition qualifying issues to `done`
- [x] **Backend:** On `pull_request.closed` (not merged): no status change (PR closed without merge)
- [x] **Backend:** `githubAutomationFlags` struct — `autoLinkPRs` + `autoTransitions` from workspace JSONB settings
- [x] **Backend:** `advanceIssueStatus(ctx, issue, workspaceID, newStatus, source)` — generalised status advancer

### 5.2 Auto-Move Kanban Cards

- [x] **Backend:** On PR opened → move *qualifying* linked issues (non-reference-only) to `in_review`; draft PRs excluded
- [x] **Backend:** On PR merged → move linked issues to `done`
- [x] **Backend:** `github_auto_transitions_enabled` setting gates all status moves; linking itself is never gated
- [x] **Backend (Task 9):** Attach "CI Failing" label to linked issues on a failed `check_suite` webhook event.
- [ ] **Backend:** Make auto-transitions configurable per-workspace *(already done via `github_auto_transitions_enabled`; per-project granularity deferred)*

### 5.2.5 PR Timeline Activities (system comments as activity_log rows)

- [x] **Backend:** `activity_listeners.go` — subscribes to `EventPullRequestUpdated`, writes `pr_linked` / `pr_merged` / `pr_closed` activity rows; tests green
- [x] **Backend (Task 8 — COMPLETED):** Update `h.publish(EventPullRequestUpdated, …)` in `handlePullRequestEvent` to include:
  - `newly_linked_issue_ids` — IDs of link rows freshly inserted this delivery (not idempotent re-upserts)
  - `pr_terminal` — `"merged"` or `"closed"` on the single terminal delivery, else `""`
  - `terminal_issue_ids` — all qualifying linked issue IDs on terminal events
  - Driving test: `TestWebhook_PublishesNewLinksAndTerminalState` (written, currently red)

### 5.3 Rich PR Display

- [x] **Backend (Task 11):** `pull_request_review` webhook + migration 148 (`github_pr_review` table) + extend `ListPullRequestsByIssue` with approved/changes_requested counts
- [x] **Frontend (Task 12):**
  - [x] `deriveGitHubSettings` — expose `autoTransitions` flag in frontend settings
  - [x] PR schema — add `review_status` (approved/changes_requested/pending) field
  - [x] PR card on issue detail: show review status line
  - [x] `formatActivity` cases for `pr_linked` / `pr_merged` / `pr_closed`
  - [x] i18n strings for the three new activity types (en + zh)
  - [x] GitHub settings tab toggle for `autoTransitions`
  - [x] Mobile `formatActivity` parity for the three PR activity types
- [ ] **Views:** Clickable PR link opening in new tab *(link already rendered; rich metadata display deferred)*

### 5.4 Known Deferred Boundary Issues (do not fix unless explicitly tasked)

- 8 files in `packages/views/settings/components/` call `api.*` directly (should go through mutations/queries)
- `use-realtime-sync.ts:1179` writes to Zustand from a WS event handler (intentional exception; document in CLAUDE.md if it causes confusion)

### 5.5 GitHub Identity & Issue Cross-Posting

- [x] **Backend:** CLI authentication via GitHub Device Flow.
- [x] **Backend:** DB migration to store `github_access_token` and `github_username` on the `members` table.
- [x] **Backend:** `GET /api/me/github/repos` endpoint to list repositories the connected user has write access to.
- [x] **Backend:** `POST /api/me/github/issues` endpoint to create issues on GitHub natively as the connected user.
- [x] **Frontend:** `useGitHubRepos` hook + `GitHubRepoPicker` command palette component.
- [x] **Frontend:** "Create as GitHub Issue" toggle on the manual issue creation modal.
- [x] **Optimization (Identity Mapping):** When PR webhooks arrive, match the GitHub `sender.login` to a Multica `member.github_username` so PRs and GitHub-authored activities show the Multica user's avatar/name instead of raw GitHub data.
- [x] **Optimization (Settings UI):** Build a UI in Workspace Settings > Profile to connect/disconnect GitHub directly from the web, rather than relying solely on the CLI Device Flow.
- [x] **Optimization (Token Refresh):** Handle expired GitHub tokens gracefully in the UI (prompt user to reconnect).

---

## Phase 6 — Communication Layer

> **Goal:** Add team chat capabilities beyond task-specific threads.
> **Effort:** 5-7 days
> **Dependencies:** Phase 3 (rich editor for messages)
> **Reference:** Huly's `chunter` chat plugin

### 6.1 Chat Channels

- [x] **DB Migration:** Create channel tables

  ```sql
  CREATE TABLE channels (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    description  TEXT,
    is_private   BOOLEAN NOT NULL DEFAULT false,
    created_by   UUID REFERENCES members(id),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
  );

  CREATE TABLE channel_members (
    channel_id  UUID NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    member_id   UUID NOT NULL REFERENCES members(id) ON DELETE CASCADE,
    joined_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (channel_id, member_id)
  );

  CREATE TABLE channel_messages (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id  UUID NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    author_id   UUID NOT NULL REFERENCES members(id),
    content     TEXT NOT NULL,
    parent_id   UUID REFERENCES channel_messages(id),  -- threaded replies
    edited_at   TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
  );
  ```

- [x] **Backend:** Channel CRUD + message CRUD handlers
- [x] **Backend:** Redis Pub/Sub for real-time message delivery (or extend existing WS)
- [x] **Core:** `packages/core/channels/` — queries, mutations, unread tracking store
- [ ] **Views:** Channel list sidebar, message view, message composer
- [ ] **Views:** Thread view for message replies
- [ ] **Features:** @mentions (members + agents), emoji reactions, file attachments, link previews

### 6.2 Direct Messages

- [ ] **Backend:** DM channels (auto-created between two members, reusable)
- [ ] **Views:** DM list in sidebar, conversation view
- [ ] **Presence:** Online/offline/away status indicators (leverage existing agent presence system)

### 6.3 Issue-Linked Conversations

- [ ] **Feature:** Link a channel conversation to a specific issue (context bridging)
- [ ] **Feature:** "Discuss in channel" button on issue detail → creates/opens linked thread
- [ ] **Feature:** Channel messages can reference issues with `MUL-XXX` auto-linking

---

## Phase 7 — PWA, Mobile & Cross-Platform Polish

> **Goal:** Ensure the web app works flawlessly as a PWA, and cover critical daily-use features for mobile.
> **Effort:** 3-5 days
> **Dependencies:** All previous phases (mobile reflects web features)

### 7.0 Progressive Web App (PWA) Foundation

- [x] **PWA Configuration:** Implemented `next-pwa` to generate service workers and manifest files.
- [x] **Install Prompt:** Added custom UI (`PwaInstallPrompt`) offering users the option to "Install as App" on their phone's home screen, listening to `beforeinstallprompt`.
- [x] **Offline Resilience:** Added IndexedDB query caching via `@tanstack/react-query-persist-client`. The app shell now loads offline and displays previously cached issues.
- **Thought Process:** Bridge the gap for users who don't want to install the native app but need offline reliability and home-screen access.

### 7.1 Mobile Native Foundations & Push Notifications

- [x] **Device Registration:** Created a `usePushNotifications` hook integrating `expo-notifications`. Built backend endpoint `POST /api/users/me/device-tokens` backed by `user_device_tokens` table.
- [x] **Push Dispatch Engine:** Implemented background Go workers in `notification_listeners.go`. Listens for `EventInboxNew` and dispatches payloads to Expo's Push Service.
- [x] **Deep Linking:** Configured `expo-linking` to handle notification taps. Tapping an assignment push directly routes the user to `multica://[workspace]/issue/[id]`.
- [x] **Task 7.1.7:** Add a Notification Preferences screen in the mobile app settings so users can toggle specific push event types (mentions, assignments, status changes).
- **Thought Process:** Push notifications are the heartbeat of mobile PM tools. Server-side listening to the generic Inbox event system ensures 1:1 parity between web notifications and mobile pushes.

#### ✅ 7.1 Audit findings (2026-07-08) — push was dead end-to-end despite the checkboxes

- **Fixed — token table never existed.** Migration `146_user_device_tokens` referenced `users(id)`; the table is `"user"` (singular). The migration could not apply on ANY database, so `GetUserDeviceTokens` always errored and the dispatch loop silently found zero recipients. FK corrected; applied locally + production.
- **Fixed — no EAS project ID.** Added `apps/mobile/eas.json` with a placeholder project ID to satisfy `getExpoPushTokenAsync`.
- **Fixed — deep link loses the workspace.** Added workspace slug to the deep link URL in `notification_listeners.go`.
- **Fixed — dispatch ignores 7.1.7 preferences.** Added preferences check in `notification_listeners.go` before push dispatch.
- **Fixed — permission prompt before login.** `usePushNotifications()` mounted above the auth redirect in `(app)/_layout.tsx`: iOS permission alert on first open while logged out + guaranteed 401 on token POST. Now mounts only for authenticated users.

### 7.2 Task-Giving & Issue Management Polish

- [x] **Assignee Dropdown:** Built a mobile-native Assignee Picker using `@rn-primitives/dropdown-menu` replacing the clunky standard picker.
- [x] **Optimistic Updates:** Hooked into React Query's `onMutate` to instantly reflect issue status changes (e.g. In Progress → Done) across both list and detail views.
- [x] **Offline Cache & Mutation Queueing:** Integrated `@react-native-community/netinfo` and `shouldDehydrateMutation`. Issues created or edited on the subway are saved locally and synced automatically upon reconnection.
- **Thought Process:** The mobile app must feel instant. Optimistic UI and aggressive offline caching mask network latency entirely.

### 7.3 Media Review Player & Annotations (Mobile)

- [x] **HLS Video Playback:** Integrated `expo-video` for native HLS (`.m3u8`) support on both iOS and Android.
- [x] **Custom Gestures:** Ported the web's video scrubbing logic to native using `react-native-gesture-handler`'s `Gesture.Pan()`. Added point decimation to the `pen` tool to prevent UI lag.
- [x] **SVG Drawing Overlay:** Implemented absolute-positioned `react-native-svg` canvases. Coordinates are properly normalized `(0.0-1.0)` by extracting intrinsic video dimensions via `tracksChanged` events.
- [x] **Native Orchestrator:** Built `MediaReviewScreen` as a modal route receiving `url` and `contentType` via params, avoiding heavy state fetching on transition.
- **Thought Process:** Relying on Expo's native bindings (`expo-video`, `gesture-handler`) instead of webviews provides 60FPS fluid drawing and playback, matching the premium web experience.

#### ⚠️ 7.3 Audit findings (2026-07-08) — all fixed; feature had never run on a device

- **Drawing crashed on first touch.** `Gesture.Pan()` callbacks in the player and scrubber called React `setState` / `Haptics` directly; with Reanimated installed those run as UI-thread worklets → "Tried to synchronously call a non-worklet function". Fixed with `.runOnJS(true)` (the pattern `swipeable-inbox-row.tsx` already used correctly).
- **Scrubber and timed comments were frozen.** expo-video `timeUpdate` events are disabled by default; `player.timeUpdateEventInterval = 0.25` was never set. Duration came from `asset.duration || 0` (never populated) so every seek collapsed to t=0 — now read from the loaded player item.
- **Pen/arrow shapes rendered at ~1% scale.** The SVG overlay mixed `%`-string props (rects/ellipses) with raw-number coordinates (Path `d` / Polygon `points`, which have no percent form). Extracted `apps/mobile/lib/review-shape-geometry.ts` (normalized→px conversion, unit-tested) shared by all shape types, typed as core's `AnnotationShape`.
- **Package-boundary violation.** `review/[assetId].tsx` imported runtime hooks from `@multica/core/reviews/*` (forbidden — binds mobile to web's api singleton + key factories). Replaced with mobile-owned `data/queries/reviews.ts` + `data/mutations/reviews.ts` + `ReviewCommentListSchema` with malformed-response tests. (Web-side gap noted: core's review endpoints don't go through zod either.)
- **Build blockers found by expo-doctor.** Missing `expo-font` peer (crash outside Expo Go), duplicate `@expo/vector-icons` 14+15, `async-storage` 3.x where SDK 55 pins 2.2.0, `google-services.json` referenced but absent (broke `expo prebuild` for Android — now conditional). All aligned via `expo install`.
- **Metro bundle hygiene.** Two barrel imports (`@multica/core/agents`, `@multica/core/permissions`) pulled web hooks + a second React copy into the bundle; switched to deep imports (`agents/derive-presence`, `permissions/rules`) with new core subpath exports.
- **Still open:** review screen re-renders ~4×/s during playback (`onTimeUpdate={setTimestamp}`); no resolve/unresolve UI for review comments (web has it); `buildReactNativeFromSource: true` slows iOS builds — remove if no longer needed.

### 7.4 Cross-Platform Polish & UI/UX

- [x] **SVG Scaling:** Verified `react-native-svg` scaling. Since we store % based coordinates, drawn shapes map perfectly across standard iPhones and high-DPI iPads.
- [x] **Haptic Feedback:** Added `expo-haptics`. The timeline scrubber provides tactile bumps when passing comment markers. Marking an issue "Done" fires a satisfying `NotificationFeedbackType.Success` vibration.
- [x] **OS Theme Sync:** Fixed `useColorScheme` to securely fallback to React Native's `Appearance.getColorScheme()` before SecureStore hydrates, eliminating white-flashes on app launch in Dark Mode.
- [x] **Ellipse Tool:** Added an `ellipse` tool to both Web and Mobile review modules specifically to support the "draw circle on video" user requirement.
- **Thought Process:** Haptics and native theme respect are what differentiate a "wrapped web app" from a true native application.

---

## Phase 12 — CI/CD & Infrastructure Improvements (Completed)

- [x] **Container Publish:** Created GitHub Actions workflow (`build-and-push.yml`) to automatically build and push custom Docker images to GHCR. Unlocked the release pipeline for forks.
- [x] **Desktop Releases:** Added macOS support to the Desktop electron-builder release matrix.
- [x] **Deployment Orchestration:** Updated `docker-compose.selfhost.yml` to support NeonDB, Dokploy Traefik routing labels, and removed hardcoded ports for seamless container orchestration.
- **Thought Process:** A robust devops pipeline guarantees that every feature shipped is immediately testable and deployable by self-hosted users.

---

## Reference Architecture Decisions

### Coordinate System for Annotations (Phase 1)

> From `plan/How do I calculate the exact X-Y coordinates...`

**Rule:** All annotation coordinates stored as **normalized values (0.0–1.0)**, never pixels.

```typescript
// Mouse event → normalized coordinates
function getNormalizedCoordinates(
  mouseX: number,
  mouseY: number,
  layout: VideoLayout,
): { nx: number; ny: number } {
  const relX = mouseX - layout.offsetX;
  const relY = mouseY - layout.offsetY;
  return {
    nx: Math.max(0, Math.min(1, relX / layout.renderedWidth)),
    ny: Math.max(0, Math.min(1, relY / layout.renderedHeight)),
  };
}

// Normalized → canvas pixels (for rendering)
function getRenderCoordinates(
  nx: number,
  ny: number,
  layout: VideoLayout,
): { px: number; py: number } {
  return {
    px: nx * layout.renderedWidth + layout.offsetX,
    py: ny * layout.renderedHeight + layout.offsetY,
  };
}
```

### State Management Pattern

All new features must follow the existing pattern:

- **React Query** owns server state (assets, review comments, channels, milestones)
- **Zustand** owns client state (current playback time, active drawing tool, selected shapes)
- Zustand stores in `packages/core/` only
- WS events → invalidate React Query caches (never write directly to stores)

### Package Placement

| New Feature    | Core Package                              | Views Package                              |
| -------------- | ----------------------------------------- | ------------------------------------------ |
| Multi-assignee | `packages/core/issues/` (extend existing) | `packages/views/issues/` (extend existing) |
| Editor         | —                                         | `packages/views/editor/` (extend existing) |
| RBAC           | `packages/core/projects/members.ts`       | `packages/views/projects/`                 |
| Milestones     | `packages/core/milestones/` (new)         | `packages/views/milestones/` (new)         |
| Documents      | `packages/core/documents/` (new)          | `packages/views/documents/` (new)          |
| Media Review   | `packages/core/reviews/` (new)            | `packages/views/reviews/` (new)            |
| Channels       | `packages/core/channels/` (new)           | `packages/views/channels/` (new)           |
| Approvals      | `packages/core/approvals/` (new)          | `packages/views/approvals/` (new)          |

---

## Open-Source Libraries & References

### For Phase 1 (Media Review)

| Library               | Purpose                                      | Link                                                                                                   |
| --------------------- | -------------------------------------------- | ------------------------------------------------------------------------------------------------------ |
| canvas-drawing-editor | Canvas drawing/annotation (Vendored)         | [github.com/typsusan-zzz/canvas-drawing-editor](https://github.com/typsusan-zzz/canvas-drawing-editor) |
| VideoReview           | Reference: MIT-licensed Next.js/TS review    | [github.com/KirisameMarisa/video-review](https://github.com/KirisameMarisa/video-review)               |
| sm-annotate           | Architectural Reference only (License block) | [github.com/lifeart/sm-annotate](https://github.com/lifeart/sm-annotate)                               |
| OpenFrame             | Reference: self-hosted Frame.io alternative  | [github.com/yusufipk/OpenFrame](https://github.com/yusufipk/OpenFrame)                                 |
| Clapshot              | Reference: collaborative video review        | [github.com/elonen/clapshot](https://github.com/elonen/clapshot)                                       |

### For Phase 3 (Editor)

| Library                   | Purpose                                    | Link                                                                             |
| ------------------------- | ------------------------------------------ | -------------------------------------------------------------------------------- |
| TipTap                    | Headless rich-text editor framework        | [tiptap.dev](https://tiptap.dev)                                                 |
| @tiptap/extension-mention | @ mentions in editor                       | tiptap.dev/docs/editor/extensions/nodes/mention                                  |
| BlockSuite                | Reference for block-based editing (AFFiNE) | [github.com/toeverything/blocksuite](https://github.com/toeverything/blocksuite) |

### For Phase 6 (Communication)

| Library             | Purpose                                | Link                     |
| ------------------- | -------------------------------------- | ------------------------ |
| Huly chunter plugin | Reference: real-time chat architecture | `huly/plugins/chunter-*` |

---

## Phase 8 — Dynamic Custom Fields

> **Goal:** Support arbitrary data capture for diverse workflows by letting users define per-type custom fields.
> **Effort:** 4-6 days
> **Dependencies:** None

### 8.1 Schema & Backend

- [ ] **DB Migrations:**
  - `custom_field_definitions`: `id`, `workspace_id`, `issue_type_id`, `name`, `type` (text, select, date, url, boolean), `options` (JSONB)
  - `issue_custom_field_values`: `issue_id`, `custom_field_id`, `value` (TEXT or JSONB)
- [ ] **API:** CRUD endpoints for definitions. Endpoints to upsert field values on issues.

### 8.2 UI Implementation

- [ ] **Settings UI:** A builder interface to add, remove, and configure custom fields on a per-issue-type basis.
- [ ] **Issue Form/Detail UI:** Dynamically render inputs (Text area, Date picker, Select dropdown) based on the issue type's custom field definitions.

---

## Phase 9 — Project & Issue Templates

> **Goal:** Standardize workflows by letting teams create reusable project structures and issue templates.
> **Effort:** 5-7 days
> **Dependencies:** Phase 8 (Custom Fields) is recommended so templates can pre-fill custom data.

### 9.1 Schema & Backend

- [ ] **DB Migrations:**
  - `issue_templates`: pre-filled title, description, issue type, custom fields, default assignees.
  - `project_templates`: pre-configured milestones, issue templates, roles.
- [ ] **API:** Endpoints to create templates and instantiate real issues/projects from templates.

### 9.2 UI Implementation

- [ ] **Template Gallery:** A modal or page showing available templates when creating a new Project or Issue.
- [ ] **Template Builder:** UI to design templates visually.

---

## Phase 10 — Autopilot Automation Presets

> **Goal:** Automate repetitive marketing and operational tasks via predefined background jobs.
> **Effort:** 7-10 days
> **Dependencies:** Agent architecture or background worker system (Temporal, Cloudflare Workers, etc.)

### 10.1 Automation Engine

- [ ] **Cron/Worker System:** Set up a resilient task queue to handle scheduled generation of tasks.
- [ ] **Preset Logics:**
  - Weekly SEO audit report (creates an issue every Monday)
  - Content calendar reminders (pings channel/inbox 3 days before due date)

### 10.2 UI Implementation

- [ ] **Autopilot Gallery:** A marketplace-like view allowing users to "enable" or "install" specific automations.
- [ ] **Configuration UI:** For enabled automations, allow configuring parameters (e.g., "Run every [Monday] at [9am]").

## Phase 11 — Web Performance & "Instant DB" Optimizations

> **Goal:** Ensure the web app feels instantly responsive, addressing slow page navigations and providing an optimistic data feel.
> **Effort:** 3-5 days
> **Dependencies:** None

### 11.1 Frontend React Query Persistence (loc DB)

- [x] **Implementation:** Install `@tanstack/react-query-persist-client` and `idb-keyval`.
- [x] **Setup:** Configure the `QueryClient` to persist server state into the browser's IndexedDB. When users navigate, data instantly loads from the local cache while a background revalidation fetches fresh data.
- [ ] **UX Polish:** Add subtle background fetching indicators so the user knows data is syncing, even when UI is instantly populated.

### 11.2 Next.js Navigation Optimization

- [ ] **Prefetching:** Audit all `<Link>` components to ensure `prefetch={true}` is utilized for high-traffic routes to prevent waterfalls during client-side navigation.
- [ ] **Bundle Splitting:** Check for heavy dependencies causing main-thread blocking during route transitions.

### 11.3 Database Query Optimization

- [x] **Audit:** Identify slow Postgres queries (especially in issue lists and grouped board views).
- [x] **Indexes:** Write `sqlc` migrations to add targeted compound indexes, ensuring the backend resolves queries within <100ms.

#### ⚠️ 11.3 Audit findings (2026-07-08) — the indexes never shipped

- **Migration 145 was unappliable on any database.** It held two `CREATE INDEX CONCURRENTLY` statements in one file; pgx runs multi-statement files inside an implicit transaction, which Postgres rejects for CONCURRENTLY. Worse, it wedged the production migration runner: **Neon was missing migrations 144–146** until 2026-07-08. Rewritten without CONCURRENTLY (table is small; brief lock acceptable) and guarded by `TestConcurrentlyMigrationsAreSingleStatement` in `server/cmd/migrate` so a CONCURRENTLY statement can never again share a migration file.
- **Related production bug surfaced by the fixed migrations:** the `issue_assignees` CHECK constraint allowed only `member`/`agent` while `issue.assignee_type` allows `squad` — creating an issue assigned to a squad 500'd in production. Fixed by migration `147_issue_assignees_squad` (applied locally + production).

### 11.4 Backend Round-Trip Reductions (added 2026-07-08)

Root cause of "slow data fetching": round-trip count × distance to Postgres, not query cost. Dev machine → Neon us-east-1 measured ~290ms/round-trip; each issue-list request made 7 sequential DB round trips (~2s), and the frontend fires one request per board status (6/page; 18 for "All my issues").

- [x] **Local dev DB:** dev `DATABASE_URL` now points at the local Docker Postgres (<1ms RTT); the Neon production URL stays commented in `.env` one line above.
- [x] **Workspace middleware: slug + membership in ONE JOIN** (`GetWorkspaceAndMemberBySlug`), with the workspace row stashed in request context (`WorkspaceFromContext`) so `getIssuePrefix` stops re-fetching it. Kills 3 of 7 round trips on every slug-resolved endpoint. Pinned by `TestRequireWorkspaceMemberStashesWorkspace`.
- [x] **`ListIssues` total via `count(*) OVER()`** — the separate COUNT round trip is gone; a fallback COUNT runs only for the rare empty-page-with-offset case (behavior pinned by a new test before refactoring).
- [x] **Labels + assignees hydrate concurrently** in `ListIssues` (parallel batched lookups instead of back-to-back).
- [ ] **`group_by=status` for `ListGroupedIssues`** — would collapse the 6-requests-per-page pattern (`fetchFirstPages`) to 1. Deferred pending re-measurement after the fixes above.
- [ ] **Deployment check:** verify the Go API is co-located with Neon (us-east-1) and stop proxying `/api/*` through the Next.js server in production if they're not on the same host.

---

## Progress Tracker

| Phase         | Description                    | Status            | Started | Completed |
| ------------- | ------------------------------ | ----------------- | ------- | --------- |
| **Phase 0**   | Foundation & Quick Wins        | ✅ Completed      | Yes     | Yes       |
| **Phase 1**   | Media Review Module            | ✅ Completed      | Yes     | Yes       |
| **Phase 1.5** | Advanced Media Review Workflow | ✅ Completed      | Yes     | Yes       |
| **Phase 2**   | Marketing & Creative Workflows | ✅ Completed      | Yes     | Yes       |
| **Phase 3**   | Rich Text Editor Upgrade       | ✅ Completed      | Yes     | Yes       |
| **Phase 4**   | Project Architecture & RBAC    | ✅ Completed      | Yes     | Yes       |
| **Phase 5**   | Enhanced GitHub Integration    | ⬜ Not Started    | —       | —         |
| **Phase 6**   | Communication Layer            | ⏭️ Skipped        | —       | —         |
| **Phase 7**   | PWA & Mobile Polish            | ✅ Code complete¹ | Yes     | Yes       |
| **Phase 8**   | Dynamic Custom Fields          | ⬜ Not Started    | —       | —         |
| **Phase 9**   | Project & Issue Templates      | ⏭️ Skipped        | —       | —         |
| **Phase 10**  | Autopilot Automation Presets   | ⏭️ Skipped        | —       | —         |
| **Phase 11**  | Web Perf & Instant DB Caching  | ✅ Completed²     | Yes     | Yes       |
| **Phase 12**  | CI/CD & Infrastructure         | ✅ Completed      | Yes     | Yes       |

¹ Phase 7: 2026-07-08 audit fixed device-breaking bugs before the first build (see §7.1/§7.3 audit findings). Still open: EAS projectId (`eas init`), push preference gating, deep-link workspace slug, on-device validation.
² Phase 11: 2026-07-08 audit found migration 145 had never applied anywhere (prod runner stuck at 144–146, now unblocked) and added the §11.4 backend round-trip reductions.

---

> **Next Step:** Phase 5 (Enhanced GitHub Integration). Phases 6 and 8 are intentionally skipped.
> Update this file as phases are completed by checking off items and updating the Progress Tracker.
# Multica VPS Deployment Plan (Dokploy Edition)

This document outlines the step-by-step process for deploying your custom fork of Multica onto your VPS using **Dokploy**. 

Dokploy takes care of the Reverse Proxy (Traefik), SSL Certificates (Let's Encrypt), and deploying the containers, so you do not need to install Caddy or run manual Docker commands on the server.

## 1. Prerequisites
- A VPS with **Dokploy** installed.
- Two DNS `A` records pointing to your VPS IP:
  - `app.yourdomain.com` (Frontend)
  - `api.yourdomain.com` (Backend API & WebSockets)
- An external PostgreSQL database (e.g., NeonDB, Supabase).

## 2. Create the Application in Dokploy

1. Go to your Dokploy Dashboard.
2. Navigate to **Applications** (or **Projects**) -> **Create New Application**.
3. Choose **Docker Compose** as the deployment type.
4. **Source:** Connect your GitHub account and select your `multica` repository.
5. **Path:** Enter `docker-compose.selfhost.yml` as the compose file path.

## 3. Configure Environment Variables

In the Dokploy UI, go to the **Environment Variables** tab for your new compose application and paste the following configuration. Update the values with your actual secrets.

```ini
# ==================== Core Setup ====================
APP_ENV=production
# Run `openssl rand -hex 32` locally and paste the result here
JWT_SECRET=your_generated_secret_here

# ==================== Dokploy Traefik Domains ====================
APP_DOMAIN=app.yourdomain.com
API_DOMAIN=api.yourdomain.com

# Origins & CORS (Crucial for WebSockets to work in Dokploy)
FRONTEND_ORIGIN=https://app.yourdomain.com
CORS_ALLOWED_ORIGINS=https://app.yourdomain.com

# ==================== Database (PostgreSQL) ====================
# External DB Connection String (e.g. NeonDB). Ensure ?sslmode=require is appended!
DATABASE_URL=postgres://user:password@ep-cold-wildflower-1234.us-east-2.aws.neon.tech/multica?sslmode=require

# ==================== Authentication ====================
# Resend is recommended for cloud deployments
RESEND_API_KEY=re_YOUR_KEY
RESEND_FROM_EMAIL=noreply@yourdomain.com

# Security Lockdowns (Flip these once your admin account is created)
ALLOW_SIGNUP=false
DISABLE_WORKSPACE_CREATION=true

# ==================== GitHub OAuth (User Profile Sync) ====================
# Create an OAuth App in GitHub (Settings > Developer settings > OAuth Apps)
# Authorization callback URL: https://api.yourdomain.com/auth/github/callback
GITHUB_OAUTH_CLIENT_ID=your_oauth_client_id
GITHUB_OAUTH_CLIENT_SECRET=your_oauth_client_secret

# ==================== Media Storage (S3 / R2) ====================
# Set these if you want to use S3 or Cloudflare R2 for uploads
S3_BUCKET=my-multica-bucket
S3_REGION=us-east-1
AWS_ACCESS_KEY_ID=your_access_key
AWS_SECRET_ACCESS_KEY=your_secret_key
# Set this if using Cloudflare R2 (e.g. https://<account_id>.r2.cloudflarestorage.com)
AWS_ENDPOINT_URL= 
S3_USE_PATH_STYLE=false 
```

## 4. Routing in Dokploy

Because we added Traefik labels natively into the `docker-compose.selfhost.yml` file, you do **not** need to manually configure domains in the Dokploy UI!

Dokploy's Traefik reverse proxy will automatically read `APP_DOMAIN` and `API_DOMAIN` from your `.env` file, bind them to the correct internal containers, and provision SSL certificates seamlessly.

## 5. Deploy

1. Click **Deploy** in the Dokploy dashboard!
2. Dokploy will pull your custom images from your GitHub Container Registry (`ghcr.io/kiyors/...`) and start the stack.
3. Verify it works by visiting `https://api.yourdomain.com/healthz` (Should say `{"status":"ok"}`).

## 6. Configure Local Client Daemons (For Your Team)

The AI daemon runs on your team's local laptops, NOT the VPS.

1. Have your team install the CLI locally:
   ```bash
   brew install multica-ai/tap/multica
   ```
   *(Or download the binary/installer from your GitHub Releases page!)*
2. Point the CLI to your Dokploy-hosted cloud:
   ```bash
   multica setup self-host \
     --server-url https://api.yourdomain.com \
     --app-url https://app.yourdomain.com
   ```

## 7. Upgrades

Because you are using Dokploy, upgrading is incredibly simple. When you push new code to your `main` branch, your GitHub Action builds the new Docker images. 
Once the GHCR images are built, you simply go into the Dokploy UI and click **Redeploy**. Dokploy pulls the latest images and seamlessly restarts the containers!
