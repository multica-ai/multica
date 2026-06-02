Purpose: Verify that Fork issue creation and status mutation guards preserve project context, labels, parent links, assignee defaults, attachment validation, and workspace scoping.

Preconditions: The Multica web app and backend are reachable. The user is signed in. At least one project, one label, and two issues exist in the workspace.

User flow:
1. Open a project detail page and create an issue from that context.
2. Verify project, labels, and default assignee behavior in the create issue dialog.
3. Create a child issue from an existing issue detail page and verify parent/context fields are prefilled.
4. Attempt to create an issue without a project through the UI or API.
5. Attempt to update issue status from comment/task paths using an issue in another workspace fixture if available.
6. Add a comment that changes status and verify status updates affect only the target issue in the current workspace.

Expected results:
- Issue creation from project detail preselects the current project.
- Manual create mode defaults assignee to the current user where configured.
- Labels selected during creation persist on the new issue.
- Create-from-current-issue flow preserves parent/context and blocks the source issue only when that workflow explicitly requires it.
- Creating an issue without `project_id` is rejected with a clear validation error.
- Status updates from comment/task paths include workspace scope and cannot mutate same-ID records in another workspace.

Notes for automation: The cross-workspace status guard is best verified with API-level fixtures. Browser automation can cover the project context, label, parent, and assignee defaults.
