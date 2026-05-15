Purpose: Verify that the issue detail page has scroll-to-top and scroll-to-bottom buttons that navigate the timeline efficiently.

Preconditions: The Multica web app is reachable. The user is signed in. An issue exists with a long timeline (many comments and activities, enough to require scrolling).

User flow: Open an issue with a long timeline. Scroll down partway through the timeline. Observe that a scroll-to-top button appears (typically a floating action button). Click it and verify the view scrolls to the top of the timeline. Then click the scroll-to-bottom button to jump to the latest entries.

Expected results: When the user has scrolled away from the top, a scroll-to-top button becomes visible. Clicking it smoothly scrolls the timeline to the beginning. A scroll-to-bottom button is available to jump to the most recent comments. Both buttons work reliably regardless of timeline length. The buttons disappear or change state when already at the respective position.

Notes for automation: The scroll control buttons are typically floating action buttons with up/down arrow icons. They appear conditionally based on scroll position. After clicking, verify scroll position changed by checking which timeline items are visible.
