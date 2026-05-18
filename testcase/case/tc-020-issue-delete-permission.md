Purpose: Verify that issue deletion is restricted to the issue creator or workspace admin, and that other members cannot delete issues.

Preconditions: The Multica web app is reachable. Two user accounts: User A (issue creator, regular member) and User B (regular member, not admin, not creator). An issue created by User A exists.

User flow: Sign in as User B. Open the issue created by User A. Look for a Delete option in the issue's action menu or context menu. Verify it is not available or disabled. Sign in as User A. Open the same issue. Confirm the Delete option is available. Also verify that a workspace admin can see the Delete option on any issue.

Expected results: User B (non-creator, non-admin) cannot see or use the Delete option on User A's issue. User A (creator) sees the Delete option and can delete their own issue. Workspace admins see the Delete option on all issues regardless of creator. Attempting to delete via API as a non-authorized user returns a permission error.

Notes for automation: Check for the presence of a Delete button or menu item in the issue's action menu. The delete action should have a confirmation dialog. Test with at least two user sessions to verify permission enforcement.
