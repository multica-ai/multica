Purpose: Verify that the Agents page shows Mine/All scope filter and that switching between them correctly filters the agent list by ownership.

Preconditions: The Multica web app is reachable. The user is signed in. The workspace has at least two agents: one owned by the current user and one owned by another user. Both agents should be visible to workspace members (the private agent visibility fix OPE-495 is in effect).

User flow: Navigate to the Agents page from the sidebar. Confirm the page shows a scope selector with options `Mine` and `All` (or equivalent segment control). By default the view should show all agents. Click `Mine` to filter. Observe the agent list. Click `All` to show all agents again.

Expected results: When `All` is selected, both the current user's agents and other users' agents appear in the list. When `Mine` is selected, only agents owned by the current user are shown. The filter persists during the session. Private agents owned by other users are visible in the `All` view (per OPE-495), but their configuration tabs show read-only state for non-owners.

Notes for automation: Identify the scope selector by its visible labels (`Mine`, `All` or equivalent text). Count agent cards before and after filtering to verify the filter effect. Do not rely on CSS classes to distinguish ownership.
