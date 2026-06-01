Purpose: Verify that DeepSeek TUI backend integration works as an agent runtime, using the native thread protocol instead of ACP, and remains discoverable after runtime config changes.

Preconditions: The Multica web app is reachable. The user is signed in. A runtime with DeepSeek-TUI provider is configured and available. An agent is created or configured to use the DeepSeek-TUI runtime.

User flow: Navigate to the Agents page. Create a new agent or edit an existing one. In the runtime picker, select a DeepSeek-TUI runtime. Save the agent configuration. Create an issue and @mention this agent. Wait for the agent to execute. Open the Runtime/Machines page and search for the runtime owner or runtime name.

Expected results: The DeepSeek-TUI runtime appears in the runtime picker with the correct label (provider shown as `DeepSeek-TUI`). The agent can be assigned to this runtime without errors. When triggered, the agent executes using the native thread protocol (not ACP). The execution produces output visible in the issue timeline. The execution log shows the agent's responses. The runtime picker sorts owned runtimes above others. Runtime search can find the DeepSeek-TUI runtime by owner username and runtime name, and the runtime list displays owner avatar/name where available.

Notes for automation: The runtime picker lists available runtimes with provider labels. Select by visible text matching `DeepSeek-TUI` or the runtime name. Execution verification requires waiting for the agent task to complete and checking for output in the timeline.
