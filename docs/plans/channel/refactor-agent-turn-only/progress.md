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
