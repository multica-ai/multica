Purpose: Verify that agent configuration tabs enforce permission controls — only the agent owner (or admin) can edit configuration, while other workspace members see read-only views.

Preconditions: The Multica web app is reachable. Two user accounts are available: User A who owns an agent, and User B who is a workspace member but not the agent owner. The agent has environment variables, instructions, and skills configured.

User flow: Sign in as User B. Navigate to the Agents page and open the agent owned by User A. Navigate through the agent's tabs: Overview, Instructions, Environment, Skills. Observe whether the fields are editable or read-only. Then sign in as User A and open the same agent to confirm full edit access.

Expected results: When User B views User A's agent: the Overview tab shows agent info in read-only mode, the Instructions tab shows text but the textarea is disabled or not present, the Environment tab shows variable keys with redacted values (no edit button), and the Skills tab shows skills in read-only mode. When User A views their own agent: all tabs are fully editable with save/update buttons visible.

Notes for automation: Check for the presence or absence of edit controls (buttons, editable textareas, input fields) to determine read-only vs editable state. The Environment tab specifically should show keys but mask values for non-owners.
