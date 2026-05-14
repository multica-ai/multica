Purpose: Verify that issues are automatically blocked when their assigned agent's task fails, and that the issue status reflects the blocked state.

Preconditions: The Multica web app is reachable. The user is signed in. An agent is configured that can be made to fail (or a failed task run already exists).

User flow: Create or open an issue assigned to an agent. Trigger the agent task. Simulate or wait for the agent task to fail (e.g., agent runtime offline, execution error). After the task fails, observe the issue status.

Expected results: When an agent task fails on an issue, the issue status automatically transitions to `blocked`. The blocked status is visible in the issue detail header and the issue list. The system comment or task-run indicator shows the failure reason. The issue can be manually unblocked by changing its status or retrying the agent task.

Notes for automation: To test this reliably, use an agent configuration that will predictably fail (e.g., invalid runtime, or trigger on an issue that causes an error). Check the status badge after the task-run-failed system event appears.
