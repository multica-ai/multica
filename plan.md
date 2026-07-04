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
6. [Phase 2 — Marketing & Creative Workflow Features](#phase-2--marketing--creative-workflow-features)
7. [Phase 3 — Rich Text Editor Upgrade](#phase-3--rich-text-editor-upgrade)
8. [Phase 4 — Project Architecture & Access Control](#phase-4--project-architecture--access-control)
9. [Phase 5 — Enhanced GitHub Integration](#phase-5--enhanced-github-integration)
10. [Phase 6 — Communication Layer](#phase-6--communication-layer)
11. [Phase 7 — Mobile & Cross-Platform Polish](#phase-7--mobile--cross-platform-polish)
12. [Reference Architecture Decisions](#reference-architecture-decisions)
13. [Open-Source Libraries & References](#open-source-libraries--references)

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
- [ ] **UI:** Update assignee picker in `packages/views/issues/` to support multi-select
- [ ] **Backward Compat:** Keep `assignee_id` column temporarily, migrate existing data to junction table, deprecate old field

### 0.2 Clean Up Dev-Only Terminology

- [ ] Audit all user-facing strings in `packages/views/locales/` for developer jargon
- [ ] Replace dev terms with user-friendly alternatives where appropriate (e.g., "Runtime" → "Agent Environment")
- [ ] Update i18n keys for both `en` and `zh-CN` locales

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
- [ ] **Backend:** Thumbnail generation for videos (extract frame at 1s mark)
- [x] **Core:** `packages/core/reviews/` — queries, mutations, upload hooks
- [x] **UI:** Drag-and-drop upload zone in issue detail page
- [ ] **UI:** Upload progress indicator with cancel support

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
- [ ] **Thread support:** Reply to review comments (threaded)
- [ ] **Resolve/unresolve:** Mark feedback as addressed
- [ ] **Filter:** All / Unresolved / Resolved comments

### 1.6 Review Workflow

- [ ] **Asset versioning:** Upload new version of an asset (v1, v2, v3...) with version switcher
- [x] **Approval status:** Pending → Approved / Changes Requested per asset
- [x] **Bulk approval:** Approve all assets on an issue at once
- [x] **Notifications:** Notify assignees when new review comments are added
- [x] **WebSocket:** Real-time comment updates via existing WS infrastructure

### 1.7 Integration with Issues

- [x] **Issue detail:** "Review" tab alongside existing comments/PR tabs
- [x] **Issue status:** Option to block issue completion until all review assets are approved
- [ ] **Board view:** Visual indicator on issue cards that have pending reviews
- [ ] **Agent integration:** Agents can view review comments and respond (future enhancement)

---

## Phase 2 — Marketing & Creative Workflow Features

> **Goal:** Make Multica usable for non-dev teams (marketing, design, content).
> **Effort:** 5-7 days
> **Dependencies:** Phase 1 (media review)
> **Reference:** Huly's workflow automation, AFFiNE's template system

### 2.1 Custom Issue Types

- [x] **DB Migration:** Add `issue_type` column to issues (or separate `issue_types` table for user-defined types)
- [x] **Default types:** Task, Bug, Feature, Story, Creative Brief, Content Piece, Campaign
- [ ] **Custom fields:** Allow per-type custom fields (text, date, select, URL)
- [ ] **Views:** Issue type selector in create/edit forms
- [ ] **Views:** Type-based icons and color badges on board cards

### 2.2 Approval Workflows

- [x] **DB Migration:** Create `approvals` table
  ```sql
  CREATE TABLE approvals (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id    UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    approver_id UUID NOT NULL REFERENCES members(id),
    status      TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
    comment     TEXT,
    decided_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
  );
  ```
- [x] **Backend:** Request approval, approve, reject endpoints
- [ ] **Views:** Approval request UI on issues
- [ ] **Views:** "Pending My Approval" inbox filter
- [ ] **Notifications:** Email/in-app notification when approval requested or decision made

### 2.3 Templates for Non-Dev Workflows

- [ ] **Feature:** Issue templates with pre-filled fields (Creative Brief template, Content Calendar template, Campaign template)
- [ ] **Feature:** Project templates (Marketing Campaign project with predefined milestones and issue templates)
- [ ] **Feature:** Template gallery accessible during project/issue creation

### 2.4 Autopilot Presets for Marketing

- [ ] **Feature:** Pre-built autopilot templates:
  - Weekly SEO audit report
  - Social media content drafting
  - Performance report generation
  - Content calendar reminders
- [ ] **UI:** "Autopilot Gallery" with one-click setup for common marketing automations

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
- [ ] **Thread support:** Reply to review comments (threaded) and Resolve/Unresolve comments.
- [ ] **Upload UX:** Add upload progress indicator and thumbnail generation.
- [ ] **Board UI:** Add a visual "Pending Review" indicator on issue cards.
- [ ] **Versioning:** Support uploading a new version of an asset.

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

- [ ] **Backend:** Parse PR title/body/branch name for Multica issue references (regex: `MUL-\d+`, `[WORKSPACE]-\d+`)
- [ ] **Backend:** On GitHub webhook `pull_request.opened` / `pull_request.edited`:
  - Extract issue references from title, body, and branch name
  - Create linkage records in DB
  - Post system comment on the Multica issue: "PR #123 linked"
- [ ] **Backend:** On `pull_request.closed` (merged):
  - If PR title contains `Fixes MUL-XXX` or `Closes MUL-XXX`, auto-transition issue status to `done`
  - Post system comment: "Resolved by PR #123"
- [ ] **Backend:** On `pull_request.closed` (not merged):
  - Post system comment: "PR #123 closed without merge"

### 5.2 Auto-Move Kanban Cards

- [ ] **Backend:** On PR opened → move linked issue to "In Review" status
- [ ] **Backend:** On PR merged → move linked issue to "Done" status
- [ ] **Backend:** On PR checks failing → add "CI Failing" label to linked issue
- [ ] **Backend:** Make auto-transitions configurable per-project (some teams may not want this)

### 5.3 Rich PR Display

- [ ] **Views:** Show PR details inline on issue detail page:
  - PR status (open/merged/closed)
  - CI check status (pass/fail/pending)
  - Review status (approved/changes requested/pending)
  - Merge conflicts indicator
  - Lines added/removed
- [ ] **Views:** Clickable PR link opening in new tab

---

## Phase 6 — Communication Layer

> **Goal:** Add team chat capabilities beyond task-specific threads.
> **Effort:** 5-7 days
> **Dependencies:** Phase 3 (rich editor for messages)
> **Reference:** Huly's `chunter` chat plugin

### 6.1 Chat Channels

- [ ] **DB Migration:** Create channel tables

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

- [ ] **Backend:** Channel CRUD + message CRUD handlers
- [ ] **Backend:** Redis Pub/Sub for real-time message delivery (or extend existing WS)
- [ ] **Core:** `packages/core/channels/` — queries, mutations, unread tracking store
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

## Phase 7 — Mobile & Cross-Platform Polish

> **Goal:** Ensure the mobile app covers critical daily-use features.
> **Effort:** 3-5 days
> **Dependencies:** All previous phases (mobile reflects web features)

### 7.1 Mobile Feature Parity Audit

- [ ] Audit `apps/mobile/` against web features
- [ ] Ensure these work on mobile:
  - [ ] Issue list + detail view
  - [ ] Create/edit issues
  - [ ] Comments (view + create)
  - [ ] Push notifications
  - [ ] Board view (simplified)
  - [ ] Inbox
  - [ ] Review assets (view mode, leave comments)

### 7.2 Push Notifications

- [ ] **Backend:** Push notification service (FCM for Android, APNs for iOS)
- [ ] **Backend:** Notification preferences (per-event-type opt-in/out)
- [ ] **Mobile:** Register device tokens
- [ ] **Mobile:** Handle notification tap → deep link to relevant screen

### 7.3 Offline Support

- [ ] **Mobile:** Cache critical data (issues list, recent comments) for offline viewing
- [ ] **Mobile:** Queue mutations when offline, sync when back online
- [ ] **Reference:** AFFiNE's local-first CRDT approach (y-octo) for inspiration

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

| Library                     | Purpose                                     | Link                                                                     |
| --------------------------- | ------------------------------------------- | ------------------------------------------------------------------------ |
| canvas-drawing-editor       | Canvas drawing/annotation (Vendored)        | [github.com/typsusan-zzz/canvas-drawing-editor](https://github.com/typsusan-zzz/canvas-drawing-editor) |
| VideoReview                 | Reference: MIT-licensed Next.js/TS review   | [github.com/KirisameMarisa/video-review](https://github.com/KirisameMarisa/video-review) |
| sm-annotate                 | Architectural Reference only (License block)| [github.com/lifeart/sm-annotate](https://github.com/lifeart/sm-annotate) |
| OpenFrame                   | Reference: self-hosted Frame.io alternative | [github.com/yusufipk/OpenFrame](https://github.com/yusufipk/OpenFrame)   |
| Clapshot                    | Reference: collaborative video review       | [github.com/elonen/clapshot](https://github.com/elonen/clapshot)         |

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

## Progress Tracker

| Phase       | Description                    | Status         | Started | Completed |
| ----------- | ------------------------------ | -------------- | ------- | --------- |
| **Phase 0** | Foundation & Quick Wins        | ⬜ Not Started | —       | —         |
| **Phase 1** | Media Review Module            | ⬜ Not Started | —       | —         |
| **Phase 2** | Marketing & Creative Workflows | ⬜ Not Started | —       | —         |
| **Phase 3** | Rich Text Editor Upgrade       | ⬜ Not Started | —       | —         |
| **Phase 4** | Project Architecture & RBAC    | ⬜ Not Started | —       | —         |
| **Phase 5** | Enhanced GitHub Integration    | ⬜ Not Started | —       | —         |
| **Phase 6** | Communication Layer            | ⬜ Not Started | —       | —         |
| **Phase 7** | Mobile & Cross-Platform Polish | ⬜ Not Started | —       | —         |

---

> **Next Step:** Start with Phase 0 (Multi-Assignee) → then Phase 1 (Media Review) → then Phase 2 (Marketing Workflows).
> Update this file as phases are completed by checking off items and updating the Progress Tracker.
