# @tutti-os/agent-activity-core

Shared agent activity state, orchestration rules, merge rules, and selectors for
Tutti agent UIs.

This package owns the frontend-side workspace engine and activity snapshot model
used by Desktop and Mobile surfaces. It does not know about Electron, React
Native, HTTP, SSE, DeviceLink, or daemon DTOs. Product-specific adapters execute
transport commands and normalize observations before they enter the engine.

## Package Boundary

`@tutti-os/agent-activity-core` is the shared model layer:

- defines sessions, messages, presences, snapshots, and event envelopes
- loads session lists and paged session messages through an adapter
- can retain live session event streams with reference-counted subscription
  lifecycle when a host adapter exposes that optional capability
- merges persisted and live messages with version-aware conflict handling
- projects exact-identity ephemeral conversations into the same snapshot,
  Turn, and Interaction vocabulary without creating a workspace engine
- analyzes normalized activity events into one inline-observation intent plus
  an explicit authoritative-reconcile requirement
- projects shared activation, prompt send, Goal Control, settings update, turn
  cancel, Interaction response, rename, pin, and batch-delete commands onto one
  typed host effect port
- executes the shared prompt state machine, including serialized required
  settings persistence before send
- exposes selectors such as `selectNeedsAttentionCount`

It intentionally does not render UI, open network connections directly, persist
state, or translate daemon/backend contracts. Those responsibilities belong to a
host adapter such as the desktop renderer adapter.

## Ephemeral Conversation Projector

`createAgentActivityEphemeralConversationProjector` supports transient,
surface-owned conversation lanes such as live Side:

```ts
const side = createAgentActivityEphemeralConversationProjector({
  workspaceId,
  agentSessionId: sideAgentSessionId,
  sourceAgentSessionId,
  provider,
  cwd
});

side.apply(normalizedEvent);
const { activitySnapshot, sessionTurns, interactions } = side.getSnapshot();
```

It is intentionally smaller than `createAgentSessionEngine`. It accepts only
already-normalized events for one exact identity, rejects forward sequence gaps,
and expires on identity mismatch or terminal state. It does not load,
reconcile, subscribe, persist, or recover data. The owning surface/controller
must dispose the transport and discard the projection when it expires.

## Session Engine

`createAgentSessionEngine` is the workspace-level orchestration loop described
in `docs/architecture/agent-gui-refactor-plan.md` (section 3.3). It is
React-free and host-agnostic:

```ts
import { createAgentSessionEngine } from "@tutti-os/agent-activity-core";

const engine = createAgentSessionEngine({
  identity: { workspaceId: "workspace-1", origin: "local-tuttid" },
  commandPort, // typed lifecycle effects plus host-specific command extensions
  scheduler, // host timer port, e.g. setTimeout-backed outside this package
  clock, // host clock port: { nowUnixMs() }
  diagnosticSink // optional instance-level diagnostics receiver
});
```

Engine rules:

- Instances are identified by the workspace + origin pair and injected
  explicitly. There is no module-level singleton; hosts running multiple
  runtimes against one workspace create one engine per origin.
- Normalized observations and advanced lifecycle intents enter through
  `dispatch(intent)`. Frontend activation enters through
  `engine.activateSession`. Existing-Session Goal Control enters through
  `engine.controlGoal`. Composer-option reads enter through
  `engine.loadComposerOptions`. These semantic methods derive workspace
  identity or target scope and hide reducer protocol from hosts. Activation
  owns its timestamps, the 120-second confirmation window, intent projection,
  and admission result while the caller retains the exact request and client
  submit identities used by optimistic state and idempotent retry. A live
  activation request identity is single-use: `activateSession` returns `true`
  only when the current dispatch creates a new activation record, never because
  an older record happens to match its Session and mode. Session mutations
  additionally allocate command identity, await exact settlement, return
  canonical results, and own the default timeout and caller cancellation.
  Cancellation aborts a mutation host effect; once delivery may have started,
  the mutation remains delivery-unknown rather than becoming a confirmed
  failure.
