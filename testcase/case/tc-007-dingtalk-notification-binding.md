Purpose: Verify that a user can bind their DingTalk account for receiving notifications, and that DingTalk notifications are delivered when an @mention occurs.

Preconditions: The Multica web app is reachable. The user is signed in. The backend has DingTalk notification configuration (bot token, app key). The user has a DingTalk account that can be bound.

User flow: Navigate to Settings > Notifications. In the Channels section, locate the DingTalk channel row. If not yet bound, follow the DingTalk binding flow (which may involve scanning a QR code or entering DingTalk credentials). Once bound, confirm the DingTalk channel shows as `active` or `enabled`. Create a test issue and have another user or agent @mention the current user in a comment. Check the DingTalk app for the notification.

Expected results: The DingTalk binding completes without error. The Notifications settings page shows DingTalk as an active delivery channel. When the user is @mentioned in an issue comment, a DingTalk notification card is delivered to the user's DingTalk app containing the issue title, comment excerpt, and a clickable action link that opens the issue in Multica.

Notes for automation: DingTalk notification delivery verification requires either a DingTalk test bot or manual verification. The binding flow involves DingTalk's external auth page. Verify the UI shows the binding state correctly after the flow completes.
