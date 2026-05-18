# Progress: Channel agent-turn-only refactor

## Milestone 1 / 5

### Implemented
- Natural-language channel messages no longer fall back to rule intent resolution when the channel agent turn client or workspace context is unavailable.
- Slash/source-command input remains deterministic and continues through the rule command resolver path.
- Added regression coverage proving natural language does not use old rules without channel turn support.

### Approach
- Kept the routing boundary in `inbound.Runtime.resolveIntent`, because this is the single ingress point that decides whether a message is deterministic command input or an agent turn.
- Reused the existing failure-notice path so missing channel-agent support terminates the event without retry spam.

### Plan Delta
- No delta from the plan for this milestone.

### Follow-Up
- The old planner/task types and `intent` package naming still exist. They are scheduled for later milestones.
- Pending clarification/action state is not implemented yet. The screenshot flow still needs that milestone to remember that "STA-82" answers a prior cancellation clarification.

### Next
- Milestone 2: split command/turn/action boundaries and start removing old chat intent planner influence from package/API naming.

## Milestone 2 / 5

### Implemented
- Runtime config now accepts only `ChannelAgentTurnClient` for natural-language channel turns; `ChatIntent` and `TurnPlanner` are no longer part of inbound runtime wiring.
- Server/channel manager assembly now builds and passes the channel-turn client directly instead of routing through async chat-intent/planner adapters.
- Legacy waiting-agent rows with `wait_kind=intent` are marked dead instead of being resumed through the removed planner path.
- Empty `wait_kind` defaults to `channel_turn`, preventing accidental re-entry into legacy intent semantics.
- Removed the unused `ChannelTurnPlanner` interface and adapter methods.

### Approach
- Kept command rule resolution in place for slash/source commands only.
- Limited this milestone to runtime and assembly boundaries so old task types can be deleted separately with focused daemon/service tests.

### Plan Delta
- No material delta. The old chat-intent task implementation still exists behind unused methods and is scheduled for the next milestone.

### Follow-Up
- Delete or quarantine `ChatIntentClient`, `AsyncChatIntentClient`, `BuildChatIntentPrompt`, and channel-intent task enqueue/daemon prompt paths if no non-channel-turn caller remains.
- Rename the remaining command-rule code so `intent` no longer reads like the primary natural-language abstraction.

### Next
- Milestone 3: remove the old chat intent planner/task surface and update prompt tests around the agent-turn contract.

## Milestone 3 / 5

### Implemented
- Removed the old chat-intent resolver/client interfaces, classifier prompt builder, daemon classifier prompt path, and channel-intent task enqueue path.
- Renamed the task-backed channel client from chat-intent terminology to channel-turn terminology.
- Stopped daemon runtime capability registration from advertising `channel_intent`; only `channel_turn` remains.
- Removed `CreateChannelIntentTask` / `GetChannelIntentTaskByInboundEvent` SQL queries and regenerated sqlc output.
- Removed channel-intent claim response fields and execution-environment special cases, so channel turns receive normal CLI credentials.

### Approach
- Deleted creation/execution paths for old channel intent tasks while retaining historical SQL list filters that hide existing `context.type = channel_intent` rows from user-facing task lists.
- Kept command-rule parsing intact for slash/source command input only.

### Plan Delta
- The database filters still reference `channel_intent` for historical data hygiene. They do not create, resume, or execute old intent tasks.

### Follow-Up
- Milestone 4 still needs pending clarification/action state so a follow-up like `STA-82` can complete the prior "close it" clarification.
- The remaining `channel/intent` package split is handled in the final cleanup milestone below.

### Next
- Milestone 4: add pending clarification/action state for multi-turn channel repair flows.

## Milestone 4 / 5

### Implemented
- Added durable pending clarification/action state to channel turn result payloads.
- Channel agent output can include a hidden `<multica_channel_state>` block; runtime strips it before sending and merges the structured state into `channel_turn.result_payload`.
- The next turn loads the latest completed turn in the same connection/conversation/sender/thread scope and exposes active `PendingAction` to the channel agent prompt.
- The prompt now explicitly tells the agent that an issue-key-only reply must resolve the previous pending action before being interpreted as a new query.
- Added regression tests for pending state persistence, prompt stripping, prompt injection, and the `STA-82`-style candidate flow.

### Approach
- Used existing `channel_turn.result_payload` rather than adding a new table. Pending state belongs to the turn lifecycle and is naturally cleared when the latest completed turn has no `pending_action`.
- Kept language out of the resolver: no Chinese-only cancellation detector was added. The server persists structured action state, and the agent receives it language-neutrally.

### Plan Delta
- No database migration was needed. The planned "pending state" is implemented as structured turn result payload instead of a separate table.

### Follow-Up
- Split the remaining package responsibilities so command parsing, channel turn prompt/state, and structured action constants no longer live under the old `channel/intent` package.

### Next
- Milestone 5: final cleanup and verification.

## Milestone 5 / 5

### Implemented
- Removed the old `server/internal/channel/intent` package.
- Added `server/internal/channel/action` for dispatch-ready action kinds/sources.
- Added `server/internal/channel/command` for deterministic slash/source-command rule parsing.
- Added `server/internal/channel/turn` for agent-turn request, prompt, result-state parsing, and user-visible channel agent errors.
- Removed unused channel turn planner/composer interfaces that still referenced the old intent package.
- Renamed legacy wait-kind handling to `WaitKindLegacyIntent` to make historical support explicit.

### Approach
- Kept `port.InboundIntent` as the existing dispatch wire contract because dispatcher, authz, proposals, and tests already consume that field. The behavior boundary changed: ordinary natural language no longer writes that dispatch field; slash/source commands still do.
- Left `channel_intent` SQL filters in place only for historical task-list hygiene. No product path creates, resumes, claims, or executes that task type.

### Verification
- `cd server && go test ./internal/channel/...`
- `cd server && go test ./internal/channel/... ./cmd/server ./internal/handler ./internal/service ./internal/daemon ./internal/daemon/execenv`

### Final Review
- No `server/internal/channel/intent` package remains.
- No `BuildChatIntentPrompt`, `ChatIntentClient`, `AsyncChatIntentClient`, `ChannelTurnPlanner`, or `TaskBackedChatIntentClient` references remain in product code.
- Natural-language routing remains agent-turn-only; deterministic command parsing is still restricted to slash/source-command input.
- Historical `channel_intent` strings remain only in task-list SQL filters and generated SQL, by design.

## Milestone 6 / 6

### Implemented
- Added `/clear` as the single deterministic channel control command for resetting short-lived conversation context.
- Persisted the reset as structured `context_reset` state on a completed channel turn.
- Applied the latest reset boundary when loading pending clarification state and recent context entities for future agent turns in the same connection/conversation/sender/thread scope.

### Approach
- Do not delete messages, turns, issues, comments, or audit records.
- Do not ask the agent to interpret `/clear`; runtime handles it even when the channel agent is unavailable.
- Treat reset as a prompt/context boundary: old history remains queryable in storage, but it is no longer automatically injected into later turns.

### Plan Delta
- Removed `/new` from the proposal before implementation. `/clear` is less ambiguous in this product because `/new` can read as creating a new issue, workspace, or session.

### Verification
- `cd server && go test ./internal/channel/...`
