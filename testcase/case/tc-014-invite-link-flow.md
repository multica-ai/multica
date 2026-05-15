Purpose: Verify that workspace members can be invited via a shareable invite link, and that the invite link acceptance flow works correctly.

Preconditions: The Multica web app is reachable. The user is signed in as a workspace admin or owner. The Settings > Members tab has an invite link feature.

User flow: Navigate to Settings > Members tab. Locate the invite link section (may show a `Copy invite link` button or invite link management UI). Generate or copy an invite link. Verify the link URL is well-formed. Open the invite link in a different browser session (or incognito) where a different user is logged in (or not logged in). Follow the acceptance flow — the new user should be able to join the workspace.

Expected results: The Settings > Members tab shows invite link management UI with a copy button. The generated invite link is a valid URL pointing to the Multica invite acceptance page. When a new user opens the invite link, they see an acceptance page with the workspace name and a `Join` or `Accept` button. After accepting, the new user becomes a workspace member and can access the workspace. The invite link validity period is shown (in days) and can be configured.

Notes for automation: The invite link acceptance flow requires two separate user sessions. Use the copy link button's visible label to locate it. The acceptance page URL matches `/invite/{token}` pattern. Verify membership by checking the Members list after acceptance.
