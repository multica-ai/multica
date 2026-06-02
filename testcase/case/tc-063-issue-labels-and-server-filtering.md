Purpose: Verify that Fork issue labels are project-scoped, usable during issue creation, editable from board cards, and respected by server-side issue filtering.

Preconditions: The Multica web app is reachable. The user is signed in. Two projects exist in the workspace. Each project has at least one label with a distinct name/color.

User flow:
1. Open Project A and create labels that are unique to Project A.
2. Open Project B and create labels that are unique to Project B.
3. From Project A, open Create Issue and verify only Project A labels are offered.
4. Create an issue with one or more labels selected.
5. Open the board/list view and use the board card label picker to add/remove a label.
6. Use issue filters for project, label, status, assignee, and keyword search.
7. Switch to Project B and verify Project A labels are not offered in Project B issue creation/filter contexts.

Expected results:
- Labels are scoped by `project_id`; labels from another project do not appear in create/edit/filter pickers for the current project.
- `label_ids` submitted during issue creation are persisted and shown on the issue card/detail.
- Board card label editing has a compact but discoverable affordance and does not resize or break the card layout.
- Server-side issue filters return only matching issues and reduce API payload size compared with fetching all issues then filtering client-side.
- Removing a label updates list, board, and detail views consistently.

Notes for automation: Use unique label names per project, such as `tc063-a-label` and `tc063-b-label`, to avoid ambiguity. Network inspection can be used to verify that filter parameters are sent to the server query.