- The activation request identity is projected as `activationId` on the typed
  host effect. Pending activation diagnostics keep command settlement separate
  from the first Session snapshot observation, including bounded outcomes for a
  missing Session, workspace mismatch, stale new-Session evidence, and a match.
  This lets hosts report command and snapshot latency without treating repeated
  snapshots or a late command result as a second lifecycle transition.
- Reducers are pure and return new state plus command descriptions; the effect
  executor performs commands and feeds every settlement (success, failure,
  timeout) back into the loop as command-result intents.
- Hosts implement `AgentSessionEffectPort` for activation, prompt send,
  Goal Control, settings update, turn cancellation, Interaction response,
  rename, pin, and batch delete. The Engine owns command-to-capability
  projection and the settings-precondition state machine. The command port declares
  `kind: "typed"` and its `execute` callback receives only
  `EngineExtensionCommand`.
- Timing is never read inside reducers. Deadlines are `scheduleExpiry`
  commands handled by the expiry clock, which re-enters the loop with expiry
  intents through the injected host scheduler.
- `dispatch(intent, { batch: true })` coalesces high-frequency intents inside
  a 33ms frame window; a non-batched dispatch flushes the pending frame first
  so ordering is preserved.
- `getSnapshot()` / `subscribe()` expose the immutable state tree. React
  surfaces subscribe through the single `useEngineSelector` binding in
  `@tutti-os/agent-gui`.
- Product hosts use the semantic Engine mutation methods and must not construct
  mutation ids, timeout policy, or mutation-record reads.

The state tree includes lifecycle entities, message windows, prompt queue,
pending intents, composer options, runtime availability, reconciliation, and
attention/read state. Hosts must consume selectors or stable snapshot
projections instead of reading reducer maps from UI components.

## Adapter Contract

Business hosts implement `AgentActivityAdapter`:

```ts
import type { AgentActivityAdapter } from "@tutti-os/agent-activity-core";

export const adapter: AgentActivityAdapter = {
  async listSessions({ workspaceId, signal }) {
    return {
      sessions: await fetchSessionsForWorkspace(workspaceId, signal),
      presences: []
    };
  },

  async listSessionMessages(input) {
    return fetchMessages(input);
  },

  async loadComposerOptions(input) {
    return fetchComposerOptions(input);
  },

  // Optional: implement only when this host wants the core controller to manage
  // per-session live event streams. Hosts with a service/runtime event bus can
  // omit this method and push events into the controller themselves.
  async subscribeSessionEvents(input) {
    const stream = openSessionEventStream({
      workspaceId: input.workspaceId,
      agentSessionId: input.agentSessionId,
      afterVersion: input.afterVersion,
      signal: input.signal,
      onEvent: input.onEvent,
      onError: input.onError
    });

    return () => stream.close();
  },

  createSession: createAgentSession,
  sendInput: sendAgentInput,
  goalControl: controlAgentGoal,
  submitInteractive: submitAgentInteractiveResponse,
  deleteSession: deleteAgentSession,
  deleteSessions: deleteAgentSessions,
  renameSession: renameAgentSession,
  setSessionPinned: setAgentSessionPinned
};
```

Adapters should normalize external data into core types before returning it.
For desktop, the concrete example is
`apps/desktop/src/renderer/src/features/workspace-agent/services/desktopAgentActivityAdapter.ts`.

## Snapshot Model

The controller exposes an `AgentActivitySnapshot`:

```ts
{
  workspaceId: string;
  sessions: AgentActivitySession[];
  presences: AgentActivityPresence[];
  sessionMessagesById: Record<string, AgentActivityMessage[]>;
}
```

`load()` replaces `sessions` and `presences` from the adapter while preserving
cached `sessionMessagesById`. This lets a UI refresh session cards without
dropping message state that may already have arrived from a paged fetch or live
stream.

`getSnapshot()` and subscription callbacks return cloned snapshots so UI callers
cannot mutate controller state by accident.

When loaded or upserted session data is unchanged, the controller preserves the
current snapshot reference and does not notify subscribers.

