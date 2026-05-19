Purpose: Verify that usage dashboards expose the new 1d and weekly usage ranges with timezone-aware data grouping.

Preconditions: The Multica web app is reachable. The user is signed in. The workspace has runtime usage records or seeded usage data.

User flow: Navigate to the workspace Dashboard or Usage page. Open the Usage tab. Select the 1d time range and verify the charts and summary cards update. Navigate to the Runtimes/Computers usage area and locate weekly usage charts for cost, tasks, time, or tokens. Change the range or timezone-related display if available, then verify chart labels remain readable.

Expected results: The workspace Usage tab includes a 1d range option. Selecting 1d updates the visible usage data without errors. Runtime usage displays weekly-dimension charts or summaries. Chart labels and aggregation boundaries align with the user's workspace/browser timezone rather than showing obviously shifted dates.

Notes for automation: Locate range controls by visible labels such as "1d", "7d", "30d", or "Week". Use chart headings, axis labels, and summary cards as browser-observable assertions. Seeded empty data may still pass if the 1d and weekly controls render and show a valid empty state.
