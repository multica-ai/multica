Purpose: Verify that the seeded fixed verification code account can sign in from the web login page and reach the default Issues workspace page.

Preconditions: The Multica web app is reachable in a browser. The backend has applied the fixed verification code migration for `tester@multica.com`. The testcase auth fixture `testcase/auth/auth.json` is available and contains `login_type: email_fixed_code`, email `tester@multica.com`, and verification code `888888`.

User flow: Open `/login`. Confirm the page shows the title `Sign in to Multica`, the helper text `Enter your email to get a login code`, an `Email` input, and a primary `Continue` button. Fill the `Email` field with `tester@multica.com` and submit. Confirm the page switches to the code step with the title `Check your email` and text saying a verification code was sent to `tester@multica.com`. Enter the 6-digit code `888888` into the one-time-code input. Wait for the post-login redirect to complete.

Expected results: The login succeeds without showing an error message. The browser leaves `/login` and lands on the authenticated default workspace destination, normally the Issues page with a URL ending in `/issues`. An issue-list heading or equivalent Issues page content is visible, proving the user is signed in and the fixed verification code path can create a valid session.

Notes for automation: Use visible text and labels instead of CSS selectors. The code entry control is a 6-slot OTP input on the `Check your email` card. If the final workspace slug differs by environment, assert the authenticated route by page content and a path ending in `/issues` rather than a hard-coded workspace slug.
