Purpose: Verify that the execution log section on the issue detail page shows run index, filter/search, trigger info, agent coloring, comment jump links, and run statistics.

Preconditions: The Multica web app is reachable. The user is signed in. An issue exists with multiple completed task runs from different agents (ideally at least 3 runs, some successful, some failed).

Fixture setup if no suitable issue exists:
1. Follow `testcase/fixtures/README.md` → "Agent task run fixture".
2. For local/self-host regression, check the local profile daemon before declaring a runtime blocker:
   ```bash
   multica --profile local daemon status --output json
   multica --profile local daemon restart
   ```
3. Create or select a disposable test issue.
4. Trigger a test agent with a markdown mention comment, not plain `@agent_name`:
   ```markdown
   [@agent_name](mention://agent/<agent_uuid>) 请回复一句简短测试消息，用于生成 TC-018 execution log 回归数据。
   ```
5. Wait for completion, then repeat with additional mentions or:
   ```bash
   multica issue rerun <issue_id_or_identifier> --output json
   ```
6. Verify at least 3 task runs exist before executing the UI assertions:
   ```bash
   multica issue runs <issue_id_or_identifier> --output json --full-id
   ```

Do not mark this case BLOCKED solely because the initially opened issue has no task runs. BLOCKED is valid only after the fixture setup steps above were attempted and the exact failing step is reported.

User flow: Open the issue detail page for the issue with task runs. Scroll to or locate the Execution Log section. Observe the run list: each run should show a numbered index, the agent name with a color indicator, trigger information, and duration/status. Use the filter/search input to filter runs by keyword or agent name. Click on a run entry to expand it or jump to the associated comment in the timeline. Check the run statistics summary.

Expected results: Each task run is displayed with a sequential run index number. Agent names are color-coded for visual distinction between different agents. Trigger info shows what initiated the run (e.g., @mention, retry, autopilot). The filter/search input narrows the visible runs list. Clicking a run entry's comment link scrolls to or highlights the corresponding comment in the timeline. Run statistics show counts of completed, failed, and total runs. Duration is displayed for completed runs.

Notes for automation: The execution log section has a heading labeled with execution log text and a filter input with a placeholder. Run entries are list items with visible index numbers, agent names, and status badges. Use the filter input's placeholder text to locate it. Capture a screenshot of the execution log and include the issue identifier used as fixture evidence.
