# Mobile App Implementation Plan

> Product: Multicam Mobile
> App: `apps/mobile`
> Primary artifact: Android build
> Source requirements: OPE-115 / OPE-119
> Last updated: 2026-04-27

## Goal

Build a React Native mobile app with Expo that supports the core Multica mobile workflows:

- email code login
- workspace switching
- Issues board
- Issue detail with comments, attachments, reactions, child issues, agent transcript, realtime
- Projects
- Mine read-only views for Runtimes, Agents, Inbox
- issue/project search
- offline read cache and explicit retry
- Expo push notifications
- deep links with `wujieai_multicam://`

The first validation target is Android with package name `com.wujieai.multica`.

## Architecture

```mermaid
flowchart TD
  App[apps/mobile] --> Nav[React Navigation]
  App --> Screens[RN screens]
  App --> Platform[Expo platform adapters]
  Platform --> Core[@multica/core]
  Core --> API[Fixed cloud API]
  Core --> WS[Realtime WS]
  Platform --> SecureStore[SecureStore token]
  Platform --> AsyncStorage[AsyncStorage cache]
  Platform --> Notifications[Expo Notifications]
```

## Work Breakdown

| Area | Scope | Nature |
|---|---|---|
| Scaffold | `apps/mobile`, Expo config, EAS config, icon | New app |
| Platform | storage, auth bootstrap, upload, notifications, linking | New mobile adapters |
| Core reuse | query/mutation usage, potential upload/push client methods | Small shared additions |
| Navigation | tabs, stacks, deep-link routing | New mobile app code |
| Screens | auth, issues, issue detail, projects, mine, search | New RN UI |
| Offline/retry | persisted query cache, failed mutation queue | New mobile app code |
| Realtime | backoff, dedup, compensation placeholder | Shared/mobile integration |
| Verification | unit tests and Android manual checklist | Tests/docs |

## Phase 0: Preparation

### Task 0.1 Confirm fixed cloud service

Input required:

- API base URL
- WebSocket URL
- Whether upload endpoint is same host

Output:

- `apps/mobile/src/app/env.ts`
- EAS env variable strategy

### Task 0.2 Confirm server contracts

Check or add placeholders for:

- push device token registration
- realtime message cursor / `msgID`
- compensation pull after `msgID`
- deep-link target resolution if client-side resolution is insufficient
- RN multipart upload compatibility

## Phase 1: Expo Scaffold

### Task 1.1 Create `apps/mobile`

Expected files:

```text
apps/mobile/
├── app.json
├── eas.json
├── package.json
├── tsconfig.json
├── assets/icon.png
└── src/app/App.tsx
```

Requirements:

- Expo + React Native + TypeScript.
- App name `Multicam`.
- Android package `com.wujieai.multica`.
- Scheme `wujieai_multicam`.
- Icon copied from `apps/desktop/resources/icon.png`.
- Add `@multica/mobile` scripts:
  - `dev`
  - `android`
  - `typecheck`
  - `test`
  - `lint`

### Task 1.2 Wire monorepo

Update:

- `pnpm-workspace.yaml` if needed.
- root `package.json` scripts if needed:
  - `dev:mobile`
- Turborepo tasks should include mobile typecheck/test/lint once stable.

Verify:

- `pnpm --filter @multica/mobile typecheck`

## Phase 2: Platform Foundation

### Task 2.1 Storage adapters

Implement:

- AsyncStorage adapter for non-sensitive Zustand/query persistence.
- SecureStore adapter for token.

Rules:

- Token never goes to AsyncStorage.
- Logout clears token only; user data cache can remain.

### Task 2.2 Core provider bootstrap

Implement mobile provider that initializes:

- ApiClient with fixed API base URL.
- auth token from SecureStore.
- QueryProvider.
- WS provider with fixed WS URL.
- workspace identity after workspace selection/deep-link resolution.

### Task 2.3 Navigation adapter

Implement mobile adapter compatible with shared navigation concepts:

- `push`
- `replace`
- `goBack`
- current path abstraction where needed

Keep React Navigation imports inside `apps/mobile`.

## Phase 3: Auth and Workspace

### Task 3.1 Email code login

Screens:

- email input
- code input
- loading/error states

APIs:

- `sendCode`
- `verifyCode`
- `getMe`

Acceptance:

- Successful login stores token in SecureStore and loads workspace list.

### Task 3.2 Workspace switcher

Screens/components:

- workspace header control
- bottom sheet or modal selector

Acceptance:

- Switch updates active workspace and clears workspace-sensitive filters where required.

## Phase 4: Issues Board

### Task 4.1 Issue data hooks

Use existing:

- `issueListOptions`
- `childIssueProgressOptions`
- issue view store filters
- issue scope store

Adapt as needed for RN.

### Task 4.2 Mobile board screen

Implement:

- status tabs/segmented control
- vertical issue card list per status
- filter sheet
- search entry
- FAB create issue

Acceptance:

- No horizontal scrolling required for card content.
- Board preserves current filter semantics.

### Task 4.3 Create issue

Implement:

- title
- description
- priority
- status
- assignee
- project
- parent issue where practical

Acceptance:

- Created issue appears on board and web/desktop.

## Phase 5: Issue Detail

### Task 5.1 Detail summary

