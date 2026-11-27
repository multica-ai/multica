-- The write half of workspace teardown's concurrency contract, and of the legacy
-- runtime merge's (MUL-5999).
--
-- THE WINDOWS
--
-- Two deletes remove rows a task write depends on, and both would otherwise strand
-- or silently drop a task that arrives while they run:
--
--   1. Workspace teardown sweeps a workspace's tasks and then commits. A task
--      inserted between the last sweep and COMMIT would be left behind with its own
--      leaf rows, owned by rows that no longer exist.
--   2. The legacy runtime merge moves an old runtime's tasks onto a new one and then
--      deletes the old runtime. A task inserted against the OLD runtime after that
--      scan is deleted by agent_task_queue's ON DELETE CASCADE when the old runtime
--      goes — it was never a task anyone chose to discard.
--
-- Re-scanning cannot close either: the check and the commit are not atomic with
-- respect to a concurrent writer, so a row can always land after the final scan.
--
-- WHAT THIS IS
--
-- A predicate that every statement writing agent_task_queue ownership calls in its
-- own WHERE clause:
--
--     INSERT INTO agent_task_queue (...)
--     SELECT ... WHERE lock_task_owner_rows(@agent_id, @issue_id, @runtime_id)
--
-- It locks, in a fixed order, the workspace rows behind the row's agent, issue and
-- runtime, and then those owner rows themselves — all FOR KEY SHARE — and returns
-- false unless every owner reference still resolves afterwards.
--
--   * FOR KEY SHARE on the workspace conflicts with the FOR UPDATE that
--     LockWorkspaceForDelete holds for a whole teardown transaction.
--   * FOR KEY SHARE on the owner rows conflicts with the FOR UPDATE that the legacy
--     merge takes on the runtimes it is about to collapse (LockRuntimesForMerge),
--     and with the DELETE of any owner row.
--
-- So a task write racing either delete blocks until it finishes, and then either
-- proceeds (the delete rolled back) or writes no row (it committed). Locking the
-- workspace alone was not enough: the merge holds only KEY SHARE there, which does
-- not conflict with another KEY SHARE, so a writer sailed past it and had its task
-- cascade-deleted with the old runtime.
--
-- WHY A CALLED PREDICATE RATHER THAN A TRIGGER OR THE FOREIGN KEYS
--
--   * Not the foreign keys. An INSERT does take FOR KEY SHARE on the rows it
--     references, which is why these windows have been survivable so far — but that
--     is a side effect of FK enforcement, and workspace_delete.sql documents the
--     legacy FKs as an expand-phase net a later contract removes. Cleanup
--     correctness must not be a hostage of that migration.
--   * Not a trigger. A row trigger is a database-layer relational constraint that
--     coordinates application behaviour behind the caller's back. This repo keeps
--     relationships in the application layer, so the write path names its own
--     fence, and TestTaskOwnershipWritesCallTheFence fails the build if a statement
--     that writes ownership forgets to.
--   * It must be in the writer's transaction. Several enqueue statements run as
--     single autocommit statements, so a separate "take the fence" statement issued
--     by the caller would be its own transaction and release the lock before the
--     insert landed. Calling the predicate from the writing statement is
--     in-transaction by construction.
--
-- DETERMINISTIC LOCK ORDER
--
-- Workspaces first (ORDER BY id), then owners as agent, issue, runtime. Every
-- reader of this contract — writers, teardown, the merge — takes locks in that
-- order, so no two of them can hold each other's next lock. Within a step,
-- LockRows sits above Sort in the plan, so ORDER BY really does fix acquisition
-- order.
--
-- VOLATILE (the default) is load-bearing: it stops the planner from inlining the
-- body or optimising the call away, which would drop the locks.

CREATE OR REPLACE FUNCTION lock_task_owner_rows(
    p_agent_id uuid,
    p_issue_id uuid,
    p_runtime_id uuid
)
RETURNS boolean
LANGUAGE plpgsql
AS $$
DECLARE
    required int := (CASE WHEN p_agent_id IS NULL THEN 0 ELSE 1 END)
                  + (CASE WHEN p_issue_id IS NULL THEN 0 ELSE 1 END)
                  + (CASE WHEN p_runtime_id IS NULL THEN 0 ELSE 1 END);
    resolved int;
    distinct_workspaces int;
    locked int;
BEGIN
    -- A row with no owner reference at all cannot belong to any workspace.
    IF required = 0 THEN
        RETURN TRUE;
    END IF;

    WITH owners AS (
        SELECT a.workspace_id FROM agent a WHERE a.id = p_agent_id
        UNION ALL
        SELECT i.workspace_id FROM issue i WHERE i.id = p_issue_id
        UNION ALL
        SELECT r.workspace_id FROM agent_runtime r WHERE r.id = p_runtime_id
    )
    SELECT count(*), count(DISTINCT workspace_id)
    INTO resolved, distinct_workspaces
    FROM owners;

    -- An owner reference that no longer resolves means its row has been deleted —
    -- by a teardown or a merge that already committed, most likely. Refuse without
    -- leaning on the foreign key to report it.
    IF resolved <> required THEN
        RETURN FALSE;
    END IF;

    -- Step 1: the workspaces.
    WITH locked_workspaces AS (
        SELECT w.id
        FROM workspace w
        WHERE w.id IN (
            SELECT a.workspace_id FROM agent a WHERE a.id = p_agent_id
            UNION
            SELECT i.workspace_id FROM issue i WHERE i.id = p_issue_id
            UNION
            SELECT r.workspace_id FROM agent_runtime r WHERE r.id = p_runtime_id
        )
        ORDER BY w.id
        FOR KEY SHARE
    )
    SELECT count(*) INTO locked FROM locked_workspaces;

    IF locked <> distinct_workspaces THEN
        RETURN FALSE;
    END IF;

    -- Step 2: the owner rows, agent then issue then runtime. Re-checking existence
    -- here is what closes the merge window: whoever is deleting one of these rows
    -- holds FOR UPDATE on it, so this either waits for them or finds the row gone.
    locked := 0;

    IF p_agent_id IS NOT NULL THEN
        PERFORM 1 FROM agent WHERE id = p_agent_id FOR KEY SHARE;
        IF FOUND THEN locked := locked + 1; END IF;
    END IF;

    IF p_issue_id IS NOT NULL THEN
        PERFORM 1 FROM issue WHERE id = p_issue_id FOR KEY SHARE;
        IF FOUND THEN locked := locked + 1; END IF;
    END IF;

    IF p_runtime_id IS NOT NULL THEN
        PERFORM 1 FROM agent_runtime WHERE id = p_runtime_id FOR KEY SHARE;
        IF FOUND THEN locked := locked + 1; END IF;
    END IF;

    RETURN locked = required;
END;
$$;
