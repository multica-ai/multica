# Multicam Mobile Technical Specification

> Status: Draft
> App target: `apps/mobile`
> Product name: Multicam
> Android package: `com.wujieai.multica`
> Last updated: 2026-04-27

## 1. Architecture Summary

Multicam Mobile should be implemented as a new Expo React Native app inside the existing monorepo.

The app should reuse headless business logic from `@multica/core`, but it should not reuse DOM-based views from `@multica/views` or shadcn/Base UI components from `@multica/ui`.

```mermaid
flowchart TD
  Mobile[apps/mobile Expo RN] --> MobilePlatform[mobile platform adapters]
  Mobile --> RNViews[RN screens/components]
  MobilePlatform --> Core[@multica/core]
  Core --> Query[TanStack Query]
  Core --> Zustand[Zustand stores]
  Core --> API[ApiClient]
  Core --> WS[WSClient]
  API --> Cloud[Fixed cloud service]
  WS --> Cloud
  MobilePlatform --> SecureStore[expo-secure-store]
  MobilePlatform --> Notifications[expo-notifications]
  MobilePlatform --> Linking[expo-linking]
```

## 2. Technical Choices

| Area | Decision | Reason |
|---|---|---|
| Framework | Expo + React Native + TypeScript | Fast Android validation, EAS-compatible, future iOS path |
| App location | `apps/mobile` | Fits monorepo app layout |
| Business logic | Reuse `@multica/core` | Keeps API, query, mutation, and store contracts aligned |
| UI layer | Native RN screens/components | Web components depend on DOM/Base UI/shadcn |
| Navigation | React Navigation | Mature stack/tab/deep-link support |
| Secure token | `expo-secure-store` | SecureStore/Keychain requirement |
| Push | `expo-notifications` | Required push approach |
| Offline/query cache | TanStack Query persistence + AsyncStorage | Aligns with existing Query ownership of server state |
| Local non-sensitive storage | AsyncStorage | Zustand preferences and query persistence |
| Attachments | Expo DocumentPicker/ImagePicker/FileSystem + RN `FormData` adapter | Mobile file source support |
| Build | EAS Build, Android-first | Android artifact is first validation target |

## 3. Proposed Workspace Layout

```text
apps/mobile/
├── app.json
├── eas.json
├── package.json
├── tsconfig.json
├── assets/
│   └── icon.png
└── src/
    ├── app/
    │   ├── App.tsx
    │   ├── providers.tsx
    │   └── env.ts
    ├── navigation/
    │   ├── root-navigator.tsx
    │   ├── linking.ts
    │   └── navigation-adapter.tsx
    ├── platform/
    │   ├── storage.ts
    │   ├── secure-token-storage.ts
    │   ├── upload.ts
    │   ├── notifications.ts
    │   └── realtime.ts
    ├── screens/
    │   ├── auth/
    │   ├── issues/
    │   ├── projects/
    │   ├── mine/
    │   └── search/
    ├── components/
    │   ├── ui/
    │   └── domain/
    ├── theme/
    │   ├── tokens.ts
    │   └── use-color-scheme.ts
    └── test/
```

## 4. Package Boundary Rules

Follow existing repo architecture with mobile-specific additions:

- `packages/core/` remains headless:
  - no `react-dom`
  - no direct `localStorage`
  - no `process.env`
  - no RN or Expo imports
- `apps/mobile/src/platform/` owns Expo/RN APIs:
  - SecureStore
  - AsyncStorage
  - Expo Notifications
  - Expo Linking
  - Document/Image picker
- Shared server state remains in TanStack Query.
- Client preferences remain in Zustand stores under `packages/core`.
- Mobile-only visual components stay in `apps/mobile/src/components`.
- Mobile-only navigation wiring stays in `apps/mobile/src/navigation`.

## 5. Environment Configuration

The mobile app connects to one fixed cloud service in the validation phase.

Required config:

```ts
export const MOBILE_ENV = {
  apiBaseUrl: "https://<fixed-cloud-host>",
  wsUrl: "wss://<fixed-cloud-host>/ws",
  appScheme: "wujieai_multicam",
};
```

Rules:

- Do not expose environment switching UI in the first version.
- Keep config in Expo constants or build-time env.
- Use the same base URL for API and file upload.

## 6. Authentication

Use existing email verification endpoints:

- `POST /auth/send-code`
- `POST /auth/verify-code`
- `GET /api/me`

