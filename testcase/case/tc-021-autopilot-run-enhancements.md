Purpose: Verify that the Autopilot feature shows enhanced run rows with duration, output summary, and expand/collapse functionality.

Preconditions: The Multica web app is reachable. The user is signed in. At least one autopilot exists with multiple completed runs (some successful, some possibly failed).

User flow: Navigate to the Autopilots page (or find it in the sidebar/settings). Open an autopilot's detail page. Scroll to the execution history / runs section. Observe the run rows: each should show duration, a brief output summary, and an expand/collapse control. Click expand on a run to see full details. Click collapse to hide them. Verify the total run count is displayed correctly.

Expected results: Each autopilot run row displays: execution duration (e.g., `2m 30s`), a truncated output summary (first line or condensed result), and an expand/collapse toggle. Expanding a row reveals the full output or execution details. The total run count shown in the header matches the actual number of runs. The cron scheduler reliably triggers autopilot runs at configured intervals without missing executions.

Notes for automation: Run rows are list items within the autopilot detail page. The expand/collapse control is a clickable element (chevron icon or button). Duration format varies; check for a time value. The output summary is truncated text that expands on click.
