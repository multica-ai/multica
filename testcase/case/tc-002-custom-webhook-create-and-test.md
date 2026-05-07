Purpose: Verify that a signed-in user can add a custom webhook endpoint from Settings and send a test payload successfully.

Preconditions: The Multica web app is reachable in a browser. The build under test includes the OPE-222 custom webhook notifications feature. The testcase auth fixture `testcase/auth/auth.json` is available and contains a valid fixed-code login for `tester@multica.com`. A reachable webhook receiver is available for the environment under test so the `Test` action can return success.

User flow: Sign in with the fixed verification code account from `testcase/auth/auth.json`. Open the workspace `Settings` page and switch to the `Notifications` tab. Confirm the page shows the `Notifications` heading, the `Custom Webhooks` section, and the `Webhook endpoints` card. In the add form, fill `Name` with `GTD`, fill `URL` with a valid HTTPS webhook endpoint, optionally fill `Secret`, and click the `Add` button. Wait for the form to complete, then use the new row's test-send action button.

Expected results: A visible success message confirms the webhook was saved. The new webhook appears in the `Webhook endpoints` list with the name `GTD`, a masked URL instead of the raw full URL, and an `active` status badge. Triggering the test action shows a visible success message such as `Test sent`, proving the configured endpoint can be used from the settings UI.

Notes for automation: Use visible labels and button names instead of CSS selectors. The test-send action is the icon button on the webhook row with an accessible label in the form `Test <webhook name>`. Assert the masked URL behavior by checking that the row is present and does not expose the full endpoint string verbatim.