## Composer Options Cache

`engine.loadComposerOptions({ targetKey, provider, section, ... })` caches
results in a single target key space with independent `core` and
`capabilities` entries. `core` owns model, reasoning, speed, permission, and
effective settings; `capabilities` owns skills, commands, and capability
catalog data. After non-empty boundary normalization, `targetKey` is an
**opaque** cache key: the engine never parses or derives meaning from it.
Callers pass the already-resolved directory target id; two distinct targets
that share a `provider` therefore keep isolated caches (no provider-dimension
fallback).

The semantic Engine method owns request identity, signature-aware cache reuse,
joining an identical in-flight request, supersession by a newer request, exact
settlement, caller abort, and engine disposal. Desktop and Mobile hosts call
this method instead of dispatching `composerOptions/loadRequested` and
subscribing to reducer state themselves.

`invalidateComposerOptions({ providers })` drops freshness markers so the next
non-forced load refetches, while the last known options stay renderable. It
filters by the `provider` stored inside each cached value, never by inspecting
the opaque `targetKey`.

## Submit Availability

The engine derives submit availability from canonical Turns and pending
Interactions. Hosts must not copy deprecated session-level lifecycle or submit
availability fields into the frontend.

A host whose command transport or access policy can differ per Session may
dispatch `session/runtimeAvailabilityChanged`. This ephemeral, session-scoped
fact is kept outside the canonical Session and blocks runtime-dependent
commands while the exact Session transport reconnects, is unavailable, or
shared access has been revoked. Omitted availability
defaults to available, so ordinary local runtimes retain their existing
behavior. A workspace-wide `engine/connectionChanged` event must not be used to
represent one remote Session's transport because that would also block
unrelated Sessions sharing the engine.

Provider adapters may also publish exact session-level `running`/`idle`
observations as `runtime_activity_update` before a canonical Turn identity
exists. The workspace event coordinator stores this ephemeral runtime activity
outside the canonical Session so consumers can bridge processing presentation
without fabricating a Turn. The coordinator consumes `occurredAtUnixMs` as a
monotonic fence, and a settled canonical Turn wins over an older `running`
observation. `idle`, session removal, or event-stream disconnect clears it;
canonical Turn state remains the lifecycle authority.

## Event Shape

Canonical streams emit a versioned `message_update`:

```ts
{
  workspaceId: "workspace-1",
  agentSessionId: "session-1",
  eventType: "message_update",
  data: {
    workspaceId: "workspace-1",
    agentSessionId: "session-1",
    eventType: "message_update",
    latestVersion: 12,
    acceptedCount: 1,
    messages: [/* canonical message snapshots */]
  }
}
```

Normalized provider text/reasoning streams may precede that confirmation with
an optimistic `message_delta`:

```ts
{
  workspaceId: "workspace-1",
  agentSessionId: "session-1",
  eventType: "message_delta",
  data: {
    workspaceId: "workspace-1",
    agentSessionId: "session-1",
    messageId: "message-1",
    turnId: "turn-1",
    role: "assistant",
    kind: "text",
    occurredAtUnixMs: 100,
    content: { operation: "append_text", text: "hello" },
    status: "streaming"
  }
}
```

Each host creates one
`createAgentActivityWorkspaceEventCoordinator` per workspace and passes
transport deliveries into it. The coordinator validates and cleans
`message_delta`, owns its optimistic projection over canonical
`sessionMessagesById`, and clears that projection after authoritative message
reads or Session removal. The generated `AgentActivityUpdatedEvent` input also
accepts:

- `turn_update`: atomically updates the canonical Turn and the cached Session's
  active-Turn reference
- `interaction_update`: updates the canonical durable interaction projection
- `session_reconcile_required`: asks the engine transport to reload the session
- `session_deleted`: removes the session through the engine tombstone flow
- `session_restored`: clears only that explicit deletion tombstone, then asks
  the engine transport to hydrate authoritative Session detail

Events with a different `workspaceId` are ignored. Unknown event types are
ignored.

