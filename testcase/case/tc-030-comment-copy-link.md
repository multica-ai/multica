Purpose: Verify that the comment copy link feature generates a direct link to a specific comment, and that opening the link scrolls to that comment.

Preconditions: The Multica web app is reachable. The user is signed in. An issue exists with multiple comments.

User flow: Open an issue with multiple comments in its timeline. Hover over or right-click a specific comment. Find and click the `Copy link` option in the comment's action menu. Paste the copied URL into a new browser tab. Navigate to the URL.

Expected results: The copy link action places a URL in the clipboard that includes the issue identifier and a comment anchor (e.g., `?comment={id}` or `#comment-{id}`). Opening the URL navigates to the issue detail page and automatically scrolls to or highlights the specific comment. The target comment is visually distinguished (highlight, border, or focus indicator) after navigation.

Notes for automation: The copy link action is in the comment's hover menu or context menu. Verify the clipboard content matches an expected URL pattern. After navigating to the link, check that the target comment is visible in the viewport.
