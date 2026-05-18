Purpose: Verify that a user can sign in to Multica using their DingTalk account via the DingTalk OAuth flow.

Preconditions: The Multica web app is reachable in a browser. The backend is configured with valid DINGTALK_CLIENT_ID and DINGTALK_CLIENT_SECRET environment variables. The DingTalk login button is enabled in the frontend build (DINGTALK_CLIENT_ID is exposed via next.config).

User flow: Open `/login`. Confirm the page shows the standard email login form AND a DingTalk login button (labeled with a DingTalk icon or text such as `Log in with DingTalk`). Click the DingTalk login button. The browser should redirect to DingTalk's OAuth authorization page. After the user authorizes (or in test environments where DingTalk mock is available, the callback is simulated), the browser returns to the Multica callback URL. Wait for the post-login redirect to complete.

Expected results: After the DingTalk OAuth callback completes, the user lands on the authenticated workspace Issues page (URL ending in `/issues`). No error messages are displayed. The user's profile shows DingTalk as a linked account in the Settings > Linked Accounts section.

Notes for automation: The DingTalk OAuth flow involves an external redirect that cannot be fully automated without a mock DingTalk API or a real DingTalk test account. In CI environments, verify the DingTalk button renders and the redirect URL is correct. For end-to-end testing, a DingTalk sandbox account is required.