The coordinator owns inline-message continuity, Engine observation intents,
Session tombstones, explicit restore admission, discontinuity reconciliation,
and reconnect hydration.
Desktop receives the full canonical event union. The paired-device live
protocol carries only delta, Turn, Interaction, and audit variants; tuttid
converts canonical message and reconcile-required events into scoped
discontinuities. It preserves `session_deleted` and `session_restored` as typed
lifecycle deliveries so Mobile enters the same Engine tombstone/restore flow
as Desktop. Platform adapters
retain socket/DeviceLink lifecycle, diagnostics, Rail invalidation, and
navigation.

`eventStreamConnectionChanged` describes only event-stream continuity and
drives reconnect hydration. The host still owns `engine/connectionChanged`,
which describes command-transport reachability. Desktop may derive both from
one WebSocket connection. Mobile must derive Engine state from
application/service command reachability and coordinator state from
`stream_ready`/disconnect frames; it must not synthesize one from the other.

The coordinator validates each realtime Turn projection and dispatches it as
one Engine intent. The lifecycle reducer applies the Turn, the owning Session's
`activeTurnId`, and downstream lifecycle decisions in one state transition. A
settled Turn may clear only its own active reference, so a delayed completion
cannot clear a newer active Turn. Cached canonical Turn versions fence stale
Session snapshots without copying event-envelope time into Session timestamps.
Invalid projections are not partially applied, and attention observes only the
Turn accepted by canonical lifecycle monotonicity.

The same intent requests a state-only `session/reconcile` with `live: true`.
Command adapters preserve that flag on `session/detailSnapshotReceived`; failed
commands retain it for the next retry. Reconciliation fills fields outside the
realtime projection and establishes Session identity when the Session was not
cached. It is a convergence path, not a prerequisite for keeping a cached Turn
and Session reference consistent.

## Typed Effect Execution

The Engine projects shared lifecycle command descriptions onto
`AgentSessionEffectPort`: `activateSession`, `sendInput`,
`controlGoal`, `updateSessionSettings`, `cancelTurn`, and
`respondToInteraction`. Hosts implement
transport and result mapping without switching on those command types. When a
queued prompt includes
a required settings patch, the Engine records a prompt continuation and enters
that patch into the same per-Session settings lane as direct UI changes and
post-activation settings persistence. Only one settings write for a Session
reaches the host at a time. Owner boundaries are queue barriers, so updates
from activation, prompt preparation, and direct UI changes are never
accidentally coalesced together. A validated settings result first updates the
canonical Session, then starts the prompt send, and only then releases later
settings writes. A failed or timed-out precondition fails the logical prompt
without attempting delivery. Capability references, structured content,
display prompt, guidance, activation placement, Tutti-mode intent, and
diagnostics survive the shared projection.
Goal-on-create and settings command/correlation identities are also retained
for external hosts that use them for goal setup or idempotency. A typed
new-Session Goal is part of activation: hosts forward `initialGoalControl` and
empty initial content to their Create transport, and Agent Host creates the
Session plus durable Goal operation without creating a Turn.
Hosts that already observe that durable operation may attach the optional,
read-only `goalSyncState` projection to the Session. The field carries only the
revision, sync status, pending operation identity, and optional Host-owned
`executionPending` proof. Omission means the host cannot prove progress;
consumers must not reinterpret it as idle or successful.
For loading continuity, `pending`, `applying`, and `unknown` require a non-empty
pending operation identity. A `synced` mutation keeps the initial-Goal bridge
only when `executionPending` is explicitly true. The Host clears that proof on
the first canonical Turn with exact Goal provenance or when the Goal becomes
terminal, diverged, failed, or otherwise non-executing. Missing proof fails
closed, including for mixed-version hosts.
Engine Session merging preserves known state across compatible projections that
omit the optional field, while an explicit `null` clears it.
Existing-Session Goal Control is a separate Engine operation. The caller
proposes a stable client-submit identity; admission returns the effective
identity actually used by the Engine. The Engine owns command identity,
one-in-flight admission, the 30-second timeout, optimistic projection, typed
Session/Goal result validation, and delivery-unknown identity reuse. Hosts only
perform transport and result mapping. A successful typed result treats its
top-level `goal` as the authoritative durable desired projection and normalizes
the returned Session to the same value. Provider observation remains in
`state.observed`, so an empty pause/resume observation cannot erase a Goal;
only a durable tombstone produces `goal: null`. This also preserves an explicit
clear even when a runtime Session snapshot still carries the previous Goal.
Pending/applying results keep their older canonical Session until Host reports
synced state. Every admitted action reaches Host so it can create the durable
revision and audit, even when the visible Goal value is already equal.
Pending/applying Host state is accepted; definitive protocol
rejection is failed; transport loss, timeout, opaque/malformed success, and
unknown/diverged Host state remain unknown. Retrying the same unknown action
reuses its original `clientSubmitId`, and the admission result tells the
surface to correlate settlement with that identity. A definitive failure
releases the old identity so an explicit retry creates a new operation.
Generic Session reconciliation and Goal
value equality cannot prove a particular operation. The latest operation is
bounded to one record per Session, is removed with that Session, and is exposed
from `getSnapshot()` and the package root only through derived
presentation/settlement state and narrow selectors; the reducer ledger is not
present in the public runtime object. Those public maps are sparse and updated
only for Session IDs whose canonical Goal, Goal operation, or Goal-bearing
activation changed. Turn activity and unrelated Session metadata preserve the
Goal branch and unaffected presentation references.
Rename and pin effects return `{ session }` with the authoritative canonical
Session, while batch delete returns `AgentActivityDeleteSessionsResult`.
Runtime validation remains fail-closed, but the public port type prevents hosts
from implementing a different result envelope.

