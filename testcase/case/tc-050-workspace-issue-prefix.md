Purpose: Verify that workspace settings allow editing the issue prefix and that newly created issues use the updated prefix.

Preconditions: The Multica web app is reachable. The user is signed in as a user allowed to edit workspace settings. The workspace has an existing issue prefix.

User flow: Navigate to Settings and open the Workspace settings tab. Locate the Issue prefix field. Change the prefix to a short test value and save. Create a new issue in the workspace. Verify the new issue identifier uses the updated prefix. Return to Workspace settings and restore the original prefix.

Expected results: The Workspace settings page exposes an editable issue prefix field. Saving a valid new prefix succeeds and persists after reload. New issue identifiers use the updated prefix. Restoring the original prefix succeeds so existing workspace behavior is preserved.

Notes for automation: Record the original prefix before changing it and restore it during cleanup. Use a timestamped prefix if the environment allows arbitrary values, but keep it short and uppercase for readability.
