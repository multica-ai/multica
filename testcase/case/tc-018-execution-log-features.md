Purpose: Verify that the execution log section on the issue detail page shows run index, filter/search, trigger info, agent coloring, comment jump links, and run statistics.

Preconditions: The Multica web app is reachable. The user is signed in. An issue exists with multiple completed task runs from different agents (ideally at least 3 runs, some successful, some failed).

User flow: Open the issue detail page. Scroll to or locate the Execution Log section. Observe the run list: each run should show a numbered index, the agent name with a color indicator, trigger information, and duration/status. Use the filter/search input to filter runs by keyword or agent name. Click on a run entry to expand it or jump to the associated comment in the timeline. Check the run statistics summary.

Expected results: Each task run is displayed with a sequential run index number. Agent names are color-coded for visual distinction between different agents. Trigger info shows what initiated the run (e.g., @mention, retry, autopilot). The filter/search input narrows the visible runs list. Clicking a run entry's comment link scrolls to or highlights the corresponding comment in the timeline. Run statistics show counts of completed, failed, and total runs. Duration is displayed for completed runs.

Notes for automation: The execution log section has a heading labeled with execution log text and a filter input with a placeholder. Run entries are list items with visible index numbers, agent names, and status badges. Use the filter input's placeholder text to locate it.
