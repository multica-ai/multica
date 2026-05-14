Purpose: Verify that email notification binding works and that email notifications are delivered when triggered by @mention or subscribed issue updates.

Preconditions: The Multica web app is reachable. The user is signed in. The backend has SMTP configured (SMTP_HOST, SMTP_PORT, SMTP_USER, SMTP_PASS) or Resend API key. The user logged in with an email account.

User flow: Navigate to Settings > Notifications. In the Channels section, locate the Email channel row. Confirm the user's email is shown (auto-bound from login). If the email switch is off, turn it on. Create a test issue and have another user @mention the current user in a comment. Check the user's email inbox for the notification.

Expected results: The Notifications settings page shows Email as an active delivery channel with the user's email address displayed. The email channel switch persists its state after page reload. When @mentioned, the user receives an email notification containing the issue title, comment content, and a link back to the issue. The email HTML properly escapes the link URL and renders correctly.

Notes for automation: Email delivery verification requires access to the recipient's mailbox or a test SMTP server (like MailHog). Verify the settings UI shows correct binding state. The email sender should use SMTP_USERNAME as the default sender address.
