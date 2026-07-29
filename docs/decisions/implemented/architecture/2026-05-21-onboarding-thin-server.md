# Decision: Onboarding collects, the workspace entry orchestrates

Status: implemented

## Problem

Two successive refactors left onboarding with no single owner. `users.onboarded_at` was written from four handlers, each with different side effects. The same "seed an install-runtime issue" work ran from four call sites and was de-duplicated underneath by an advisory lock. The Connect and Skip paths through step 3 were asymmetric — one deferred the mark, the other did it immediately. The workspace modal queried the runtime list and picked the first entry, discarding whatever the user had chosen a screen earlier. And `onboarded_at` carried two meanings at once: "this user finished onboarding" and "show the welcome modal".

An intermediate design persisted the step-3 choice into `users.onboarding_runtime_id` and `users.onboarding_runtime_skipped` and had the workspace entry read those columns through a four-branch dispatcher. It worked, but it converted a transient in-memory intent into durable database state and moved backend complexity up rather than down: an `OnboardingService`, a `WorkspaceContentService`, an `EnsureOnboardingContent` endpoint, and a four-branch component on the frontend. A production audit found 33,172 users with a workspace and zero of them mid-flow (`onboarded_at IS NULL`), so those columns had no real users and could be removed.

## Decision

Onboarding steps 1–3 only collect. Finishing step 3 does three things: mark the user onboarded, write a signal to a transient store, and navigate.

`users.onboarded_at` is the only onboarding state column, and it means one thing: onboarding is finished. Two hard gates read it — `apps/web/app/[workspaceSlug]/layout.tsx` redirects a not-yet-onboarded user to `/onboarding`, and the onboarding page redirects an already-onboarded user into their workspace. Desktop reaches the same outcome through its overlay decision in `App.tsx` rather than a route replacement, because onboarding there is a `WindowOverlay` and not a router destination.

The welcome experience is triggered by `packages/core/onboarding/welcome-store.ts`, a non-persisted Zustand store with read-once-then-clear semantics. `packages/views/workspace/welcome-after-onboarding.tsx` consumes it on mount and renders nothing without a signal, which is why an invited user, a returning user, and a page refresh all see no modal.

The backend carries no onboarding business logic. It marks the user and stores the questionnaire. Everything the welcome flow creates — the Helper agent, the starter issues — goes through the ordinary `createAgent` and `createIssue` APIs from the frontend hook. The long-form English and Chinese copy those calls need (the Helper's instructions, the install-runtime issue body, the create-agent guide issue body) lives in TypeScript modules under `packages/views/onboarding/templates/`, not in i18n JSON, because it is multi-paragraph Markdown with lists and code blocks. Short strings — titles, subtitles, buttons, card labels — stay in i18n JSON.

Three things guard against creating a duplicate Helper when React mounts the hook twice: a `useRef` latch within a mount, the store's consume-once semantics across mounts, and a name lookup against the workspace's visible agents before creating one.

Because `onboarded_at` is already set when the welcome flow runs, a failure there never traps the user. The runtime path shows a blocking modal with a retry and an abandon action that clears the signal; the skip path's modal is dismissible and retries per card.

`AcceptInvitation` must keep its `MarkUserOnboarded` call. Without it the layout gate bounces every invited user back to `/onboarding`.

### Deprecated bootstrap endpoints

`server/internal/handler/onboarding_shim.go` keeps the two `BootstrapOnboarding*` handlers alive for installed desktop clients that have not yet auto-updated. All deprecated code is in that one file; the live path in `onboarding.go` contains none of it. The shim calls queries directly rather than reinstating the service layer, and its copy constants are a second copy of the templates under `packages/views/onboarding/templates/` — the two must stay in sync until the shim is removed. It goes away, along with its routes and its five regression tests, once `X-Client-Version` telemetry shows no active desktop calling those endpoints.

## Alternatives considered

**Persist the step-3 choice in two new user columns and dispatch on them at the workspace entry.** This was built first and then removed. It works, but it durably stores an intent that is transient by nature — the user's answer matters for the next thirty seconds and never again — and it paid for that with two new services, a new endpoint, and a four-branch dispatcher component. The production audit showing zero mid-flow users removed the last argument for durability.

**Pass the choice through navigation state.** Rejected because `NavigationAdapter`'s `push(path: string)` takes no state object, so `navigate(path, { state })` is not available to shared views. The transient store also gives a property navigation state would not: it is not persisted, so refreshing the page cannot replay the welcome modal, which matches the one-time nature of the experience.

**Keep `resolvePostAuthDestination` resolving on workspace presence first.** The intermediate design changed it to avoid bouncing mid-flow users back into onboarding. Reverted to onboarded-first, because the layout gate already forces that redirect when `onboarded_at` is null, so a workspace-first resolver only spends an extra navigation — and the mid-flow window is now just "closed the app between step 2 and step 3".

**Put the Helper instructions and starter issue bodies in i18n JSON.** Rejected. The instructions are 94 lines of Markdown and the issue descriptions are 60-plus lines with lists and code blocks. Translation tooling and JSON escaping are the wrong shape for that; a TypeScript module exporting `{ en, zh }` constants is not.

**Delete the bootstrap endpoints outright.** Rejected. There is a window between deploying the server and desktop clients finishing their auto-update, and during it an older client calling a deleted endpoint gets a 404 in the middle of onboarding, with no way forward.

## Consequences

The backend's onboarding surface is a mark and a questionnaire. Adding to the welcome experience is now frontend work against public APIs, with no new endpoint and no service layer.

The cost is a second copy of the onboarding copy for as long as the shim lives, and the discipline to update both. The shim's five regression tests exist to make an accidental divergence visible.

Migration 098, which added the two intermediate columns, was rewritten in place to drop them; its down migration is a documented no-op. Production never ran the add, so the drop is a no-op there, and `IF EXISTS` lets any development database converge.

`starter_content_state` stays as a column. The v3 backend never touches it, but older desktop builds read it to suppress a legacy import dialog. It can be dropped once those builds are end-of-life.

Both hard gates are currently covered by manual end-to-end checks rather than tests at the layout and `App.tsx` level.
