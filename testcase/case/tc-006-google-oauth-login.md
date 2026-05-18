Purpose: Verify that a user can sign in to Multica using their Google account via the Google OAuth flow, and that the Google account binding persists.

Preconditions: The Multica web app is reachable in a browser. The backend is configured with GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET. If a GOOGLE_OAUTH_PROXY is configured, the proxy is reachable.

User flow: Open `/login`. Confirm the page shows a Google login button (labeled with a Google icon or text such as `Log in with Google`). Click the Google login button. The browser should redirect to Google's OAuth consent screen. After authorization, the browser returns to the Multica OAuth callback. Wait for the post-login redirect to complete. Then navigate to Settings > Linked Accounts.

Expected results: After the Google OAuth callback completes, the user lands on the authenticated workspace Issues page. No error messages are displayed. In Settings > Linked Accounts, the user sees their Google account listed as a linked account with their Google email visible or partially masked.

Notes for automation: Google OAuth requires a real or sandboxed Google account. In CI, verify the Google button renders and the OAuth redirect URL is well-formed. The GOOGLE_OAUTH_PROXY configuration should be honored when present.