Show:

- title/identifier
- status
- priority
- assignee/creator
- project
- due date
- description

Allow key property edits where supported.

### Task 5.2 Comments and timeline

Implement:

- comments list
- create comment
- reply if supported
- update/delete if permitted
- timeline display where useful

### Task 5.3 Reactions

Implement:

- issue reactions
- comment reactions

### Task 5.4 Attachments

Implement:

- image/document picker
- upload helper
- issue/comment attachment linking
- retry failed upload
- preview/download

### Task 5.5 Child issues

Implement:

- child issue list
- child progress
- navigation to child detail

### Task 5.6 Agent transcript

Implement:

- task run list
- task messages
- transcript view

Acceptance:

- First official version includes all detail capabilities, even if internal milestones phase them.

## Phase 6: Projects

### Task 6.1 Project list

Implement:

- list projects
- single-column cards
- progress, priority, status, lead, created/updated metadata

### Task 6.2 Project filter/sort

Align with current web capability:

- status filter if exposed by API
- sort by priority/status/created/updated/title

### Task 6.3 Project detail

Implement:

- project summary
- associated issue access where current APIs allow

## Phase 7: Search

### Task 7.1 Issue/project search

APIs:

- `searchIssues`
- `searchProjects`

Behavior:

- debounce input
- cancel stale requests
- show grouped results
- navigate to selected issue/project

Deferred:

- command palette commands
- recent pages
- copy link/theme commands

## Phase 8: Mine Read-Only

### Task 8.1 Mine home

Show:

- account identity
- logout
- links to Runtimes, Agents, Inbox

### Task 8.2 Runtimes read-only

Use:

- `listRuntimes`

Show status, provider/model metadata where available.

### Task 8.3 Agents read-only

Use:

- `listAgents`

Show active/archive status and runtime linkage where available.

### Task 8.4 Inbox read-only

Use:

- `listInbox`
- optional unread count

No management actions in first version unless explicitly accepted later.

## Phase 9: Offline, Retry, Realtime

### Task 9.1 Query persistence

Persist selected Query cache to AsyncStorage.

Prioritize:

- issues
- issue detail
- comments/timeline
- projects
- agents/runtimes/inbox

### Task 9.2 Failed mutation retry

Implement a local retry queue for:

- comments
- reactions
- issue updates
- attachment uploads

Acceptance:

- Failed writes are visible and retryable.

### Task 9.3 Realtime hardening

Implement:

- exponential backoff
- foreground/background handling
- recent 200 `msgID` dedup window
- compensation pull placeholder or API integration

## Phase 10: Push and Deep Links

### Task 10.1 Deep-link parser/router

Support:

- workspace
- issue
- project
- comment

Tests:

- parser unit tests
- auth/no-access/not-found routing cases

### Task 10.2 Expo Notifications

Implement:

- permission request
- Expo push token fetch
- token registration call or placeholder
- notification open routing through deep-link resolver

## Phase 11: UI Polish and Validation

### Task 11.1 Mobile design pass

Apply:

- web-aligned neutral theme
- bolder board separators
- solid status backgrounds where helpful
- no horizontal card content scroll
- safe area support

### Task 11.2 Android build

Create Android artifact with EAS or local Expo build flow.

Validation checklist:

- install artifact
- login
- workspace switch
- issue board
- create issue
- issue detail comments/reactions/attachments
- project list/detail
- search
- Mine read-only
- offline read cache
- realtime reconnect
- push open
- deep link open

## Delivery Milestones

| Milestone | Scope | Exit criteria |
|---|---|---|
| M0 | Scaffold and platform adapters | App boots, token storage works, fixed API configured |
| M1 | Login/workspace/tabs | User reaches Issues/Projects/Mine after login |
| M2 | Issues board/create/search | Board works with filters and create issue |
| M3 | Issue detail core | Summary, comments, reactions, attachments, children |
| M4 | Agent transcript/realtime/offline retry | Detail is official-release complete |
| M5 | Projects/Mine | Project screens and read-only Mine screens complete |
| M6 | Push/deep links/Android artifact | Android validation artifact ready |

## Acceptance Criteria

- Android artifact installs and launches as Multicam.
- App uses package `com.wujieai.multica`.
- Email code login works and token is stored securely.
- Issues board supports mobile-friendly status lanes and current filters.
- Vertical lists do not require horizontal scrolling to read item content.
- Issue detail includes the complete official-release scope.
- Search supports issue/project search.
- Mine exposes Runtimes/Agents/Inbox as read-only.
- Offline cached content remains readable.
- Failed writes have explicit retry.
- WS reconnects with backoff and dedups recent events.
- Deep links route workspace/issue/project/comment with clear auth/no-access handling.
- Push notification open routes to the correct resource where supported.

## Risks and Dependencies

| Risk | Dependency | Mitigation |
|---|---|---|
| Push token endpoint missing | Server API | Add typed placeholder and server follow-up |
| Realtime compensation missing | Server event cursor | Implement API after confirming `msgID` semantics |
| RN multipart upload incompatible | Server upload handler | Add RN upload adapter and backend test |
| Board too dense | Product/UI | One-status-at-a-time vertical board |
| Query persistence grows too large | Cache policy | Persist only selected keys and prune stale cache |

