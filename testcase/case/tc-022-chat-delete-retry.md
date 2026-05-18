Purpose: Verify that chat message deletion and retry work correctly, including rate-limit auto-retry behavior.

Preconditions: The Multica web app is reachable. The user is signed in. A chat session exists with at least one message exchange (user message + agent response).

User flow: Open an existing chat session. Locate a user message in the conversation. Use the message's action menu (hover or right-click) to find the Delete option. Click Delete and confirm. Observe the message is removed. Then send a new message. If the agent response fails or is incomplete, locate the Retry action on the failed message. Click Retry and observe the message being resent.

Expected results: Deleting a user message removes it from the chat timeline. The agent's response to that message is also removed (cascading delete). The Retry action resends the last user message to the agent. If a rate limit (429) is encountered during retry, the system automatically retries after a delay without user intervention. The chat history remains consistent after delete/retry operations.

Notes for automation: Message action menus appear on hover over chat bubbles. The Delete action should have a confirmation step. For rate-limit testing, this requires triggering multiple rapid retries. Verify timeline consistency by counting messages before and after operations.
