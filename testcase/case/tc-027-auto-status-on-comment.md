Purpose: Verify that the issue status automatically transitions to `in_progress` when a comment (especially an agent comment) is posted, reflecting active work on the issue.

Preconditions: The Multica web app is reachable. The user is signed in. An issue exists with status `todo` or `backlog` (not yet in progress).

User flow: Open an issue that is in `todo` status. Post a comment (or trigger an agent task that posts a comment). After the comment is posted, observe the issue's status indicator.

Expected results: After a comment is posted on a `todo` or `backlog` issue, the issue status automatically transitions to `in_progress`. The status change is reflected in the issue detail header and in the issue list. This automatic transition does NOT occur if the issue is already in a terminal state (e.g., `done`, `cancelled`). The transition is triggered by comment creation, not by viewing the issue.

Notes for automation: Check the status badge or indicator before and after posting a comment. The status change may take a moment to propagate via WebSocket. Verify by refreshing or checking the status indicator text/color change.
