Purpose: Verify that Fork-added agent runtime providers and standardized streaming capabilities work across Copilot, OpenCode, Kiro, CodeBuddy, and stream-capable agents.

Preconditions: The Multica web app is reachable. The user is signed in. At least one runtime is registered for each provider being tested, or the test report clearly marks unavailable providers as fixture-blocked. A disposable issue exists for triggering agents.

User flow:
1. Open the Runtime/Machines page and verify Fork runtime providers appear with correct names/icons: Copilot, OpenCode, Kiro, Tencent CodeBuddy, and any configured DeepSeek-TUI runtime.
2. Create or edit one agent per provider and select the matching runtime.
3. Trigger each agent from an issue with a markdown mention.
4. While a run is active, open the run stream/trace panel and observe standardized trace output.
5. For OpenCode, trigger an approval-policy/tool-observation path if the fixture supports it.
6. For providers without real-time stream capability, verify the UI shows a clear degraded/non-streaming state instead of raw internal trace payloads.
7. After completion, open the execution log and verify final output, duration, and token usage where supported.

Expected results:
- Fork providers are selectable in the runtime picker and persist on the agent after save.
- Copilot and OpenCode standardized runtimes can execute tasks through the daemon.
- Provider capabilities gate real-time stream rendering: stream-capable providers show structured traces, while non-stream providers do not leak raw trace payloads.
- OpenCode tool-observation traces are emitted only for the intended approval-policy path and are not duplicated.
- Token usage is populated for supported providers and missing provider fields do not break run display.
- Degraded stream notices are styled as user-readable status, not raw logs.

Notes for automation: This case needs real local runtimes or mocked daemon fixtures. Do not mark provider-specific checks PASS if that provider runtime was absent; report provider-level BLOCKED with the daemon status and runtime list.
