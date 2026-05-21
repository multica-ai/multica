-- name: ListCommentsForIssue :many
-- All comments for an issue in chronological order, capped at $3 (DB safety
-- net). Issue p99 is ~30 comments, max ever observed in prod is ~1.1k, so
-- the handler-side cap of 2000 is purely defensive.
SELECT * FROM comment
WHERE issue_id = $1 AND workspace_id = $2
ORDER BY created_at ASC, id ASC
LIMIT $3;

-- name: ListCommentsSinceForIssue :many
-- Comments created strictly after $3 in chronological order, capped at $4.
-- Powers the CLI's `--since` agent-polling flow.
SELECT * FROM comment
WHERE issue_id = $1 AND workspace_id = $2 AND created_at > $3
ORDER BY created_at ASC, id ASC
LIMIT $4;

-- name: ListThreadCommentsForIssue :many
-- Returns the root of the thread containing @anchor_id plus every descendant
-- (recursive — defends against any future deeper nesting; today's data is two
-- layers because the CreateComment path collapses replies to root, but the
-- schema does not enforce that). @anchor_id may itself be a root or a reply.
-- Output is chronological so it can be fed straight to the agent.
WITH RECURSIVE root_of AS (
    -- Walk up from the anchor until parent_id IS NULL.
    SELECT c.id, c.parent_id
    FROM comment c
    WHERE c.id = @anchor_id AND c.issue_id = @issue_id AND c.workspace_id = @workspace_id
    UNION ALL
    SELECT p.id, p.parent_id
    FROM comment p
    JOIN root_of r ON p.id = r.parent_id
),
thread_root AS (
    SELECT id FROM root_of WHERE parent_id IS NULL LIMIT 1
),
descendants AS (
    -- Start from the root, then keep adding any comment whose parent is
    -- already in the set. Cycle-safe under PK constraint (a comment cannot
    -- be its own ancestor).
    SELECT c.id, c.issue_id, c.author_type, c.author_id, c.content, c.type,
           c.created_at, c.updated_at, c.parent_id, c.workspace_id,
           c.resolved_at, c.resolved_by_type, c.resolved_by_id
    FROM comment c
    JOIN thread_root tr ON c.id = tr.id
    UNION
    SELECT c.id, c.issue_id, c.author_type, c.author_id, c.content, c.type,
           c.created_at, c.updated_at, c.parent_id, c.workspace_id,
           c.resolved_at, c.resolved_by_type, c.resolved_by_id
    FROM comment c
    JOIN descendants d ON c.parent_id = d.id
    WHERE c.issue_id = @issue_id AND c.workspace_id = @workspace_id
)
SELECT id, issue_id, author_type, author_id, content, type,
       created_at, updated_at, parent_id, workspace_id,
       resolved_at, resolved_by_type, resolved_by_id
FROM descendants
ORDER BY created_at ASC, id ASC
LIMIT @row_limit;

