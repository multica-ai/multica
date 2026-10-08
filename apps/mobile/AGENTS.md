# Mobile App Instructions

These rules apply only to `apps/mobile/`, in addition to the [root instructions](../../AGENTS.md). Paths below are relative to `apps/mobile/` unless prefixed with `packages/` or `.github/`.

## Sharing and Dependencies

- Mobile owns its UI, state, hooks, providers, API client, QueryClient, i18n, and build/release workflow. Import core types with `import type`; runtime imports are limited to pure utilities and platform-independent schemas. Do not import web/desktop stores, hooks, Query factories, or WS updaters.
- Use `package.json` and the lockfile for current versions. Mobile pins Expo/React Native dependencies rather than taking the root React catalog.
- Add SDK-aligned native packages with `pnpm exec expo install <package>` from this directory. Check compatibility before adding other dependencies; do not pick versions from memory.
- Follow [README.md](README.md) for setup, simulator/device builds, and environment variants. Keep iOS scripts routed through `scripts/ios-run.sh`, which prebuilds before running iOS so config plugins are reapplied. Preserve the caller's `APP_ENV`; avoid clean prebuilds in the normal edit loop.
- Generated `ios/` and `android/` directories are not source. Check new source paths with `git check-ignore -v <path>` when they could match the root ignore rules, particularly `data/`, `build/`, and `bin/`.

## Behavioral Parity

- Before implementing a feature, inspect its web/desktop implementation in `packages/views/` and `packages/core/`: endpoints, permissions, state transitions, display transforms, and cross-cache side effects.
- Match product semantics: counts/visibility, permissions, enums, and canonical IDs/fields. Adapt UI and navigation to the phone; document meaningful divergences with the source behavior they preserve.
- Mirror the relevant behavior using mobile-owned caches and helpers; do not copy cache shapes or UI preprocessing blindly. List transforms must preserve pagination, grouping, and visibility semantics.
- Reuse `lib/inbox-display.ts` for inbox list display. Badge counts come from the server unread summary via `lib/unread-counts.ts` and `data/queries/inbox.ts`; do not derive them from the downloaded list. Inbox writes/events refresh both the list and unread summary through `data/realtime/inbox-ws-updaters.ts`.
- An explicit implementation request authorizes work. Explain significant interaction choices and parity points briefly; ask when an unresolved product decision affects the implementation, not for a second approval using special wording.

## UI Components

- Inspect existing rows, pickers, forms, and domain visuals before adding components. Extend a suitable existing pattern; do not rewrite a domain component merely because a new feature uses it.
- For a new interaction, prefer a native iOS/RN API, then an RNR component. If neither fits, compose existing primitives inline for a local need. A new generic primitive requires at least three callers and no suitable native/RNR alternative; clarify unresolved interaction requirements before inventing one.
- Native examples: `Alert.prompt` for text prompts, `Alert.alert` for confirmation, `ActionSheetIOS` for action menus, existing native date/media/document pickers, `Share.share`, and `expo-haptics`.
- Add RNR components with `npx @react-native-reusables/cli@latest add <name>`. Review generated changes and preserve local customizations. Keep default variants/spacing unless a concrete product need requires changes.
- Generic primitives live in `components/ui/`; domain compositions live in `components/<domain>/`. Use the existing `cn()` in `lib/utils.ts` and semantic tokens.
- Screens have titles, tab bars have icons, secondary labels use type-aware helpers, and multiple trailing row elements stack vertically. Check long text, keyboard, safe areas, scrolling, and both themes.

### Theming model

- `global.css` defines mobile color variables; `tailwind.config.js` maps classes and `lib/theme.ts` supplies native style/navigation values. Update the corresponding mappings together when changing tokens.
- Preserve class-based light/dark/system switching through `lib/use-color-scheme.ts` and its secure-store persistence. React Navigation uses `NAV_THEME` from `lib/theme.ts`.
- Keep mobile tokens local; do not import web/desktop CSS. Read [docs/markdown-rendering-adr.md](docs/markdown-rendering-adr.md) before changing the Markdown renderer or its native styling.

### Sheets and navigation

| Content | Container |
| --- | --- |
| Confirmation or text prompt | Native alert/prompt |
| Short action menu | Native action sheet; reuse existing local popover patterns when appropriate |
| Long list, search, form, or keyboard interaction | Expo Router `presentation: "formSheet"` route |
| Multi-screen modal flow | Expo Router `presentation: "modal"` |

