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
- Remaining package name `channel/intent` still holds command intent structs and the agent-turn prompt. Renaming/splitting is the next cleanup boundary after pending-state behavior is in place.

### Next
- Milestone 4: add pending clarification/action state for multi-turn channel repair flows.
