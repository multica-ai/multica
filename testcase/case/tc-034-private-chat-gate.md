Purpose: Verify that private chat sessions are gated by agent ownership — only the agent's owner can access private chat sessions with that agent.

Preconditions: The Multica web app is reachable. Two users: User A who owns a private agent, and User B who does not. User A has initiated a chat session with their private agent.

User flow: Sign in as User A. Open the Chat section. Start or open a chat session with User A's private agent. Verify the chat works normally. Then sign in as User B. Navigate to Chat. Verify that User A's private agent does NOT appear in User B's agent selection for new chats. Attempt to access the chat session URL directly (if known). Verify access is denied.

Expected results: User A can create and use chat sessions with their own private agent. User B cannot see User A's private agent in the chat agent picker. If User B tries to access a private chat session URL directly, they receive an access denied error or are redirected. The private agent selection filter respects agent ownership at the API level, not just UI filtering.

Notes for automation: Test requires two separate user sessions. In the chat agent picker dropdown, verify the private agent's absence for User B. Direct URL access testing requires knowing the chat session URL format.
