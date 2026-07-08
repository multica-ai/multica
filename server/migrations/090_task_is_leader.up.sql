-- agent_task_queue.is_leader_task marks a task as enqueued in the squad-leader
-- role. The squad-leader self-trigger guard previously skipped a comment
-- whenever its author equalled `squad.LeaderID`, which mis-fired for an agent
-- that is simultaneously a leader and a worker of the same squad: a comment
-- posted by the agent in its worker role never woke its leader role. The
-- guard now consults the agent's most recent task on the issue and skips
-- only when that task was itself a leader task.
ALTER TABLE agent_task_queue
    ADD COLUMN is_leader_task BOOLEAN NOT NULL DEFAULT FALSE;