Prompt command ordering is an Engine implementation detail. Consumers implement
`AgentSessionEffectPort`; the package root does not expose the internal prompt
execution helper or its precondition port. The reducer-only continuation is not
part of public `EngineIntent`, and its execution ledger is omitted from
`AgentSessionEngineState`, `getSnapshot()`, and subscription callbacks.

Host-only commands such as Desktop attention persistence or the
composer-options transport call remain in an `EngineExtensionCommand` adapter.
The public `engine.loadComposerOptions` method owns the shared load/cache
protocol; the extension adapter only maps the resulting command to host
transport. The Engine, not the host adapter, decides whether a validated
settings result requires a provider-declared options refresh. That refresh is
target-scoped and non-blocking for the current send. Timeout, abort,
observation, and command-result dispatch remain owned by the Engine effect
executor. Every typed effect receives its own Engine command's `AbortSignal`;
hosts must propagate it through their transport.

## Message Merge Rules

Messages are keyed by `messageId` within a session.

- Higher `version` replaces lower `version`.
- If versions are equal, higher or equal numeric `id` replaces the existing
  message.
- Replacement merges payload fields instead of discarding existing payload keys.
- Final message order is `version`, then `id`, then `messageId`.

These rules let stale paged responses arrive after fresher live events without
overwriting the user's current view.

## Retained Streams

Use `retainSessionEvents()` when a UI surface needs live updates for a session:

```ts
const release = controller.retainSessionEvents({
  agentSessionId: "session-1",
  onError: reportStreamError
});

release();
```

Lifecycle behavior:

- Multiple consumers of the same session share one adapter subscription.
- Each `release` callback is idempotent.
- The adapter stream is aborted and unsubscribed after the last consumer
  releases it.
- If subscription setup fails, the retained stream is cleaned up so a later
  caller can retry.
- When `afterVersion` is omitted, the controller subscribes after the latest
  cached message version for that session.

## Needs Attention

`selectNeedsAttentionItems(snapshot)` returns pending user-action items sorted
newest first. `selectNeedsAttentionCount(snapshot)` returns its length.

The selector treats non-terminal messages as actionable when they look like:

- permission or approval requests
- direct user questions
- constraint requests
- waiting assistant/system messages that do not match a more specific category

Terminal statuses such as `completed`, `failed`, `answered`, and `resolved` are
not counted.
