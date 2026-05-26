---
case_id: TC-058
change_relation: new
feature_scope: create-issue-from-current
source_issue: OPE-1581
last_synced_at: 2026-05-26
---

Purpose: Verify that a user can create a new child issue from an existing issue detail page, with the original issue context prefilled and the original issue marked as blocked after successful creation.

Preconditions: The Multica web app is reachable. The user is signed in as `tester@multica.com` with verification code `888888`. At least one project exists in the workspace.

User flow: Open the Issues page. Create or choose a source issue with a recognizable title, description, and project. Open the source issue detail page. In the issue actions menu, find the top-level item directly below "Copy local workdir path" labeled "New issue from this" or "基于此 issue 新建". Click it to open the create-issue dialog. Confirm that the dialog is prefilled with the source issue title, description, project, and parent issue. Change the title to a unique child issue title if needed, then submit the dialog.

Expected results: The create-issue dialog opens from the issue detail page action item, not from the More submenu. The title, description, project, and parent issue fields match the source issue before submit. After creating the new issue, the new issue exists as a child of the source issue, and the source issue status is visibly changed to "Blocked" or "已阻塞". Ordinary issue creation controls remain available for editing fields before submit.

Notes for automation: Locate controls by visible sidebar text, issue title text, action item labels, create-issue dialog title, field labels, selected project text, parent issue text, status pill text, child issue section text, toast messages, and URL changes. Use a unique timestamp suffix for created issue titles so the scenario can identify the source and child records.
