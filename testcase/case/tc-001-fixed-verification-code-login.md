---
case_id: TC-001
change_relation: updated
feature_scope: login-workspace-root
source_issue: OPE-1279
last_synced_at: 2026-05-22
---

Purpose: Verify that the seeded fixed verification code account can sign in from the web login page and reach the workspace root blank page instead of Issues or My Issues.

Preconditions: The Multica web app is reachable in a browser. The backend has applied the fixed verification code migration for `tester@multica.com`. The testcase auth fixture `testcase/auth/auth.json` is available and contains `login_type: email_fixed_code`, email `tester@multica.com`, and verification code `888888`.

User flow: Open `/login`. Confirm the page shows the title `Sign in to Multica`, the helper text `Enter your email to get a login code`, an `Email` input, and a primary `Continue` button. Fill the `Email` field with `tester@multica.com` and submit. Confirm the page switches to the code step with the title `Check your email` and text saying a verification code was sent to `tester@multica.com`. Enter the 6-digit code `888888` into the one-time-code input. Wait for the post-login redirect to complete.

Expected results: The login succeeds without showing an error message. The browser leaves `/login` and lands on the authenticated default workspace destination with a URL shaped as `/{workspaceSlug}`. The URL must not end in `/issues` or `/my-issues`. The workspace sidebar remains visible, while the main content area is blank and does not show the Issues list, My Issues list, welcome content, entry cards, or a workspace overview.

Notes for automation: Use visible text and labels instead of CSS selectors. The code entry control is a 6-slot OTP input on the `Check your email` card. If the final workspace slug differs by environment, assert the URL has exactly one path segment after the host and that no Issues or My Issues page heading/content appears.