Token handling:

- Store token in `expo-secure-store`.
- Inject token into `ApiClient.setToken(token)` at app boot.
- On logout:
  - clear SecureStore token
  - clear in-memory auth state
  - keep non-sensitive cached user/workspace data

Token refresh:

- Not implemented in mobile.
- Server-owned token lifecycle can be added later.

## 7. Core Provider Adaptation

`@multica/core` already accepts a `StorageAdapter` in `CoreProvider`.

Mobile needs:

- Async storage adapter for non-sensitive store persistence.
- Secure token adapter or auth bootstrap path for token hydration.
- Mobile navigation adapter equivalent to web/desktop platform layers.
- Optional mobile upload adapter because RN file objects are not browser `File`.

Potential core change:

```ts
export interface CoreProviderProps {
  storage?: StorageAdapter;
  tokenStorage?: StorageAdapter;
  apiBaseUrl?: string;
  wsUrl?: string;
  cookieAuth?: boolean;
}
```

If adding `tokenStorage` is too invasive, mobile can initialize `ApiClient` directly in its platform provider while preserving the existing singleton contract.

## 8. API Surface

Use the existing `ApiClient` where possible.

### Required Existing Calls

| Domain | API |
|---|---|
| Auth | `sendCode`, `verifyCode`, `getMe`, `logout` |
| Workspace | `listWorkspaces`, `getWorkspace` |
| Issues | `listIssues`, `searchIssues`, `getIssue`, `createIssue`, `updateIssue`, `listChildIssues`, `getChildIssueProgress` |
| Issue detail | `listComments`, `createComment`, `listTimeline`, `listTaskMessages`, `listTasksByIssue`, `listAttachments` |
| Reactions | `addIssueReaction`, `removeIssueReaction`, `addReaction`, `removeReaction` |
| Projects | `listProjects`, `searchProjects`, `getProject` |
| Mine | `listRuntimes`, `listAgents`, `listInbox` |
| Upload | `/api/upload-file` through a mobile-compatible upload helper |

### Placeholder/Missing Calls

These should be represented as typed client methods early, even if the server implements them later:

```ts
registerMobileDeviceToken(input: {
  token: string;
  platform: "ios" | "android";
  deviceId?: string;
}): Promise<void>

listRealtimeEventsAfter(input: {
  workspaceId: string;
  afterMsgId: string;
  limit?: number;
}): Promise<{ events: RealtimeEnvelope[] }>

resolveDeepLink(input: {
  workspaceSlug: string;
  targetType: "workspace" | "issue" | "project" | "comment";
  targetId?: string;
  commentId?: string;
}): Promise<ResolvedDeepLink>
```

## 9. Realtime Design

Use the existing WebSocket client as the baseline.

Mobile-specific requirements:

- Exponential backoff reconnect:
  - initial: 1s
  - multiplier: 2
  - max: 30s
  - reset after stable connection
- Pause/reconnect around app foreground/background transitions.
- Keep last received `msgID` per workspace when server supports it.
- On reconnect, request compensation events after the last local `msgID`.
- Deduplicate using the latest 200 message IDs.

Assumption:

- Server `msgID` is globally increasing. If not true, server must expose a stable event cursor per workspace.

Dedup policy:

```text
if msgID exists in recentWindow:
  ignore
else:
  handle event
  append msgID
  trim recentWindow to 200
```

## 10. Offline Cache and Retry

Server state remains owned by TanStack Query.

Cache strategy:

- Persist selected Query cache data in AsyncStorage.
- Cache issue list, issue detail, comments/timeline, projects, agents, runtimes, inbox.
- Search results can be cached opportunistically, keyed by workspace and query.
- Show stale state when offline.

Write strategy:

- Mutations should be optimistic only when rollback is simple.
- Failed writes must show a retry action.
- Attachment upload failures remain in a local failed-upload list until retried or dismissed.
- Full conflict-resolution offline editing is not part of first validation.

Suggested retry queue fields:

```ts
interface FailedMutation {
  id: string;
  workspaceId: string;
  type: "comment" | "reaction" | "issue-update" | "attachment-upload";
  payload: unknown;
  createdAt: string;
  retryCount: number;
  lastError?: string;
}
```

## 11. Deep Linking

Scheme: `wujieai_multicam://`

Supported patterns:

