Purpose: Verify that the custom webhook notification feature supports configurable message formats, allowing users to customize the payload structure sent to their webhook endpoints.

Preconditions: The Multica web app is reachable. The user is signed in. At least one custom webhook endpoint is already configured (from tc-002). The webhook format customization feature is available.

User flow: Navigate to Settings > Notifications > Custom Webhooks. Edit an existing webhook endpoint. Look for a format/template configuration option. If available, modify the message format (e.g., switch between default, DingTalk card format, or custom template). Save the changes. Trigger a notification event (e.g., @mention the user in a comment). Verify the webhook receives the payload in the configured format.

Expected results: The webhook configuration allows specifying a message format beyond the default. DingTalk action card format is supported (preserving action links and metadata). Custom format changes are saved and applied to subsequent webhook deliveries. The test-send action respects the configured format. If format is invalid, an appropriate error message is shown.

Notes for automation: The format configuration may be a dropdown or text field in the webhook edit form. Verify by checking the webhook receiver's received payload structure after triggering a notification. The DingTalk card format should include action links that preserve the notification metadata.
