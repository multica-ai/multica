Purpose: Verify that the Retry button appears on agent comments and system task-run comments, and that clicking Retry re-triggers the agent task correctly.

Preconditions: The Multica web app is reachable. The user is signed in. An issue exists with at least one agent comment (from a completed or failed task run). The comment can be either a regular agent comment or a system-type task-run comment.

User flow: Open an issue that has agent task run comments in its timeline. Locate an agent's comment or a system task-run comment. Hover over or interact with the comment to reveal action buttons. Find the `Retry` button. Click it. Observe the task being re-triggered.

Expected results: The Retry button is visible on all agent comments (not just system-type comments, per OPE-425 broadening). Clicking Retry creates a new task run for the same agent on the same issue. The new task run appears in the execution log. The Retry action preserves the original trigger actor metadata. For system comments without a parent chain, the retry falls back gracefully without crashing.

Notes for automation: The Retry button may appear on hover or in a context menu. Look for an icon button with retry/refresh semantics. After clicking, verify a new task run entry appears in the execution log section or a loading indicator shows the task was triggered.