-- name: ListThreadCommentsForIssuePaged :many
-- Same root-walk + descendants expansion as ListThreadCommentsForIssue, but
-- returns root + only the @reply_limit most recent replies (per the
-- (created_at, id) composite key). When @has_cursor=TRUE only replies with
-- (created_at, id) < (@before_at, @before_id) are eligible — that is the
-- cursor for scrolling *within* a thread.
--
-- Root is unconditional: it is included regardless of @reply_limit (even 0)
-- and regardless of the cursor. A reader landing on a long thread needs the
-- root for the "what is this thread about" context, even if every reply has
-- been paginated past.
--
-- Reply selection happens DESC (newest replies first) so the cursor walks
-- toward older replies; the outer SELECT then re-sorts the combined output
-- ASC so the body stays chronological (oldest → newest), matching every
-- other comment list path.
WITH RECURSIVE root_of AS (
    SELECT c.id, c.parent_id
    FROM comment c
    WHERE c.id = @anchor_id AND c.issue_id = @issue_id AND c.workspace_id = @workspace_id
    UNION ALL
    SELECT p.id, p.parent_id
    FROM comment p
    JOIN root_of r ON p.id = r.parent_id
),
thread_root AS (
    SELECT id FROM root_of WHERE parent_id IS NULL LIMIT 1
),
descendants AS (
    SELECT c.id, c.issue_id, c.author_type, c.author_id, c.content, c.type,
           c.created_at, c.updated_at, c.parent_id, c.workspace_id,
           c.resolved_at, c.resolved_by_type, c.resolved_by_id
    FROM comment c
    JOIN thread_root tr ON c.id = tr.id
    UNION
    SELECT c.id, c.issue_id, c.author_type, c.author_id, c.content, c.type,
           c.created_at, c.updated_at, c.parent_id, c.workspace_id,
           c.resolved_at, c.resolved_by_type, c.resolved_by_id
    FROM comment c
    JOIN descendants d ON c.parent_id = d.id
    WHERE c.issue_id = @issue_id AND c.workspace_id = @workspace_id
),
reply_page AS (
    SELECT d.id, d.issue_id, d.author_type, d.author_id, d.content, d.type,
           d.created_at, d.updated_at, d.parent_id, d.workspace_id,
           d.resolved_at, d.resolved_by_type, d.resolved_by_id
    FROM descendants d
    WHERE d.id NOT IN (SELECT id FROM thread_root)
      AND (
          @has_cursor::boolean = FALSE
          OR (d.created_at, d.id) < (@before_at::timestamptz, @before_id::uuid)
      )
    ORDER BY d.created_at DESC, d.id DESC
    LIMIT @reply_limit
)
SELECT id, issue_id, author_type, author_id, content, type,
       created_at, updated_at, parent_id, workspace_id,
       resolved_at, resolved_by_type, resolved_by_id
FROM (
    SELECT d.id, d.issue_id, d.author_type, d.author_id, d.content, d.type,
           d.created_at, d.updated_at, d.parent_id, d.workspace_id,
           d.resolved_at, d.resolved_by_type, d.resolved_by_id
    FROM descendants d
    JOIN thread_root tr ON d.id = tr.id
    UNION ALL
    SELECT id, issue_id, author_type, author_id, content, type,
           created_at, updated_at, parent_id, workspace_id,
           resolved_at, resolved_by_type, resolved_by_id
    FROM reply_page
) combined
ORDER BY created_at ASC, id ASC;

