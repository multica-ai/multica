Purpose: Verify that My Issues includes issues where the current user is involved through squad assignment.

Preconditions: The Multica web app is reachable. The user is signed in. The workspace has a squad containing the current user, and at least one issue is assigned to that squad rather than directly to the user.

User flow: Navigate to Issues or My Issues. Switch to the My Issues view. Locate an issue assigned to a squad that includes the current user. Open the issue and verify the assignee/squad context. Return to the list and switch to an All Issues view for comparison.

Expected results: My Issues includes issues where `involves_user_id` matches the current user through squad membership. Squad-assigned issues appear alongside directly assigned issues. The list does not require the issue to be directly assigned to the current user. Switching to All Issues keeps the same issue accessible.

Notes for automation: Use a seeded squad membership when possible. Identify squad assignment by visible assignee chip, squad name, or issue metadata. This covers the user-visible behavior of the `involves_user_id` query path.
