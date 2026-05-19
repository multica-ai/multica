Purpose: Verify that an agent detail page exposes the tasks panel as a searchable task list.

Preconditions: The Multica web app is reachable. The user is signed in. At least one agent exists with completed or failed task history linked to issues.

User flow: Navigate to Agents and open an agent detail page. Open the Tasks panel or tab. Confirm task rows are listed with issue context and status. Use the issue search field to search by an issue title, identifier, or keyword that appears in one task. Clear the search field and observe the list again.

Expected results: The Tasks panel shows a list of tasks for the selected agent without requiring a transcript-first view. Each row exposes useful issue context, status, and timing information. Searching by issue text narrows the visible task list to matching tasks. Clearing the search restores the full task list.

Notes for automation: Locate the panel by visible text such as "Tasks". Use seeded or newly created task history when available. Search assertions can compare the count or visible issue identifiers before and after filtering.
