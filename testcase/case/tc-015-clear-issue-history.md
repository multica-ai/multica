Purpose: Verify that the one-click Clear History feature removes all comments and task runs from an issue, with confirmation dialog.

Preconditions: The Multica web app is reachable. The user is signed in. An issue exists with at least one comment and one completed task run.

User flow: Open an existing issue that has comments and task run history. Locate the Clear History action (button or menu item on the issue detail page). Click it. A confirmation dialog should appear warning that this will remove all comments and task runs. Confirm the action. Wait for the page to refresh or the timeline to update.

Expected results: Before clearing: the issue timeline shows comments and task run entries. A confirmation dialog appears when Clear History is clicked, clearly stating what will be removed. After confirming: all comments are removed from the timeline, all task run records are cleared, and the issue itself remains intact (title, description, status unchanged). The action is irreversible — a warning should be present in the confirmation dialog.

Notes for automation: Locate the Clear History button by its visible label text. The confirmation dialog should have `Confirm` and `Cancel` buttons. After clearing, verify the timeline is empty by checking for the absence of comment cards and task run entries.
