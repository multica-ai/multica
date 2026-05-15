Purpose: Verify that project sorting works on the Projects page, allowing users to reorder projects by their preferred criteria.

Preconditions: The Multica web app is reachable. The user is signed in. At least 3 projects exist in the workspace.

User flow: Navigate to the Projects page from the sidebar. Observe the default project order. Look for a sort control (dropdown, toggle, or drag handle). Change the sort order (if drag-based, drag a project to a new position; if dropdown-based, select a different sort criterion). Verify the list reorders accordingly. Refresh the page and confirm the sort persists.

Expected results: The Projects page displays projects in a configurable order. Sort changes are reflected immediately in the UI. The chosen sort order persists across page reloads (saved server-side or in user preferences). All projects remain visible regardless of sort order.

Notes for automation: The sort mechanism varies — it may be drag-and-drop reordering or a dropdown selector. Identify it by visible sort-related UI elements. Verify order change by comparing the first project in the list before and after sorting.
