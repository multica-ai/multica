Purpose: Verify that issue comments render as thread-aware conversations and preserve reply context.

Preconditions: The Multica web app is reachable. The user is signed in. An issue exists where the user can add comments and replies.

User flow: Open an issue detail page. Add a top-level comment with unique text. Reply to that comment with another unique text. Add a second top-level comment. Refresh the page or reopen the issue. Verify the reply remains visually grouped under its parent comment and that top-level comments and replies keep their expected order. If a reply button or thread count is shown, use it to navigate within the thread.

Expected results: Top-level comments and replies are displayed with clear parent/reply structure. Replies remain attached to the correct parent after reload. The comment list can load more comments without flattening replies into unrelated top-level items. Reply controls continue to target the intended parent comment.

Notes for automation: Use unique timestamped comment text so the parent and reply can be identified by visible content. Prefer visible reply controls, indentation, thread labels, and parent/child proximity over DOM selectors.
