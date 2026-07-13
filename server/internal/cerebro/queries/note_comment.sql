-- Wave 3 / G1 (TECH-3556) — comments + suggestions on a Note.
-- See migration 9077_cerebro_note_comment for the anchoring + thread +
-- suggestion model. All queries are keyed by note_id (the note's artifact id).

-- name: CreateNoteComment :one
INSERT INTO cerebro_note_comment (
    note_id, thread_root_id, kind, body,
    anchor_quote, anchor_prefix, anchor_suffix, anchor_start, anchor_end,
    suggestion_text, author_type, author_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: GetNoteComment :one
SELECT * FROM cerebro_note_comment WHERE id = $1;

-- name: ListNoteComments :many
-- Every comment (roots + replies) for a note, oldest first. The client builds
-- the threads from thread_root_id and re-anchors each root against the body.
SELECT * FROM cerebro_note_comment
WHERE note_id = $1
ORDER BY created_at ASC, id ASC;

-- name: UpdateNoteCommentBody :one
-- Edit a comment's own text. Author-only (enforced in the handler).
UPDATE cerebro_note_comment
SET body = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetNoteCommentResolved :one
-- Resolve / reopen a thread. Applied to the root; the handler also flips the
-- replies' visibility client-side (replies inherit the root's resolved state).
UPDATE cerebro_note_comment
SET resolved    = $2,
    resolved_by = $3,
    resolved_at = CASE WHEN $2 THEN now() ELSE NULL END,
    updated_at  = now()
WHERE id = $1
RETURNING *;

-- name: SetNoteSuggestionState :one
-- Accept / reject a suggestion. Accepting also resolves the thread so it leaves
-- the active margin. The body splice into the note happens in the handler.
UPDATE cerebro_note_comment
SET suggestion_state = $2,
    resolved         = CASE WHEN $2 = 'accepted' THEN true ELSE resolved END,
    resolved_by      = $3,
    resolved_at      = CASE WHEN $2 = 'accepted' THEN now() ELSE resolved_at END,
    updated_at       = now()
WHERE id = $1 AND kind = 'suggestion'
RETURNING *;

-- name: DeleteNoteComment :one
-- Deletes a comment; replies cascade via thread_root_id FK ON DELETE CASCADE.
DELETE FROM cerebro_note_comment
WHERE id = $1
RETURNING *;

-- name: CountOpenNoteComments :one
-- Open (unresolved) root-comment count for the note — drives the margin badge.
SELECT COUNT(*) FROM cerebro_note_comment
WHERE note_id = $1 AND thread_root_id IS NULL AND resolved = false;

-- name: MarkNoteCommentsSent :many
-- FIR-1621 — stamp the given comments on a note as sent-to-agent. Idempotent:
-- an already-sent comment keeps its original timestamp (COALESCE), so re-sending
-- a batch that includes earlier comments never rewrites their send time. Scoped
-- by note_id so a caller can only mark comments that belong to the note in hand.
-- Returns the updated rows so the handler can echo the new state to the client.
UPDATE cerebro_note_comment
SET sent_to_agent_at = COALESCE(sent_to_agent_at, now()),
    updated_at       = now()
WHERE note_id = sqlc.arg(note_id)
  AND id = ANY(sqlc.arg(comment_ids)::uuid[])
RETURNING *;

-- name: CountUnsentNoteComments :one
-- FIR-1621 — count of comments on the note not yet sent to an agent. Drives the
-- "you have N unsent comments" notice at the top of the note.
SELECT COUNT(*) FROM cerebro_note_comment
WHERE note_id = $1 AND sent_to_agent_at IS NULL;

-- name: ListUnsentNoteComments :many
-- FIR-1621 — the unsent draft comments on a note (sent_to_agent_at IS NULL),
-- oldest first. The send-to-agent flow uses this to gather "all unsent" when the
-- caller did not name specific comment ids.
SELECT * FROM cerebro_note_comment
WHERE note_id = $1 AND sent_to_agent_at IS NULL
ORDER BY created_at ASC, id ASC;

-- name: SetNoteCommentIssue :one
-- FIR-3102 — attach (or detach with NULL) a comment's OWN issue. The "Create
-- issue from this comment" flow points the comment at the issue it spawned, so
-- the note UI can show "opened <issue>" on that exact comment. NULL detaches,
-- letting the comment fall back to the note's overarching issue.
UPDATE cerebro_note_comment
SET issue_id   = sqlc.narg(issue_id),
    updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;