```text
wujieai_multicam://workspace/:workspaceSlug
wujieai_multicam://workspace/:workspaceSlug/issue/:issueId
wujieai_multicam://workspace/:workspaceSlug/project/:projectId
wujieai_multicam://workspace/:workspaceSlug/issue/:issueId/comment/:commentId
```

Flow:

1. Parse link.
2. If unauthenticated, store pending link and navigate to login.
3. After login, resume pending link.
4. Resolve workspace.
5. Fetch target resource.
6. Navigate or show explicit no-access/not-found state.

## 12. Push Notifications

Use Expo Notifications.

Flow:

1. Ask permission after login or first entry to Mine settings.
2. Get Expo push token.
3. Register token with server.
4. Handle notification receive/open.
5. Convert notification payload into a deep link target.

Payload recommendation:

```json
{
  "type": "issue_comment",
  "workspace_slug": "acme",
  "issue_id": "uuid",
  "comment_id": "uuid"
}
```

Trigger scenarios:

- Issue assigned.
- Comment or mention.
- Agent run completed.
- New inbox item.
- Runtime status change.

## 13. Attachment Upload

Use existing `/api/upload-file` when possible.

Mobile upload helper responsibilities:

- Convert Expo picker result to RN `FormData` part:
  - `uri`
  - `name`
  - `type`
- Attach optional `issue_id` or `comment_id`.
- Include auth and workspace headers.
- Expose progress when possible.
- Support retry after failure.

Potential compatibility issue:

- Existing `ApiClient.uploadFile(file: File)` assumes browser `File`. Add a parallel mobile upload helper rather than polluting core with Expo types.

## 14. UI Technical Standards

Mobile UI should translate current design tokens into RN theme tokens.

Rules:

- No horizontal scrolling for normal vertical lists.
- Use one-column cards for issues/projects.
- Use wrapping metadata rows or chips.
- Keep type scale compact.
- Use semantic colors, not decorative palettes.
- Board uses stronger borders and solid status backgrounds where needed.
- Touch target minimum should be close to 44px for primary interactive controls.
- Respect safe areas.

Suggested mobile issue card fields:

```text
Identifier + priority/status
Title
Project / assignee / due date
Child progress / comment count / attachment count
```

If content is too dense, secondary metadata should wrap or move behind a details affordance.

## 15. Board Implementation

Recommended first implementation:

- Status tabs at the top.
- Active status renders a vertical FlatList.
- Board filters stay in a bottom sheet.
- Status counts appear in tabs.
- Changing status happens through:
  - issue detail property picker
  - issue card quick action sheet

This preserves board semantics while avoiding a wide kanban layout that is hard to use on phones.

## 16. Testing Standards

Minimum tests:

- Unit tests for deep-link parser.
- Unit tests for mobile storage adapters.
- Unit tests for realtime dedup window.
- Unit tests for retry queue reducer.
- Component tests for key screens where feasible.
- Manual Android validation for:
  - login
  - workspace switch
  - issue board
  - issue detail comments/reactions/attachments
  - search
  - push open
  - deep links
  - offline read cache

CI:

- Add `@multica/mobile` to Turborepo typecheck/lint/test once scaffolded.
- Avoid requiring Android emulator in default CI until dedicated mobile CI exists.

## 17. Build and Release

Validation target:

- Android artifact from Expo/EAS.

Expo config:

```json
{
  "expo": {
    "name": "Multicam",
    "slug": "multicam",
    "scheme": "wujieai_multicam",
    "android": {
      "package": "com.wujieai.multica"
    }
  }
}
```

EAS profiles:

- `development`: dev client if needed.
- `preview`: Android internal artifact.
- `production`: reserved for signed release later.

Signing:

- Can be deferred for first local/internal Android validation.
- Add signing and store metadata before public distribution.

## 18. Security Considerations

- Store auth token only in SecureStore/Keychain.
- Do not persist token in AsyncStorage.
- Clear token on logout.
- Keep cached user data non-sensitive and avoid storing raw attachment files unless required.
- Attachment URLs should honor backend authorization and expiry semantics.
- Deep links must not bypass workspace permission checks.
- Push payloads should avoid sensitive comment content unless product approves it.

## 19. Open Technical Questions

1. What is the fixed cloud API/WS host?
2. Does the server currently expose globally increasing realtime `msgID`?
3. Is there an existing push device token table or endpoint?
4. Does `/api/upload-file` accept RN multipart file parts without backend changes?
5. Should mobile add a typed deep-link resolver endpoint or resolve client-side from existing APIs?