- All pickers in one attribute row use the same formSheet interaction, including short option lists.
- Place picker routes under their owning context and register them in `app/(app)/[workspace]/_layout.tsx` using `SHEET_OPTIONS`. Reuse that configuration rather than copying its current values; document necessary overrides at the route.
- Sheet bodies own their data lookup and mutation, then return with `router.back()`. Draft flows use the appropriate local draft store when there is no persisted record. Follow the existing body-header pattern unless the route explicitly requires native header chrome.
- Check the return destination when opening a sheet from another modal, and handle deep links without assuming the record is already cached.
- Destructive swipes reveal an action that requires a tap; never auto-execute on full swipe. Reuse `components/inbox/swipeable-inbox-row.tsx`, including its one-shot haptic feedback.

## Data Layer

Use the existing helpers rather than rebuilding request or subscription plumbing.

| Concern | Implementation |
| --- | --- |
| Requests | `data/api.ts`: `fetchValidated` for GET, `fetchValidatedWith` for writes with consumed response bodies |
| Response schemas/fallbacks | `data/schemas.ts`, `lib/parse-response.ts`; reuse compatible pure schemas from core |
| Query keys and options | `data/queries/`; mutations import the corresponding key factory |
| Query lifecycle | `data/query-client.ts`: AppState focus and NetInfo connectivity |
| Realtime lifecycle | `data/realtime/ws-client.ts`, `data/realtime/realtime-provider.tsx`, `lib/use-ws-subscriptions.ts` |

- UI-consumed responses follow the root API compatibility rules. Raw `fetch` is reserved for writes whose response body is unused. Fallback values must satisfy the expected success type.
- Forward each query's `signal` through the API method to fetch. Preserve the request timeout and caller-abort forwarding in `data/api.ts`; do not replace its cancellation implementation without checking the supported mobile runtime.
- Preserve the API client's request IDs/logging and the idempotent 401 cleanup in `app/_layout.tsx` (auth, workspace, Query cache, navigation). Mobile uses bearer auth; do not copy browser cookie/CSRF plumbing.
- Use feature key factories, with `wsId` for workspace data and separate account-level keys for cross-workspace summaries. Do not hardcode keys or require every key to have the same number of segments.
- **Optimistic-update exception:** inbox mark-read may patch synchronously before navigation to avoid an unread row in the iOS transition snapshot. Keep this inside the mutation, capture rollback state before patching, and refresh on settle. It is not permission to optimistically create/delete/leave or to generalize navigation-time optimism.
- Other mutations follow the root optimistic-update policy; message sends retain visible pending/retry states.

## Realtime

- Keep listing subscriptions mounted for the workspace session in `RealtimeSubscriptions` in `app/(app)/[workspace]/_layout.tsx`. Record subscriptions belong to the owning screen, filter by record ID, and clean up on unmount/ID changes.
- Use `useWSSubscriptions` and typed `ws.on`. When adding protocol events, update both `WSEventType` and `WSEventPayloadMap` in `packages/core/types/events.ts`; narrow unknown payloads rather than adding unchecked casts.
- Updaters belong to the feature whose cache they change. That feature subscribes to relevant foreign events; for example, inbox owns its reactions to issue status/deletion events.
- Patch when the payload and cache shape make the result determinate. Invalidate when information is incomplete, list membership is uncertain, or an infrequent event makes refetching simpler. Use existing refresh helpers that handle in-flight request races.
- Each feature refreshes its affected caches on reconnect, including any account-level projections it owns. Avoid a global refetch sweep or subscriptions with no mobile consumer.
- Accept authoritative WS updates over local optimistic state. Do not add timestamp gates to protect optimistic patches without a demonstrated ordering problem.

## Verification

From the repository root:

```bash
pnpm --filter @multica/mobile typecheck
pnpm --filter @multica/mobile lint
pnpm --filter @multica/mobile test
```

- Root frontend checks exclude mobile. `.github/workflows/mobile-verify.yml` defines the current mobile CI scope; these checks do not build an IPA or verify native rendering.
- For UI changes, verify the affected flow in the simulator/device, including themes, keyboard/scrolling, and navigation. For shared semantics or realtime changes, change the same data from web and confirm mobile catches up without manual refresh, including after reconnect.
- Test parsing/transforms in the existing Vitest setup. Preserve `scripts/ios-run.test.sh` coverage when changing the native build wrapper.
- Report which checks ran and which native/cross-client checks were unavailable. Do not claim visual or release verification from typecheck/unit tests alone.
