# TC-004: Create Issue auto-sets current Project when opened from project detail page

## Summary

Verify that when a user is on a project detail page and clicks "New Issue", the Project field in the create-issue dialog is automatically pre-filled with the current project.

## Precondition

- Logged in as `tester@multica.com` with verification code `888888`.
- At least one project exists in the workspace.

## Steps

1. Navigate to the Projects page from the sidebar.
2. Click on any project in the list to open its detail page (the URL should contain `/projects/<id>`).
3. On the project detail page, locate and click the "New Issue" button (visible in the sidebar or the project page header).
4. In the Create Issue dialog/modal that opens, observe the Project field.

## Expected Result

The Project field in the Create Issue dialog is pre-filled with the name of the project whose detail page the user was on. The user should NOT need to manually select a project.

## Cleanup

Close the Create Issue dialog without saving.
