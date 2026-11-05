# Agent conversation GUI

This package renders Multica's local conversations. Desktop owns navigation,
provider discovery, authentication and device transport. The GUI owns presentation
and component state; `packages/conversation-client` owns observable conversation
models and command services.

## Reading and rendering

`ConversationServicesProvider` scopes services to one mounted instance. A main
conversation and its Side conversation have separate providers. Production code
never resolves a last-mounted or module-global instance.

- `AgentSessionsModel` derives sessions, turns, interactions and admission state
  from the native conversation source at a committed revision. Its writes only
  remove and restore local pointers; it does not accept host snapshots.
- `TranscriptModel` indexes committed messages by conversation and turn.
- `ConversationRowsModel` projects one turn per computed value using the existing
  pure row builder. Pending input is a separate tail. Lists observe structure;
  `AgentTranscriptItemView` resolves current row content by key and message IDs.
- Queue components observe `PromptService` directly. Streamed assistant text does
  not participate in the page controller or composer props.
- `ActiveConversationState`, `SessionPresentationState` and composer/detail
  state classes own native-derived computed values. Consumers receive model
  handles; draft content remains separate client-owned props.
- Host subscriptions and connection-notice timers belong to
  `SessionPresentationHostService`. The presentation model performs no I/O.
  The rich text editor retains its controlled update boundary.

`AgentGUINode` composes the page provider and frame. Feature props are separate
shell, selection, readiness, operations, detail, interaction and composer groups.
Shell context carries identity and platform plumbing. Drafts remain in the
existing draft store, including content, attachments and settings restoration.

A consumer reads observable state during render:

```tsx
const SubmitState = observer(({ sessionId }: { sessionId: string }) => {
  const state = useActiveConversationState(sessionId);
  return <output>{state.isSubmitting ? "Sending" : "Ready"}</output>;
});
```

Keep model instances stable across draft keystrokes. Put native-derived work in
`computed`, not in React memo/effect dependencies. DOM measurement hooks accept
plain render inputs from the observer boundary. Keep transient dialogs, focus
and draft storage in their existing owners. Remove `"use memo"` from touched
observer components; do not wrap the rich text editor in `observer`.

Tests create real service/model instances with fake transports. Use separate
service scopes for main and Side. Assert that an update reaches its consuming
observer without rerendering a parent or sibling, and that unmounting releases
host listeners and timers. The canonical examples are
`state/ConversationPresentationState.spec.tsx` and
`state/ConversationRowsModel.observer.spec.tsx`.

## Commands and host boundaries

The provider exposes native `prompts`, `activations`, `sessionLoads`, `composer`
and `mutations` services. Operations flush the commit cadence before deciding
which turn to stop, answer or retry. Unknown send/create results retain their
identity and draft for recovery; they are never retried with a replacement ID.
Deletion awaits the server before selection or draft cleanup.

`conversations` loads session data and older turns through the native source.
Copying a complete transcript loads every older turn and rejects stalled paging.
`attachments` owns upload, pasted-text staging and image reads. Optional
`diagnostics` receives sanitized presentation diagnostics and settings events.

The desktop host supplies direct grouped methods for navigation, files, storage,
model discovery and native commands. Workspaces, focus, cancellation and lifetime
remain checked at that boundary. The MessagePort transport exists only in the
isolated browser harness.

The old activity engine, runtime snapshot API, provider rail and inactive embedded
Side implementation have been removed. Native Multica Side uses another GUI
instance. Read-only rendering of historical collaboration records remains;
adoption writes have no desktop implementation and are not exposed.

## UI and verification

The existing UI primitives and Multica host tokens remain the styling source.
Scrolling, virtualizer thresholds, entry animations, tool expansion and editor
behavior retain their existing contracts. No editor-wide observer is installed.

Tests live alongside their implementation. Run the complete GUI suite from the
repository root with:

```sh
node packages/tutti-agent-gui-host/scripts/test-gui.mjs
```

The host package's normal `test` command includes this suite. Its smoke tests mount
the real GUI with fake native services. Browser verification and isolated desktop
startup are described in
[local conversation view state](../../../../../docs/engineering/conversation-view-state.md).
