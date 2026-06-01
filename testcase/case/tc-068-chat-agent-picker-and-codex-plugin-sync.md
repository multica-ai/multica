Purpose: Verify that chat agent selection supports search and that Codex App plugin synchronization does not duplicate turns or lose submitted prompts.

Preconditions: The Multica web app is reachable. The user is signed in. The workspace has multiple chat-capable agents. Codex App plugin sync is available in the environment being tested, or the plugin sync path can be exercised with a fixture.

User flow:
1. Open Chat.
2. Open the agent dropdown and type part of an agent name.
3. Select a filtered agent and send a short chat message.
4. If Codex App plugin sync is enabled, submit a prompt through the plugin path and observe the corresponding Multica chat/turn.
5. Submit another prompt quickly after the first and wait for synchronization to settle.

Expected results:
- The chat agent dropdown filters agents by search text and keeps selected/current agents visible.
- Selecting a filtered agent updates the active chat target without resetting the message draft unexpectedly.
- Plugin-submitted prompts appear in Multica exactly once.
- Codex plugin turn synchronization waits for submitted prompts and does not create duplicate assistant/user turns.
- Conversation labels from Codex are not leaked as user-visible chat noise.

Notes for automation: Browser-only runs can cover the agent dropdown search path. Plugin sync requires the Codex App plugin fixture; mark only that subsection BLOCKED if the plugin environment is unavailable.
