Purpose: Verify that the Retry button appears on agent comments and system task-run comments, and that clicking Retry re-triggers the agent task correctly.

Preconditions: The Multica web app is reachable. The user is signed in to any workspace. An issue exists in that workspace with at least one agent comment from a completed or failed task run. The comment can be either a regular agent comment or a system-type task-run comment.

Workspace note: Retry is a product-level feature, not tied to a specific workspace. You can run this test in any workspace your auth can access.

Fixture setup if no suitable issue/comment exists:
1. Follow `testcase/fixtures/README.md` → "Agent task run fixture".
2. For local/self-host regression, check the local profile daemon before declaring a runtime blocker:
   ```bash
   multica --profile local daemon status --output json
   multica --profile local daemon restart
   ```
3. Create or select a disposable test issue.
4. Trigger a test agent with a markdown mention comment, not plain `@agent_name`:
   ```markdown
   [@agent_name](mention://agent/<agent_uuid>) 请回复一句简短测试消息，用于生成 TC-019 retry 回归数据。
   ```
5. Wait for the agent final comment and task run to appear.
6. Verify the fixture before executing the UI assertions:
   ```bash
   multica issue runs <issue_id_or_identifier> --output json --full-id
   multica issue comment list <issue_id_or_identifier> --output json
   ```

Do not mark this case BLOCKED solely because the initially opened issue has no agent comment or retryable task run. BLOCKED is valid only after the fixture setup steps above were attempted and the exact failing step is reported.

User flow: Open an issue that has agent task run comments in its timeline. Locate an agent's comment or a system task-run comment. Hover over or interact with the comment to reveal action buttons. Find the `Retry` button. Click it. Observe the task being re-triggered. Wait for the new task run to appear or for an in-progress task indicator to become visible.

Expected results: The Retry button is visible on all agent comments (not just system-type comments, per OPE-425 broadening). Clicking Retry creates a new task run for the same agent on the same issue. The new task run appears in the execution log. The Retry action preserves the original trigger actor metadata. For system comments without a parent chain, the retry falls back gracefully without crashing.

Notes for automation: The Retry button may appear on hover or in a context menu. Look for an icon button with retry/refresh semantics. After clicking, verify a new task run entry appears in the execution log section or a loading indicator shows the task was triggered. Capture screenshots before and after retry, and include the issue identifier used as fixture evidence.
