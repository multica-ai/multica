Purpose: Verify that the native Claude plan mode works correctly — the sidebar displays the plan, approval is gated to stream-enabled agents, and plan mode is limited to Claude models only.

Preconditions: The Multica web app is reachable. The user is signed in. An agent exists that uses a Claude model (e.g., Claude Sonnet) with streaming enabled. The runtime trace and approval bridge features are deployed.

User flow: Open an issue assigned to a Claude-based agent. Trigger a task (via @mention or other mechanism). Once the agent begins executing, observe the issue detail sidebar — a plan panel should appear showing the agent's execution plan. If the plan requires approval (e.g., for file edits or destructive actions), an approval prompt should appear in the sidebar. Approve or reject the action. Verify only agents with stream enabled show the plan sidebar.

Expected results: When a Claude agent runs with plan mode enabled, a sidebar panel shows the agent's step-by-step plan. The plan updates in real-time as the agent progresses. Approval requests appear as interactive prompts that the user can accept or reject. Non-Claude agents (e.g., GPT-based) do NOT show the plan sidebar (plan mode is limited to Claude). Agents without streaming enabled do NOT show plan mode. The plan bridge handles approval and trace events securely (no unauthorized access).

Notes for automation: The plan sidebar appears to the right of the issue timeline during agent execution. Check for the sidebar panel presence and its content updating. The approval prompt has Accept/Reject buttons. This feature requires a live agent execution to test fully.