-- name: ListRecentThreadCommentsForIssue :many
-- Returns the N most recently active threads (root + every descendant) rather
-- than the N most recent rows. A thread's "last activity" is MAX(created_at)
-- over its whole subtree; threads are ranked by (last_activity_at DESC,
-- root_id DESC) and the top N are expanded.
--
-- Why thread-grouped instead of row-recent: with row-recent the newest 20
-- comments can come from 8 different threads — the agent sees 8 unrelated
-- tails. With thread-grouped the agent sees N complete conversational arcs,
-- which matches how a human reads an issue (#2340).
--
-- Response ordering:
--   threads:     (thread_last_activity_at ASC, root_id ASC)
--   in-thread:   (created_at ASC, id ASC)
-- So the oldest-active thread appears first and the most recently-active
-- thread is at the tail, closest to "now" in an agent prompt.
--
-- Cursor scrolls back through threads. When @has_cursor=TRUE only threads
-- with (last_activity_at, root_id) < (@before_at, @before_id) are eligible.
-- The cursor is a THREAD cursor — both values identify a thread (its last
-- activity timestamp and its root comment id), not a single row.
--
-- The recursive `membership` CTE labels each comment with its thread root by
-- walking down from every root. It does not assume any maximum nesting depth,
-- which preserves correctness even if the schema ever allows reply-of-reply
-- (the agent path in TaskService.createAgentComment collapses to root today,
-- but the user-facing CreateComment handler does not enforce it).
WITH RECURSIVE membership(id, root_id, comment_created_at) AS (
    -- Each root maps to itself.
    SELECT c.id, c.id AS root_id, c.created_at
    FROM comment c
    WHERE c.issue_id = @issue_id
      AND c.workspace_id = @workspace_id
      AND c.parent_id IS NULL
    UNION ALL
    -- Each descendant inherits its parent's root_id.
    SELECT c.id, m.root_id, c.created_at
    FROM comment c
    JOIN membership m ON c.parent_id = m.id
    WHERE c.issue_id = @issue_id
      AND c.workspace_id = @workspace_id
),
thread_stats AS (
    SELECT root_id, MAX(comment_created_at)::timestamptz AS last_activity_at
    FROM membership
    GROUP BY root_id
),
picked AS (
    SELECT ts.root_id, ts.last_activity_at
    FROM thread_stats ts
    WHERE (
        @has_cursor::boolean = FALSE
        OR (ts.last_activity_at, ts.root_id) < (@before_at::timestamptz, @before_id::uuid)
    )
    ORDER BY ts.last_activity_at DESC, ts.root_id DESC
    LIMIT @thread_limit
)
SELECT c.id, c.issue_id, c.author_type, c.author_id, c.content, c.type,
       c.created_at, c.updated_at, c.parent_id, c.workspace_id,
       c.resolved_at, c.resolved_by_type, c.resolved_by_id,
       p.root_id AS thread_root_id,
       p.last_activity_at AS thread_last_activity_at
FROM picked p
JOIN membership m ON m.root_id = p.root_id
JOIN comment c ON c.id = m.id
ORDER BY p.last_activity_at ASC, p.root_id ASC, c.created_at ASC, c.id ASC;

-- name: CountComments :one
SELECT count(*) FROM comment
WHERE issue_id = $1 AND workspace_id = $2;

-- name: GetComment :one
SELECT * FROM comment
WHERE id = $1;

-- name: GetCommentInWorkspace :one
SELECT * FROM comment
WHERE id = $1 AND workspace_id = $2;

-- name: CreateComment :one
INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, parent_id)
VALUES ($1, $2, $3, $4, $5, $6, sqlc.narg(parent_id))
RETURNING *;

-- name: UpdateComment :one
UPDATE comment SET
    content = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: HasAgentCommentedSince :one
SELECT EXISTS (
    SELECT 1 FROM comment
    WHERE issue_id = @issue_id
      AND author_type = 'agent'
      AND author_id = @author_id
      AND created_at >= @since
) AS commented;

-- name: HasAgentRepliedInThread :one
-- Returns true if the given agent has posted a reply in the thread rooted at
-- the specified parent comment. Used to detect agent participation in a
-- member-started thread so that follow-up member replies still trigger the agent.
SELECT count(*) > 0 AS has_replied FROM comment
WHERE parent_id = @parent_id AND author_type = 'agent' AND author_id = @agent_id;

-- name: DeleteComment :exec
DELETE FROM comment WHERE id = $1;

-- name: ResolveComment :one
-- Idempotent: re-resolving keeps the original resolved_at + resolver. Always
-- returns the row so the handler can surface the canonical state.
UPDATE comment SET
    resolved_at = COALESCE(resolved_at, now()),
    resolved_by_type = COALESCE(resolved_by_type, $2),
    resolved_by_id = COALESCE(resolved_by_id, $3),
    updated_at = CASE WHEN resolved_at IS NULL THEN now() ELSE updated_at END
WHERE id = $1
RETURNING *;

-- name: UnresolveComment :one
-- Idempotent: a no-op clear (already unresolved) just returns the row.
UPDATE comment SET
    resolved_at = NULL,
    resolved_by_type = NULL,
    resolved_by_id = NULL,
    updated_at = CASE WHEN resolved_at IS NOT NULL THEN now() ELSE updated_at END
WHERE id = $1
RETURNING *;
