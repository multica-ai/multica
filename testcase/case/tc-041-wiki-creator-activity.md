Purpose: Verify that wiki pages display their creator information and a lightweight activity/update log showing recent edit history.

Preconditions: The Multica web app is reachable. The user is signed in. The workspace has the Wiki feature enabled. At least one wiki page exists (or create one during the test).

User flow: Navigate to the Wiki section from the sidebar. Open an existing wiki page (or create a new one). Locate the creator information display area — this may be shown as metadata near the page title, in a sidebar panel, or in a page info/details section. Verify the creator's name or avatar is displayed. Make an edit to the wiki page content and save. Locate the activity log or "Recent updates" section. Verify that the edit just made appears in the log with timestamp and editor info. If another user edits the page, verify their activity also appears in the log.

Expected results: Each wiki page shows who created it (creator name/avatar). An activity log or update history section is accessible, showing: the editor's identity, a timestamp for each edit, and optionally a brief summary of changes. The log updates in near real-time or after page refresh when new edits are made. The creator info persists even after other users edit the page (creator ≠ last editor).

Notes for automation: Look for creator display near the page title or in a metadata/info panel. The activity log may be in a sidebar, a dedicated tab, or a collapsible section. Use visible text like "创建者" (Creator), "更新记录" (Update log), "活动" (Activity), or user avatar elements to locate these UI sections.
