Purpose: Verify that users can subscribe to and unsubscribe from issues, and that the subscriber list displays with subscribed users sorted first.

Preconditions: The Multica web app is reachable. The user is signed in. An issue exists in the workspace. At least two workspace members are available (for verifying subscriber list ordering).

User flow: Navigate to an issue detail page. Locate the subscription control — this may be a "Subscribe" / "订阅" button or bell icon in the issue header or sidebar. If not already subscribed, click Subscribe. Verify the UI reflects the subscribed state (button changes to "Unsubscribe" / "取消订阅", or visual indicator appears). Open the subscriber list (click subscriber count or a "Subscribers" section). Verify the current user appears in the list. Have a second user subscribe to the same issue. Refresh the subscriber list. Verify that subscribed users appear before non-subscribed users in the list (OPE-995: subscribed-first sorting). Click Unsubscribe. Verify the UI returns to the unsubscribed state and the user is removed from the subscriber list.

Expected results: The subscribe/unsubscribe toggle works correctly and the UI state updates immediately. The subscriber list is accessible and shows all subscribers. Subscribed users are sorted to the top of the subscriber list (OPE-995). After unsubscribing, the user is removed from the list and the subscribe button returns to its default state. Subscription status is persisted across page reloads.

Notes for automation: Look for subscribe controls by text labels ("Subscribe" / "订阅" / "Unsubscribe" / "取消订阅") or bell icon buttons. The subscriber list may be in a sidebar panel, a popover, or a dedicated section. Sorting verification requires at least two entries in the subscriber list.
