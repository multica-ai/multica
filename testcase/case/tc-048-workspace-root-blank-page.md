---
case_id: TC-048
change_relation: new
feature_scope: login-workspace-root
source_issue: OPE-1279
last_synced_at: 2026-05-22
---

Purpose: Verify that opening a workspace root URL renders the workspace layout with an empty main area.

Preconditions: The Multica web app is reachable in a browser. The user is signed in and has access to at least one workspace such as `openharness`.

User flow: Navigate directly to `/{workspaceSlug}`. Wait for the workspace layout to finish loading. Observe the left workspace navigation and the right main content area without clicking any sidebar item.

Expected results: The browser stays on `/{workspaceSlug}` and does not redirect to `/{workspaceSlug}/issues` or `/{workspaceSlug}/my-issues`. The left workspace navigation remains visible with the normal workspace sections. The right main content area is intentionally blank: it does not show an Issues page, My Issues page, welcome page, entry cards, or workspace overview content.

Notes for automation: Prefer the workspace slug reached after login when the environment does not guarantee `openharness`. Assert the final URL path contains only the workspace slug. Use visible sidebar labels to confirm the layout is present, then assert common Issues/My Issues headings or list content are absent from the main area.
