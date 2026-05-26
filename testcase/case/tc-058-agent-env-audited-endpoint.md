Purpose: Verify that the dedicated agent environment management endpoint (GET/PUT /api/agents/{id}/env) enforces owner-only access, masks values for reveal, rejects agent-actor requests, and audits reveals.

Preconditions: The Multica web app is reachable. User A owns an agent with at least 2 environment variables configured. User B is a workspace admin but NOT the agent owner. An agent runtime is connected (for testing agent-actor rejection via task token).

User flow:
1. Sign in as User A (agent owner). Navigate to the agent detail page, open the Environment tab. Verify env keys are listed with masked values (showing "****"). Click "Reveal" or equivalent to show plaintext values. Confirm values are now visible.
2. Edit an env variable value, save. Verify the updated value persists after page reload.
3. Add a new env key-value pair, save. Verify it appears in the list.
4. Delete an env key, save. Verify it is removed.
5. Sign in as User B (admin, non-owner). Navigate to the same agent's Environment tab. Verify that the reveal action is not available or returns a 403/forbidden error. Verify that PUT (edit) is blocked.
6. (API-level) Attempt GET /api/agents/{id}/env with a task token (agent actor). Verify it returns 403 "agents may not access env management endpoints".

Expected results:
- Owner can reveal, add, edit, and delete env variables via the dedicated endpoint.
- Non-owner admin cannot reveal or modify env variables (403 forbidden).
- Agent-actor tokens are rejected at the env endpoint regardless of backing member.
- The env tab in the UI shows masked values by default; reveal requires explicit action.
- After PUT with a value of "****", the original value is preserved (sentinel guard).

Notes for automation: The Environment tab URL pattern includes the agent ID. Check for the presence of reveal/edit controls for owner vs their absence for non-owner. The 403 rejection for agent actors is best verified via direct API call if browser automation cannot impersonate a task token.
