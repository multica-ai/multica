Purpose: Verify that My Issues view includes issues created by the current user that are unassigned, in addition to issues assigned to the user.

Preconditions: The Multica web app is reachable. The user is signed in. The user has created at least one issue that is NOT assigned to anyone. The user also has at least one issue assigned to them.

User flow: Navigate to the Issues page. Switch to the `My Issues` filter/view (if not already selected by default). Observe the issue list.

Expected results: The My Issues view shows: (1) all issues assigned to the current user, AND (2) issues created by the current user that have no assignee. Both types appear in the same list. Issues created by others that are unassigned do NOT appear. The combined list is sorted by the standard sort order (usually by update time or priority).

Notes for automation: The My Issues filter may be a tab, toggle, or default view. Verify by creating an unassigned issue, then checking it appears in My Issues. Cross-check that an unassigned issue created by another user does not appear.
