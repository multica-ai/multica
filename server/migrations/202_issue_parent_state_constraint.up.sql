-- Parent/child completion is a database invariant because issue rows are
-- written by HTTP handlers, background workers, integrations, and imports.
-- Keeping the verdict at the row-write boundary means no caller can observe a
-- completed parent with an active descendant by skipping an application-side
-- preflight check.
CREATE OR REPLACE FUNCTION enforce_issue_parent_state_constraint()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    old_parent_issue_id UUID;
    lock_id UUID;
    newly_locked_count INTEGER;
    locked_ids UUID[] := ARRAY[]::UUID[];
    incomplete_descendant_count BIGINT;
    terminal_parent_id UUID;
BEGIN
    -- UpdateIssue always names status and parent_issue_id in its SQL, even for
    -- title-only edits. Avoid taking hierarchy locks unless either value
    -- actually changes.
    IF TG_OP = 'UPDATE'
       AND NEW.status IS NOT DISTINCT FROM OLD.status
       AND NEW.parent_issue_id IS NOT DISTINCT FROM OLD.parent_issue_id THEN
        RETURN NEW;
    END IF;

    IF TG_OP = 'UPDATE' THEN
        old_parent_issue_id := OLD.parent_issue_id;
    END IF;

    -- Every competing operation locks the changed row plus both old and new
    -- ancestor chains. A concurrent reparent can commit while this statement
    -- waits on one of those locks, so recompute after each acquisition round
    -- until no newly visible ancestor remains. Without this fixed-point loop,
    -- a later terminal-parent update could miss the reparented branch.
    --
    -- The first round is UUID-sorted. A newly discovered lower UUID can make
    -- a later round contend out of order; PostgreSQL then aborts one contender
    -- rather than permitting an inconsistent tree, and callers can retry it.
    LOOP
        newly_locked_count := 0;
        FOR lock_id IN
            WITH RECURSIVE related(id) AS (
                SELECT NEW.id
                UNION
                SELECT NEW.parent_issue_id
                WHERE NEW.parent_issue_id IS NOT NULL
                UNION
                SELECT old_parent_issue_id
                WHERE old_parent_issue_id IS NOT NULL
                UNION
                SELECT i.parent_issue_id
                FROM issue AS i
                JOIN related AS r ON r.id = i.id
                WHERE i.parent_issue_id IS NOT NULL
            )
            SELECT id
            FROM related
            WHERE NOT (id = ANY(locked_ids))
            ORDER BY id
        LOOP
            PERFORM pg_advisory_xact_lock(hashtextextended(lock_id::text, 0));
            locked_ids := array_append(locked_ids, lock_id);
            newly_locked_count := newly_locked_count + 1;
        END LOOP;

        EXIT WHEN newly_locked_count = 0;
    END LOOP;

    -- A terminal parent is valid only when every direct or indirect child is
    -- terminal. UNION makes a malformed historic cycle finite rather than
    -- allowing the validation query itself to loop forever.
    IF NEW.status IN ('done', 'cancelled') THEN
        WITH RECURSIVE descendants(id) AS (
            SELECT id
            FROM issue
            WHERE parent_issue_id = NEW.id
            UNION
            SELECT child.id
            FROM issue AS child
            JOIN descendants AS descendant ON child.parent_issue_id = descendant.id
        )
        SELECT COUNT(*)
        INTO incomplete_descendant_count
        FROM issue AS descendant_issue
        JOIN descendants ON descendants.id = descendant_issue.id
        WHERE descendant_issue.status NOT IN ('done', 'cancelled');

        IF incomplete_descendant_count > 0 THEN
            RAISE EXCEPTION USING
                ERRCODE = 'P0001',
                MESSAGE = 'parent_has_incomplete_descendants',
                DETAIL = format(
                    'parent_issue_id=%s;incomplete_descendant_count=%s',
                    NEW.id,
                    incomplete_descendant_count
                );
        END IF;
    END IF;

    -- Creating, reparenting, or reopening a non-terminal issue below any
    -- terminal ancestor is forbidden. The caller must first make a separate,
    -- explicit parent reopen (normally the Review/in_review state); this
    -- trigger never mutates the parent on the caller's behalf.
    IF NEW.status NOT IN ('done', 'cancelled')
       AND NEW.parent_issue_id IS NOT NULL THEN
        WITH RECURSIVE ancestors(id, parent_issue_id, status, depth, path) AS (
            SELECT id, parent_issue_id, status, 1, ARRAY[id]
            FROM issue
            WHERE id = NEW.parent_issue_id
            UNION ALL
            SELECT parent.id,
                   parent.parent_issue_id,
                   parent.status,
                   ancestor.depth + 1,
                   ancestor.path || parent.id
            FROM issue AS parent
            JOIN ancestors AS ancestor ON parent.id = ancestor.parent_issue_id
            WHERE NOT parent.id = ANY(ancestor.path)
        )
        SELECT id
        INTO terminal_parent_id
        FROM ancestors
        WHERE status IN ('done', 'cancelled')
        ORDER BY depth
        LIMIT 1;

        IF terminal_parent_id IS NOT NULL THEN
            RAISE EXCEPTION USING
                ERRCODE = 'P0001',
                MESSAGE = 'parent_must_be_reopened',
                DETAIL = format('parent_issue_id=%s', terminal_parent_id);
        END IF;
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_issue_parent_state_constraint
BEFORE INSERT OR UPDATE OF status, parent_issue_id ON issue
FOR EACH ROW
EXECUTE FUNCTION enforce_issue_parent_state_constraint();
